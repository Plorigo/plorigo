package agentcore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Labels the agent stamps on every container it manages, so it can find and replace the
// previous container for a SERVICE on a redeploy and recognize its own containers. The
// label value is the service id (carried as the job's app_label).
const (
	labelManaged    = "plorigo.managed"
	labelService    = "plorigo.service"
	labelDeployment = "plorigo.deployment"
	// Basic-auth for a protected preview route, stamped on the container so the agent can rebuild
	// the Caddy route (with auth) from Docker truth. The hash is bcrypt — never the plaintext.
	labelBasicAuthUser = "plorigo.basicauth.user"
	labelBasicAuthHash = "plorigo.basicauth.hash"
	// The pretty public host label for a preview route (empty for production), so the route's host
	// survives a rebuild from Docker truth.
	labelRouteHost = "plorigo.routehost"
)

const (
	healthCheckTimeout = 30 * time.Second
	healthCheckDial    = 2 * time.Second
	logTail            = "200"
	maxReportLogLines  = 200
)

// dockerClient wraps the subset of the Docker Engine API the agent uses to run a
// deployment: pull an image, run the new one, discover its published host port,
// retire older containers for the same environment, and tail logs. It talks to the local Docker daemon
// (honoring DOCKER_HOST via client.FromEnv).
type dockerClient struct {
	cli *client.Client
}

func newDockerClient() (*dockerClient, error) {
	// API-version negotiation is on by default in this client.
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &dockerClient{cli: cli}, nil
}

func (d *dockerClient) close() {
	if d != nil && d.cli != nil {
		_ = d.cli.Close()
	}
}

// serverVersion returns the Docker daemon's version string, or an error if the daemon is
// unreachable. It is a cheap liveness+version probe the heartbeat uses each beat (see
// health.go) over the same negotiated client the deploy loop uses — so a daemon that goes
// down or comes back after startup is reflected without reconstructing the client.
func (d *dockerClient) serverVersion(ctx context.Context) (string, error) {
	if d == nil || d.cli == nil {
		return "", errors.New("docker client is not initialized")
	}
	v, err := d.cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return "", err
	}
	return v.Version, nil
}

// runInput is everything needed to run one deployment's container. public controls whether
// the container publishes a host port (so Caddy can route it); a private service publishes
// nothing to the host and is reached only by siblings over networkName (its DNS alias is
// networkAlias, the service slug).
type runInput struct {
	name          string
	imageRef      string
	env           []string
	containerPort int32
	appLabel      string
	deploymentID  string
	public        bool
	networkName   string
	networkAlias  string
	// Optional basic-auth for a protected preview route (bcrypt hash; both empty otherwise).
	basicAuthUser string
	basicAuthHash string
	// routeHost is the pretty public host label for a preview (empty for production).
	routeHost string
}

// pull pulls imageRef, surfacing distinct progress status lines through emit. It returns
// an error if the daemon reports the pull failed (e.g. an unknown image or tag).
func (d *dockerClient) pull(ctx context.Context, imageRef string, emit func(string)) error {
	resp, err := d.cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Close() }()
	last := ""
	for msg, err := range resp.JSONMessages(ctx) {
		if err != nil {
			return err
		}
		if msg.Error != nil {
			return msg.Error
		}
		if msg.Status != "" && msg.Status != last {
			emit(msg.Status)
			last = msg.Status
		}
	}
	return nil
}

// replacePreviousExcept stops and removes any older container created for this app
// label (the service), keeping the newly healthy container by id. This runs only
// after the replacement passes health checks, so a bad redeploy leaves the previous
// release running.
func (d *dockerClient) replacePreviousExcept(ctx context.Context, appLabel, keepID string, emit func(string)) error {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", labelService+"="+appLabel),
	})
	if err != nil {
		return err
	}
	for _, c := range list.Items {
		if c.ID == keepID {
			continue
		}
		emit("removing previous container " + shortID(c.ID))
		_, _ = d.cli.ContainerStop(ctx, c.ID, client.ContainerStopOptions{})
		if _, err := d.cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			return err
		}
	}
	return nil
}

func (d *dockerClient) removeContainer(ctx context.Context, containerID string, emit func(string)) error {
	if containerID == "" {
		return nil
	}
	emit("removing failed container " + shortID(containerID))
	_, _ = d.cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{})
	if _, err := d.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil {
		return err
	}
	return nil
}

// removeByService stops and removes every container (running or stopped) stamped with the given
// plorigo.service label — the teardown of a preview, which is keyed by its route_key. It returns
// how many it removed, so an already-gone preview (0 removed) is reported as an idempotent success.
func (d *dockerClient) removeByService(ctx context.Context, appLabel string, emit func(string)) (int, error) {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", labelService+"="+appLabel),
	})
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, c := range list.Items {
		emit("removing preview container " + shortID(c.ID))
		_, _ = d.cli.ContainerStop(ctx, c.ID, client.ContainerStopOptions{})
		if _, err := d.cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// removeNetwork removes a preview's isolated Docker network by name, best-effort: a network that is
// already gone, or still has an endpoint, is not an error the teardown should fail on (the agent
// reconciles Caddy and removes the container regardless). The production per-environment network is
// never passed here — only a preview's own plorigo-preview-{route_key} network.
func (d *dockerClient) removeNetwork(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	if _, err := d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); err != nil {
		return nil // already gone
	}
	_, err := d.cli.NetworkRemove(ctx, name, client.NetworkRemoveOptions{})
	return err
}

// containerLabels builds the Plorigo labels stamped on a managed container, including the optional
// basic-auth labels for a protected preview — so listManagedRoutes can rebuild the Caddy route
// (with its auth) from Docker truth after a restart or for the route-sync loop.
func containerLabels(in runInput) map[string]string {
	labels := map[string]string{
		labelManaged:    "true",
		labelService:    in.appLabel,
		labelDeployment: in.deploymentID,
	}
	if in.basicAuthUser != "" && in.basicAuthHash != "" {
		labels[labelBasicAuthUser] = in.basicAuthUser
		labels[labelBasicAuthHash] = in.basicAuthHash
	}
	if in.routeHost != "" {
		labels[labelRouteHost] = in.routeHost
	}
	return labels
}

// run creates and starts the container and returns its id and host port. A PUBLIC service
// publishes containerPort to an ephemeral host port (so Caddy can route it), discovered and
// returned; a PRIVATE service publishes nothing and returns host port 0 (it is reached only
// over the per-environment network). Every service joins networkName with networkAlias (its
// slug) so siblings resolve it by name. The container id is returned even on a start/port
// error so the caller can still record and clean it up.
func (d *dockerClient) run(ctx context.Context, in runInput) (string, int32, error) {
	port, err := network.ParsePort(fmt.Sprintf("%d/tcp", in.containerPort))
	if err != nil {
		return "", 0, err
	}

	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}
	if in.public {
		// Let Docker pick a free host port; discovered below.
		hostConfig.PortBindings = network.PortMap{port: []network.PortBinding{{
			HostIP:   netip.IPv4Unspecified(),
			HostPort: "0",
		}}}
	}

	// Attach the container to the per-environment network with its slug as a DNS alias, so a
	// sibling reaches it at http://{alias}:{containerPort}. The network is created lazily.
	var netConfig *network.NetworkingConfig
	if in.networkName != "" {
		if err := d.ensureNetwork(ctx, in.networkName); err != nil {
			return "", 0, err
		}
		netConfig = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			in.networkName: {Aliases: aliasList(in.networkAlias)},
		}}
	}

	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: in.name,
		Config: &container.Config{
			Image:        in.imageRef,
			Env:          in.env,
			ExposedPorts: network.PortSet{port: struct{}{}},
			Labels:       containerLabels(in),
		},
		HostConfig:       hostConfig,
		NetworkingConfig: netConfig,
	})
	if err != nil {
		return "", 0, err
	}
	if _, err := d.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return created.ID, 0, err
	}
	// A private service publishes no host port — there is nothing to discover.
	if !in.public {
		return created.ID, 0, nil
	}
	hostPort, err := d.publishedPort(ctx, created.ID, uint16(in.containerPort))
	if err != nil {
		return created.ID, 0, err
	}
	return created.ID, hostPort, nil
}

// ensureNetwork makes sure the per-environment bridge network exists, creating it if not.
// It tolerates a concurrent create (re-inspecting to confirm) so two parallel deploys into
// the same environment don't fail.
func (d *dockerClient) ensureNetwork(ctx context.Context, name string) error {
	if _, err := d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); err == nil {
		return nil
	}
	if _, err := d.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge"}); err != nil {
		// A concurrent create may have won the race; if the network now exists, that's fine.
		if _, ierr := d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); ierr == nil {
			return nil
		}
		return err
	}
	return nil
}

// aliasList returns the network aliases for a container (the service slug), or none when the
// alias is empty.
func aliasList(alias string) []string {
	if alias == "" {
		return nil
	}
	return []string{alias}
}

// publishedPort polls the container list for the host port mapped to privatePort. The
// mapping can take a moment to appear after start, so it retries briefly.
func (d *dockerClient) publishedPort(ctx context.Context, containerID string, privatePort uint16) (int32, error) {
	for attempt := 0; attempt < 25; attempt++ {
		list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
			All:     true,
			Filters: client.Filters{}.Add("id", containerID),
		})
		if err != nil {
			return 0, err
		}
		for _, c := range list.Items {
			for _, p := range c.Ports {
				if p.PrivatePort == privatePort && p.PublicPort != 0 {
					return int32(p.PublicPort), nil
				}
			}
		}
		if !sleep(ctx, 200*time.Millisecond) {
			return 0, ctx.Err()
		}
	}
	return 0, fmt.Errorf("no published host port found for container port %d", privatePort)
}

// recentLogs returns the container's recent stdout+stderr as plain lines. Non-TTY logs
// are multiplexed, so they are demuxed with stdcopy (both streams into one buffer to
// preserve rough ordering).
func (d *dockerClient) recentLogs(ctx context.Context, containerID string, limit int) []string {
	r, err := d.cli.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       logTail,
	})
	if err != nil {
		return nil
	}
	defer func() { _ = r.Close() }()
	var combined strings.Builder
	if _, err := stdcopy.StdCopy(&combined, &combined, r); err != nil && !errors.Is(err, io.EOF) {
		return tailLines(combined.String(), limit)
	}
	return tailLines(combined.String(), limit)
}

// listManagedRunning returns the agent's currently-running managed containers, each with
// the deployment id it was started for (its plorigo.deployment label). Stopped containers
// are excluded (All:false) — the runtime-log loop only tails live ones.
func (d *dockerClient) listManagedRunning(ctx context.Context) ([]managedContainer, error) {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     false,
		Filters: client.Filters{}.Add("label", labelManaged+"=true"),
	})
	if err != nil {
		return nil, err
	}
	out := make([]managedContainer, 0, len(list.Items))
	for _, c := range list.Items {
		depID := c.Labels[labelDeployment]
		if depID == "" {
			continue // not one of ours, or pre-dates the deployment label
		}
		out = append(out, managedContainer{ID: c.ID, DeploymentID: depID})
	}
	return out, nil
}

func (d *dockerClient) listManagedRoutes(ctx context.Context) ([]managedRoute, error) {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     false,
		Filters: client.Filters{}.Add("label", labelManaged+"=true"),
	})
	if err != nil {
		return nil, err
	}
	out := make([]managedRoute, 0, len(list.Items))
	for _, c := range list.Items {
		serviceID := c.Labels[labelService]
		depID := c.Labels[labelDeployment]
		hostPort := firstPublishedTCPPort(c.Ports)
		// A private service publishes no host port, so firstPublishedTCPPort is 0 and it is
		// naturally excluded here — Caddy only ever routes public services.
		if serviceID == "" || depID == "" || hostPort == 0 {
			continue
		}
		out = append(out, managedRoute{
			ServiceID:     serviceID,
			DeploymentID:  depID,
			ContainerID:   c.ID,
			HostPort:      hostPort,
			BasicAuthUser: c.Labels[labelBasicAuthUser],
			BasicAuthHash: c.Labels[labelBasicAuthHash],
			RouteHost:     c.Labels[labelRouteHost],
		})
	}
	return out, nil
}

// findRunningByService returns the id of a running managed container for the given service (its
// plorigo.service label), if one is running on this host. Used by the backup loop to locate the
// managed Postgres container to dump.
func (d *dockerClient) findRunningByService(ctx context.Context, serviceID string) (string, bool, error) {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     false,
		Filters: client.Filters{}.Add("label", labelService+"="+serviceID),
	})
	if err != nil {
		return "", false, err
	}
	if len(list.Items) == 0 {
		return "", false, nil
	}
	return list.Items[0].ID, true, nil
}

// execPgDump runs pg_dump INSIDE the managed Postgres container and streams the SQL dump to dst.
// The connection credentials are supplied by the control plane per job; PGPASSWORD is passed via
// the exec environment (not the arg list, so it never shows up in `ps`). The command is fixed —
// only the validated user/database identifiers vary — so there is no caller-controlled shell. A
// non-zero exit code surfaces the container's stderr as the error.
func (d *dockerClient) execPgDump(ctx context.Context, containerID, user, password, database string, dst io.Writer) error {
	created, err := d.cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd:          []string{"pg_dump", "--no-owner", "--no-privileges", "-U", user, "-d", database},
		Env:          []string{"PGPASSWORD=" + password},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}
	attach, err := d.cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attach.Close()
	// Non-TTY exec multiplexes stdout (the dump) and stderr (diagnostics); demux them.
	var stderr strings.Builder
	if _, err := stdcopy.StdCopy(dst, &stderr, attach.Reader); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	inspect, err := d.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("pg_dump exited with code %d", inspect.ExitCode)
	}
	return nil
}

// execPsqlRestore pipes a SQL dump (src) into psql INSIDE the target Postgres container, restoring
// the database. psql runs with ON_ERROR_STOP=1 so a bad dump fails loudly instead of applying
// partially. As with execPgDump, credentials come from the control plane and PGPASSWORD is passed
// via the exec environment, not the arg list. stdin is streamed in a goroutine while stdout/stderr
// are read concurrently, so a chatty restore can't deadlock on a full pipe.
func (d *dockerClient) execPsqlRestore(ctx context.Context, containerID, user, password, database string, src io.Reader) error {
	created, err := d.cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd:          []string{"psql", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", database},
		Env:          []string{"PGPASSWORD=" + password},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}
	attach, err := d.cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attach.Close()

	writeErr := make(chan error, 1)
	go func() {
		_, e := io.Copy(attach.Conn, src)
		// Half-close stdin so psql sees EOF and finishes; the read side stays open for output.
		_ = attach.CloseWrite()
		writeErr <- e
	}()
	var stderr strings.Builder
	_, demuxErr := stdcopy.StdCopy(io.Discard, &stderr, attach.Reader)
	if e := <-writeErr; e != nil {
		return e
	}
	if demuxErr != nil && !errors.Is(demuxErr, io.EOF) {
		return demuxErr
	}
	inspect, err := d.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("psql exited with code %d", inspect.ExitCode)
	}
	return nil
}

func firstPublishedTCPPort(ports []container.PortSummary) int32 {
	var chosenPrivate, chosenPublic uint16
	for _, p := range ports {
		if p.Type != "tcp" || p.PublicPort == 0 {
			continue
		}
		if chosenPrivate == 0 || p.PrivatePort < chosenPrivate {
			chosenPrivate, chosenPublic = p.PrivatePort, p.PublicPort
		}
	}
	return int32(chosenPublic)
}

// logsSince returns a container's stdout+stderr lines produced after the `since` cursor
// (empty = from now on), and the cursor to pass next time. It asks the daemon for
// timestamped logs (Timestamps:true) and advances the cursor to just past the newest line,
// so the next fetch — Since is inclusive — never re-emits a line already seen. The
// timestamp prefix is stripped from each returned line.
func (d *dockerClient) logsSince(ctx context.Context, containerID, since string, limit int) ([]string, string, error) {
	opts := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
	}
	if since != "" {
		opts.Since = since
	}
	r, err := d.cli.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return nil, since, err
	}
	defer func() { _ = r.Close() }()
	var combined strings.Builder
	// Non-TTY logs are multiplexed; demux both streams into one buffer to preserve rough
	// ordering. A demux error still yields whatever was read (cf. recentLogs).
	_, _ = stdcopy.StdCopy(&combined, &combined, r)
	lines, next := splitTimestampedLines(combined.String(), since, limit)
	return lines, next, nil
}

// splitTimestampedLines parses `docker logs --timestamps` output (each non-blank line is
// "<RFC3339Nano> <message>"), returning the messages (timestamp stripped, capped to the
// last limit) and the next cursor: one nanosecond past the newest parsed timestamp, or the
// unchanged `since` when nothing parseable was produced. Lines without a parseable
// timestamp are kept verbatim and don't move the cursor.
func splitTimestampedLines(s, since string, limit int) ([]string, string) {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	var maxTs time.Time
	advanced := false
	for _, l := range raw {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		ts, msg, ok := strings.Cut(l, " ")
		if !ok {
			lines = append(lines, l)
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			lines = append(lines, l)
			continue
		}
		lines = append(lines, msg)
		if !advanced || t.After(maxTs) {
			maxTs, advanced = t, true
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	next := since
	if advanced {
		next = maxTs.Add(time.Nanosecond).UTC().Format(time.RFC3339Nano)
	}
	return lines, next
}

// healthCheck waits until something is listening on the published host port, or fails
// after healthCheckTimeout. The agent runs on the same host, so it dials loopback.
func healthCheck(ctx context.Context, hostPort int32) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(hostPort)))
	deadline := time.Now().Add(healthCheckTimeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, healthCheckDial)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("container did not listen on %s within %s", addr, healthCheckTimeout)
		}
		if !sleep(ctx, 500*time.Millisecond) {
			return ctx.Err()
		}
	}
}

// envSlice renders an env map as the []string{"KEY=VALUE"} form Docker expects.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// tailLines splits combined output into non-blank lines, keeping at most the last limit.
func tailLines(s string, limit int) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

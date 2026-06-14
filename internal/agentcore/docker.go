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
// previous container for an environment on a redeploy and recognize its own containers.
const (
	labelManaged     = "plorigo.managed"
	labelEnvironment = "plorigo.environment"
	labelDeployment  = "plorigo.deployment"
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

// runInput is everything needed to run one deployment's container.
type runInput struct {
	name          string
	imageRef      string
	env           []string
	containerPort int32
	appLabel      string
	deploymentID  string
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
// label (the environment), keeping the newly healthy container by id. This runs only
// after the replacement passes health checks, so a bad redeploy leaves the previous
// release running.
func (d *dockerClient) replacePreviousExcept(ctx context.Context, appLabel, keepID string, emit func(string)) error {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", labelEnvironment+"="+appLabel),
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

// run creates and starts the container, publishing containerPort to an ephemeral host
// port, and returns the container id and the chosen host port. The container id is
// returned even on a start/port error so the caller can still record and clean it up.
func (d *dockerClient) run(ctx context.Context, in runInput) (string, int32, error) {
	port, err := network.ParsePort(fmt.Sprintf("%d/tcp", in.containerPort))
	if err != nil {
		return "", 0, err
	}
	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: in.name,
		Config: &container.Config{
			Image:        in.imageRef,
			Env:          in.env,
			ExposedPorts: network.PortSet{port: struct{}{}},
			Labels: map[string]string{
				labelManaged:     "true",
				labelEnvironment: in.appLabel,
				labelDeployment:  in.deploymentID,
			},
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{port: []network.PortBinding{{
				HostIP:   netip.IPv4Unspecified(),
				HostPort: "0", // let Docker pick a free host port; discovered below
			}}},
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		},
	})
	if err != nil {
		return "", 0, err
	}
	if _, err := d.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return created.ID, 0, err
	}
	hostPort, err := d.publishedPort(ctx, created.ID, uint16(in.containerPort))
	if err != nil {
		return created.ID, 0, err
	}
	return created.ID, hostPort, nil
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
		envID := c.Labels[labelEnvironment]
		depID := c.Labels[labelDeployment]
		hostPort := firstPublishedTCPPort(c.Ports)
		if envID == "" || depID == "" || hostPort == 0 {
			continue
		}
		out = append(out, managedRoute{
			EnvironmentID: envID,
			DeploymentID:  depID,
			ContainerID:   c.ID,
			HostPort:      hostPort,
		})
	}
	return out, nil
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

package agentcore

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
)

// Deployment statuses the agent reports, matching the agent.v1 protocol contract (and
// the control plane's deployments.Status* vocabulary). Defined here so the agent binary
// stays independent of the control-plane module.
const (
	statusCloning     = "cloning"
	statusBuilding    = "building"
	statusPulling     = "pulling"
	statusStarting    = "starting"
	statusHealthcheck = "healthcheck"
	statusRouting     = "routing"
	statusRunning     = "running"
	statusFailed      = "failed"
)

// sourceGit is the source_kind for a build-from-Git deployment (vs. a pre-built image).
const sourceGit = "git"

// Log streams the agent tags its reports with, matching the control plane's
// deployments.Stream* vocabulary. streamBuild is the agent's own clone/build/pull/start
// output; streamRuntime is the container's stdout/stderr.
const (
	streamBuild   = "build"
	streamRuntime = "runtime"
)

type deploymentRuntime interface {
	pull(ctx context.Context, imageRef string, emit func(string)) error
	// clone fetches the repo at gitRef into dir (shallow, anonymous — public repos only)
	// and returns the exact commit SHA it checked out.
	clone(ctx context.Context, cloneURL, gitRef, dir string, emit func(string)) (commitSHA string, err error)
	// build builds the Dockerfile at the root of dir into the local image tag, with BuildKit.
	build(ctx context.Context, dir, tag string, emit func(string)) error
	// detectPort returns the image's exposed port (its Dockerfile/base EXPOSE) so a git
	// deployment can publish the right port when the caller didn't specify one.
	detectPort(ctx context.Context, imageTag string) (int32, error)
	run(ctx context.Context, in runInput) (containerID string, hostPort int32, err error)
	replacePreviousExcept(ctx context.Context, appLabel, keepID string, emit func(string)) error
	removeContainer(ctx context.Context, containerID string, emit func(string)) error
	recentLogs(ctx context.Context, containerID string, limit int) []string
	// listManagedRunning returns the agent's currently-running managed containers, each
	// paired with the deployment id it was started for, so the runtime-log loop knows what
	// to tail and where to attach the lines.
	listManagedRunning(ctx context.Context) ([]managedContainer, error)
	// logsSince returns a container's new stdout+stderr lines produced after the `since`
	// cursor (empty = from now on), demuxed and timestamp-stripped, plus the cursor to pass
	// next time. It returns at most limit lines.
	logsSince(ctx context.Context, containerID, since string, limit int) (lines []string, nextSince string, err error)
	// listManagedRoutes returns the route-relevant details for currently-running managed
	// containers, so the agent can rebuild Caddy's desired state from Docker truth.
	listManagedRoutes(ctx context.Context) ([]managedRoute, error)
}

// managedContainer is a running container the agent manages, paired with the deployment
// it belongs to (read from its plorigo.deployment label).
type managedContainer struct {
	ID           string
	DeploymentID string
}

var runHealthCheck = healthCheck

// deployLoop polls the control plane for deployment work and executes it until ctx is
// cancelled. It runs beside heartbeatLoop, reading the identity fresh on every call so
// it follows a runtime re-registration (see heartbeatLoop). Transport errors back off
// and retry; a failed deployment (including Docker being unavailable) is reported,
// never fatal.
func deployLoop(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, router deploymentRouter, interval time.Duration) error {
	backoff := time.Second
	for {
		st := ident.get()
		resp, err := deploy.PollDeployment(ctx, connect.NewRequest(&agentv1.PollDeploymentRequest{
			AgentId:    st.AgentID,
			Credential: st.Credential,
		}))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "poll failed (retrying in %s): %v\n", backoff, err)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		if !resp.Msg.GetHasWork() {
			if !sleep(ctx, interval) {
				return nil
			}
			continue
		}
		executeDeployment(ctx, out, deploy, ident, runtime, router, resp.Msg)
		// Loop straight back to poll in case more work is queued; PollDeployment
		// returns has_work=false quickly when the queue is empty.
	}
}

// executeDeployment runs one claimed deployment end to end, reporting each transition
// and the container's recent logs. An image deployment goes pulling -> starting ->
// healthcheck -> routing -> running; a git deployment clones and builds first (cloning ->
// building -> starting -> healthcheck -> routing -> running). A private service has no
// published port to probe and no Caddy route, so it skips healthcheck/routing. Any step
// can fail.
func executeDeployment(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, router deploymentRouter, job *agentv1.PollDeploymentResponse) {
	depID := job.GetDeploymentId()
	// commitSHA/builtImageRef are set for git deployments and reported on every transition
	// after the build so the control plane records what was built. routeURL is set once the
	// agent computes the Caddy route so the dashboard can display the real deployment URL.
	var commitSHA, builtImageRef, routeURL string
	report := func(stream, status string, hostPort int32, containerID, message string, logs []string) {
		st := ident.get()
		_, err := deploy.ReportDeployment(ctx, connect.NewRequest(&agentv1.ReportDeploymentRequest{
			AgentId:       st.AgentID,
			Credential:    st.Credential,
			DeploymentId:  depID,
			Status:        status,
			HostPort:      hostPort,
			ContainerId:   containerID,
			Message:       message,
			LogLines:      logs,
			LogStream:     stream,
			CommitSha:     commitSHA,
			BuiltImageRef: builtImageRef,
			RouteUrl:      routeURL,
		}))
		if err != nil {
			fmt.Fprintf(out, "report failed for deployment %s: %v\n", depID, err)
		}
	}
	// reportBuild tags log lines as the agent's clone/build/pull/start output; reportRuntime
	// tags them as the container's own stdout/stderr. Status-only reports (no logs) use build.
	reportBuild := func(status string, hostPort int32, containerID, message string, logs []string) {
		report(streamBuild, status, hostPort, containerID, message, logs)
	}
	reportRuntime := func(status string, hostPort int32, containerID, message string, logs []string) {
		report(streamRuntime, status, hostPort, containerID, message, logs)
	}

	if runtime == nil {
		reportBuild(statusFailed, 0, "", "Docker is not available on this server", nil)
		return
	}
	if router == nil {
		reportBuild(statusFailed, 0, "", "Caddy routing is not configured on this server", nil)
		return
	}

	// Get the image onto the host: pull a pre-built image, or clone + build a git source.
	// prepLogs carries the pull/build output forward into the starting report.
	imageRef := job.GetImageRef()
	containerPort := job.GetContainerPort()
	var prepLogs []string
	if job.GetSourceKind() == sourceGit {
		built, logs, ok := buildFromSource(ctx, out, runtime, job, &commitSHA, reportBuild)
		if !ok {
			return // buildFromSource already reported the failure
		}
		imageRef, builtImageRef, prepLogs = built, built, logs

		// Auto-detect the port from the built image's EXPOSE when the caller didn't set one.
		if containerPort == 0 {
			port, err := runtime.detectPort(ctx, built)
			if err != nil {
				reportBuild(statusFailed, 0, "", "could not determine which port to publish — set a container port, or add an EXPOSE to the Dockerfile: "+err.Error(), prepLogs)
				return
			}
			containerPort = port
			fmt.Fprintf(out, "auto-detected container port %d for deployment %s\n", port, depID)
		}
	} else {
		reportBuild(statusPulling, 0, "", "pulling image "+imageRef, nil)
		if err := runtime.pull(ctx, imageRef, func(l string) { prepLogs = appendCapped(prepLogs, l, 30) }); err != nil {
			reportBuild(statusFailed, 0, "", "image pull failed: "+err.Error(), prepLogs)
			return
		}
	}

	reportBuild(statusStarting, 0, "", "starting container", prepLogs)
	appLabel := job.GetAppLabel()
	// A public service publishes a host port + gets a Caddy route; a private service is
	// reachable only by siblings over the per-environment network (no host port, no route).
	public := job.GetVisibility() != "private"
	containerID, hostPort, err := runtime.run(ctx, runInput{
		name:          containerName(depID),
		imageRef:      imageRef,
		env:           envSlice(job.GetEnv()),
		containerPort: containerPort,
		appLabel:      appLabel,
		deploymentID:  depID,
		public:        public,
		networkName:   job.GetNetworkName(),
		networkAlias:  job.GetNetworkAlias(),
	})
	if err != nil {
		// Capture the container's own output BEFORE cleanup removes it: the usual cause here is
		// the app crashing on startup (so no port is ever published), and its stderr is the only
		// thing that explains why. Report it on the runtime stream — it's the container talking.
		var crashLogs []string
		if containerID != "" {
			crashLogs = runtime.recentLogs(ctx, containerID, maxReportLogLines)
		}
		reportRuntime(statusFailed, hostPort, containerID, "could not start container: "+err.Error(), crashLogs)
		cleanupFailedContainer(ctx, out, runtime, containerID)
		return
	}

	// A public service must answer on its published host port before we route to it. A
	// private service publishes no host port (nothing to probe from the host), so it reaches
	// running once the container has started and joined the network with its alias.
	if public {
		// Health check is its own reported phase, so the timeline shows it distinctly and a
		// failure here is attributed to the health check (not to "start container").
		reportBuild(statusHealthcheck, hostPort, containerID, "running health check (waiting for the container to accept connections)", nil)
		if err := runHealthCheck(ctx, hostPort); err != nil {
			reportRuntime(statusFailed, hostPort, containerID, "health check failed: "+err.Error(), runtime.recentLogs(ctx, containerID, maxReportLogLines))
			cleanupFailedContainer(ctx, out, runtime, containerID)
			return
		}

		routeURL, err = router.routeURL(appLabel)
		if err != nil {
			reportBuild(statusFailed, hostPort, containerID, "could not derive Caddy route: "+err.Error(), nil)
			cleanupFailedContainer(ctx, out, runtime, containerID)
			return
		}
		reportBuild(statusRouting, hostPort, containerID, "routing traffic to "+routeURL, nil)
		routes, err := runtime.listManagedRoutes(ctx)
		if err != nil {
			reportBuild(statusFailed, hostPort, containerID, "could not inspect running containers for Caddy routes: "+err.Error(), nil)
			cleanupFailedContainer(ctx, out, runtime, containerID)
			return
		}
		routes = routesForDeployment(routes, managedRoute{
			ServiceID:    appLabel,
			DeploymentID: depID,
			ContainerID:  containerID,
			HostPort:     hostPort,
		})
		if logs, err := router.apply(ctx, routes); err != nil {
			if len(logs) == 0 {
				logs = []string{err.Error()}
			}
			reportBuild(statusFailed, hostPort, containerID, "Caddy routing failed: "+err.Error(), logs)
			cleanupFailedContainer(ctx, out, runtime, containerID)
			return
		}
	}

	message := runningMessage(public, routeURL, job.GetNetworkAlias(), containerPort, hostPort)
	if err := runtime.replacePreviousExcept(ctx, appLabel, containerID, func(l string) { fmt.Fprintln(out, l) }); err != nil {
		message += "; could not remove previous container: " + err.Error()
		fmt.Fprintf(out, "warning: could not remove previous container for deployment %s: %v\n", depID, err)
	}

	reportRuntime(statusRunning, hostPort, containerID, message, runtime.recentLogs(ctx, containerID, maxReportLogLines))
	syncCustomRoutes(ctx, out, deploy, ident, runtime, router)
	fmt.Fprintf(out, "deployment %s %s\n", depID, message)
}

func syncCustomRoutes(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, router deploymentRouter) {
	if runtime == nil || router == nil {
		return
	}
	routes, err := runtime.listManagedRoutes(ctx)
	if err != nil {
		fmt.Fprintf(out, "custom domain route sync skipped: could not inspect running containers: %v\n", err)
		return
	}
	if len(routes) == 0 {
		return
	}
	st := ident.get()
	reqRoutes := make([]*agentv1.ManagedRoute, 0, len(routes))
	byService := make(map[string]managedRoute, len(routes))
	for _, r := range routes {
		reqRoutes = append(reqRoutes, &agentv1.ManagedRoute{
			ServiceId:    r.ServiceID,
			DeploymentId: r.DeploymentID,
			HostPort:     r.HostPort,
		})
		byService[r.ServiceID] = r
	}
	resp, err := deploy.SyncRoutes(ctx, connect.NewRequest(&agentv1.SyncRoutesRequest{
		AgentId:    st.AgentID,
		Credential: st.Credential,
		Routes:     reqRoutes,
	}))
	if err != nil {
		fmt.Fprintf(out, "custom domain route sync failed: %v\n", err)
		return
	}
	var results []*agentv1.RouteSyncResult
	desired := routes
	for _, override := range resp.Msg.GetOverrides() {
		base, ok := byService[override.GetServiceId()]
		if !ok || len(override.GetHostnames()) == 0 {
			continue
		}
		desired = routesForDeployment(desired, managedRoute{
			ServiceID:    base.ServiceID,
			DeploymentID: base.DeploymentID,
			ContainerID:  base.ContainerID,
			HostPort:     base.HostPort,
			CustomHosts:  override.GetHostnames(),
		})
		results = append(results, &agentv1.RouteSyncResult{
			ServiceId:    base.ServiceID,
			DeploymentId: base.DeploymentID,
			Hostnames:    override.GetHostnames(),
			Ok:           true,
			Message:      "Domain is routed to this service.",
		})
	}
	if logs, err := router.apply(ctx, desired); err != nil {
		msg := "Caddy routing failed: " + err.Error()
		if len(logs) > 0 {
			msg = logs[len(logs)-1]
		}
		for _, r := range results {
			r.Ok = false
			r.Message = msg
		}
	}
	if len(results) == 0 {
		return
	}
	st = ident.get()
	if _, err := deploy.ReportRouteSync(ctx, connect.NewRequest(&agentv1.ReportRouteSyncRequest{
		AgentId:    st.AgentID,
		Credential: st.Credential,
		Results:    results,
	})); err != nil {
		fmt.Fprintf(out, "custom domain route sync report failed: %v\n", err)
	}
}

func routesForDeployment(existing []managedRoute, current managedRoute) []managedRoute {
	out := make([]managedRoute, 0, len(existing)+1)
	for _, r := range existing {
		if r.ServiceID == current.ServiceID || r.ContainerID == current.ContainerID {
			continue
		}
		out = append(out, r)
	}
	return append(out, current)
}

// runningMessage describes a running deployment: a public service shows its URL + host port;
// a private service shows the internal address siblings reach it at.
func runningMessage(public bool, routeURL, alias string, containerPort, hostPort int32) string {
	if public {
		return fmt.Sprintf("running at %s (internal host port %d)", routeURL, hostPort)
	}
	return fmt.Sprintf("running (private; reachable at http://%s:%d inside the environment)", alias, containerPort)
}

// buildFromSource clones the job's repo and builds its Dockerfile into a local image tag,
// reporting cloning -> building. It returns the built image tag, the commit SHA, and the
// build logs to carry into the starting report; ok is false when it has already reported a
// failure (the caller should stop). It sets *commitSHA so later reports include it, and
// always cleans up the temporary checkout. No credential is used — public repos only.
func buildFromSource(ctx context.Context, out io.Writer, runtime deploymentRuntime, job *agentv1.PollDeploymentResponse, commitSHA *string, report func(status string, hostPort int32, containerID, message string, logs []string)) (builtImageRef string, prepLogs []string, ok bool) {
	dir, err := os.MkdirTemp("", "plorigo-build-")
	if err != nil {
		report(statusFailed, 0, "", "could not create a build workspace: "+err.Error(), nil)
		return "", nil, false
	}
	// 0700 so the checked-out source isn't world-readable; removed when the build is done.
	_ = os.Chmod(dir, 0o700)
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			fmt.Fprintf(out, "warning: could not remove build workspace %s: %v\n", dir, rmErr)
		}
	}()

	report(statusCloning, 0, "", "cloning "+job.GetCloneUrl(), nil)
	var cloneLines []string
	checkedOut, err := runtime.clone(ctx, job.GetCloneUrl(), job.GetGitRef(), dir, func(l string) { cloneLines = appendCapped(cloneLines, l, 30) })
	if err != nil {
		report(statusFailed, 0, "", "clone failed: "+err.Error(), cloneLines)
		return "", nil, false
	}
	*commitSHA = checkedOut

	tag := job.GetBuiltImageTag()
	report(statusBuilding, 0, "", "building image with BuildKit", cloneLines)
	var buildLines []string
	if err := runtime.build(ctx, dir, tag, func(l string) { buildLines = appendCapped(buildLines, l, maxReportLogLines) }); err != nil {
		report(statusFailed, 0, "", "build failed: "+err.Error(), buildLines)
		return "", nil, false
	}
	return tag, buildLines, true
}

func cleanupFailedContainer(ctx context.Context, out io.Writer, runtime deploymentRuntime, containerID string) {
	if containerID == "" {
		return
	}
	if err := runtime.removeContainer(ctx, containerID, func(l string) { fmt.Fprintln(out, l) }); err != nil {
		fmt.Fprintf(out, "warning: could not remove failed container %s: %v\n", shortID(containerID), err)
	}
}

// containerName is a stable, unique name per deployment.
func containerName(deploymentID string) string {
	return "plorigo-" + shortID(deploymentID)
}

func appendCapped(s []string, line string, limit int) []string {
	if len(s) >= limit {
		return s
	}
	return append(s, line)
}

// defaultRuntimeLogInterval is how often the agent tails each running container for new
// output. Short enough that the dashboard's runtime logs feel live, but each tick also
// caps the lines it forwards (maxReportLogLines) so a chatty app can't flood the timeline.
const defaultRuntimeLogInterval = 3 * time.Second
const defaultRouteSyncInterval = 20 * time.Second

func routeSyncLoop(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, router deploymentRouter, interval time.Duration) error {
	if runtime == nil || router == nil {
		if !sleep(ctx, interval) {
			return nil
		}
	}
	for {
		syncCustomRoutes(ctx, out, deploy, ident, runtime, router)
		if !sleep(ctx, interval) {
			return nil
		}
	}
}

// runtimeLogLoop continuously tails the stdout/stderr of every running container the agent
// manages and streams new lines to the control plane as runtime-stream log events, so a
// running app's logs are visible in the dashboard without SSH. It runs beside
// heartbeatLoop/deployLoop until ctx is cancelled, reading the identity fresh each report
// so it follows a runtime re-registration.
//
// Per-container cursors live in memory only. A container first seen this process run is
// tailed from "now" forward (seeded on first sight, fetched from the next tick on). That
// both avoids re-sending the deploy-time snapshot the deploy loop already captured and
// avoids duplicating history after an agent restart — at the cost of a small gap for any
// lines emitted between container start (or agent restart) and the first tick.
func runtimeLogLoop(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, interval time.Duration) error {
	if runtime == nil {
		// No Docker: nothing to tail. Stay alive like the sibling loops so Run() doesn't
		// treat this as a fatal early return and cancel the others.
		<-ctx.Done()
		return nil
	}
	if interval <= 0 {
		interval = defaultRuntimeLogInterval
	}
	cursors := map[string]string{} // container id -> next `since` cursor
	for {
		if !sleep(ctx, interval) {
			return nil
		}
		running, err := runtime.listManagedRunning(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "runtime log: could not list containers: %v\n", err)
			continue
		}
		seen := make(map[string]struct{}, len(running))
		for _, c := range running {
			seen[c.ID] = struct{}{}
			since, known := cursors[c.ID]
			if !known {
				// First sight: start from now so we don't replay the container's history.
				cursors[c.ID] = nowCursor()
				continue
			}
			lines, next, err := runtime.logsSince(ctx, c.ID, since, maxReportLogLines)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(out, "runtime log: could not tail %s: %v\n", shortID(c.ID), err)
				continue
			}
			cursors[c.ID] = next
			if len(lines) == 0 {
				continue
			}
			st := ident.get()
			if _, err := deploy.ReportDeployment(ctx, connect.NewRequest(&agentv1.ReportDeploymentRequest{
				AgentId:      st.AgentID,
				Credential:   st.Credential,
				DeploymentId: c.DeploymentID,
				Status:       statusRunning,
				LogLines:     lines,
				LogStream:    streamRuntime,
			})); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(out, "runtime log: report for deployment %s failed: %v\n", c.DeploymentID, err)
			}
		}
		// Drop cursors for containers that are gone (stopped, removed, or superseded), so a
		// reused id starts fresh and the map can't grow without bound.
		for id := range cursors {
			if _, ok := seen[id]; !ok {
				delete(cursors, id)
			}
		}
	}
}

// nowCursor is the `since` cursor seeding value: the current time, which logsSince uses to
// fetch only lines produced from here on. The agent shares the Docker host's clock.
func nowCursor() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

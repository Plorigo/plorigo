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
	statusCloning  = "cloning"
	statusBuilding = "building"
	statusPulling  = "pulling"
	statusStarting = "starting"
	statusRunning  = "running"
	statusFailed   = "failed"
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
}

var runHealthCheck = healthCheck

// deployLoop polls the control plane for deployment work and executes it until ctx is
// cancelled. It runs beside heartbeatLoop, reading the identity fresh on every call so
// it follows a runtime re-registration (see heartbeatLoop). Transport errors back off
// and retry; a failed deployment (including Docker being unavailable) is reported,
// never fatal.
func deployLoop(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, interval time.Duration) error {
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
		executeDeployment(ctx, out, deploy, ident, runtime, resp.Msg)
		// Loop straight back to poll in case more work is queued; PollDeployment
		// returns has_work=false quickly when the queue is empty.
	}
}

// executeDeployment runs one claimed deployment end to end, reporting each transition
// and the container's recent logs. An image deployment goes pulling -> starting ->
// running; a git deployment clones and builds first (cloning -> building -> starting ->
// running). Any step can fail.
func executeDeployment(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, job *agentv1.PollDeploymentResponse) {
	depID := job.GetDeploymentId()
	// commitSHA/builtImageRef are set for git deployments and reported on every transition
	// after the build so the control plane records what was built.
	var commitSHA, builtImageRef string
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
	containerID, hostPort, err := runtime.run(ctx, runInput{
		name:          containerName(depID),
		imageRef:      imageRef,
		env:           envSlice(job.GetEnv()),
		containerPort: containerPort,
		appLabel:      appLabel,
		deploymentID:  depID,
	})
	if err != nil {
		reportBuild(statusFailed, hostPort, containerID, "could not start container: "+err.Error(), nil)
		cleanupFailedContainer(ctx, out, runtime, containerID)
		return
	}

	if err := runHealthCheck(ctx, hostPort); err != nil {
		reportRuntime(statusFailed, hostPort, containerID, "health check failed: "+err.Error(), runtime.recentLogs(ctx, containerID, maxReportLogLines))
		cleanupFailedContainer(ctx, out, runtime, containerID)
		return
	}

	message := fmt.Sprintf("running on host port %d", hostPort)
	if err := runtime.replacePreviousExcept(ctx, appLabel, containerID, func(l string) { fmt.Fprintln(out, l) }); err != nil {
		message = fmt.Sprintf("running on host port %d; could not remove previous container: %v", hostPort, err)
		fmt.Fprintf(out, "warning: could not remove previous container for deployment %s: %v\n", depID, err)
	}

	reportRuntime(statusRunning, hostPort, containerID, message, runtime.recentLogs(ctx, containerID, maxReportLogLines))
	fmt.Fprintf(out, "deployment %s running on host port %d\n", depID, hostPort)
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

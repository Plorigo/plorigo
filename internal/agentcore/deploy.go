package agentcore

import (
	"context"
	"fmt"
	"io"
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
)

// Deployment statuses the agent reports, matching the agent.v1 protocol contract (and
// the control plane's deployments.Status* vocabulary). Defined here so the agent binary
// stays independent of the control-plane module.
const (
	statusPulling  = "pulling"
	statusStarting = "starting"
	statusRunning  = "running"
	statusFailed   = "failed"
)

type deploymentRuntime interface {
	pull(ctx context.Context, imageRef string, emit func(string)) error
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
// (pulling -> starting -> running, or failed) and the container's recent logs.
func executeDeployment(ctx context.Context, out io.Writer, deploy agentv1connect.DeployServiceClient, ident *identity, runtime deploymentRuntime, job *agentv1.PollDeploymentResponse) {
	depID := job.GetDeploymentId()
	report := func(status string, hostPort int32, containerID, message string, logs []string) {
		st := ident.get()
		_, err := deploy.ReportDeployment(ctx, connect.NewRequest(&agentv1.ReportDeploymentRequest{
			AgentId:      st.AgentID,
			Credential:   st.Credential,
			DeploymentId: depID,
			Status:       status,
			HostPort:     hostPort,
			ContainerId:  containerID,
			Message:      message,
			LogLines:     logs,
		}))
		if err != nil {
			fmt.Fprintf(out, "report failed for deployment %s: %v\n", depID, err)
		}
	}

	if runtime == nil {
		report(statusFailed, 0, "", "Docker is not available on this server", nil)
		return
	}

	imageRef := job.GetImageRef()
	report(statusPulling, 0, "", "pulling image "+imageRef, nil)
	var pullLines []string
	if err := runtime.pull(ctx, imageRef, func(l string) { pullLines = appendCapped(pullLines, l, 30) }); err != nil {
		report(statusFailed, 0, "", "image pull failed: "+err.Error(), pullLines)
		return
	}

	report(statusStarting, 0, "", "starting container", pullLines)
	appLabel := job.GetAppLabel()
	containerID, hostPort, err := runtime.run(ctx, runInput{
		name:          containerName(depID),
		imageRef:      imageRef,
		env:           envSlice(job.GetEnv()),
		containerPort: job.GetContainerPort(),
		appLabel:      appLabel,
		deploymentID:  depID,
	})
	if err != nil {
		report(statusFailed, hostPort, containerID, "could not start container: "+err.Error(), nil)
		cleanupFailedContainer(ctx, out, runtime, containerID)
		return
	}

	if err := runHealthCheck(ctx, hostPort); err != nil {
		report(statusFailed, hostPort, containerID, "health check failed: "+err.Error(), runtime.recentLogs(ctx, containerID, maxReportLogLines))
		cleanupFailedContainer(ctx, out, runtime, containerID)
		return
	}

	message := fmt.Sprintf("running on host port %d", hostPort)
	if err := runtime.replacePreviousExcept(ctx, appLabel, containerID, func(l string) { fmt.Fprintln(out, l) }); err != nil {
		message = fmt.Sprintf("running on host port %d; could not remove previous container: %v", hostPort, err)
		fmt.Fprintf(out, "warning: could not remove previous container for deployment %s: %v\n", depID, err)
	}

	report(statusRunning, hostPort, containerID, message, runtime.recentLogs(ctx, containerID, maxReportLogLines))
	fmt.Fprintf(out, "deployment %s running on host port %d\n", depID, hostPort)
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

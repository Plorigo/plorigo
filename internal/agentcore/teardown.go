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

// Teardown statuses the agent reports, matching the agent.v1 protocol and the control plane's
// deployments.TeardownStatus* vocabulary.
const (
	teardownStatusStopping  = "stopping"
	teardownStatusRemoving  = "removing"
	teardownStatusSucceeded = "succeeded"
	teardownStatusFailed    = "failed"
)

const defaultTeardownPollInterval = 10 * time.Second

// teardownRuntime is the Docker surface the teardown loop needs: remove a preview's containers (by
// its plorigo.service={route_key} label), re-read the running routes so Caddy can be reconciled from
// Docker truth, and remove the preview's isolated network. *dockerClient satisfies it (alongside
// deploymentRuntime / backupRuntime).
type teardownRuntime interface {
	removeByService(ctx context.Context, appLabel string, emit func(string)) (int, error)
	listManagedRoutes(ctx context.Context) ([]managedRoute, error)
	removeNetwork(ctx context.Context, name string) error
}

// teardownLoop polls the control plane for preview-teardown work and runs it until ctx is cancelled.
// It runs beside the heartbeat/deploy/backup loops, reading the identity fresh on every call so it
// follows a runtime re-registration. Transport errors back off and retry; a failed teardown
// (including Docker being unavailable) is reported, never fatal.
func teardownLoop(ctx context.Context, out io.Writer, teardown agentv1connect.TeardownServiceClient, ident *identity, runtime teardownRuntime, router deploymentRouter, interval time.Duration) error {
	if interval <= 0 {
		interval = defaultTeardownPollInterval
	}
	backoff := time.Second
	for {
		st := ident.get()
		resp, err := teardown.PollTeardownJob(ctx, connect.NewRequest(&agentv1.PollTeardownJobRequest{
			AgentId:    st.AgentID,
			Credential: st.Credential,
		}))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "teardown poll failed (retrying in %s): %v\n", backoff, err)
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
		executeTeardown(ctx, out, teardown, ident, runtime, router, resp.Msg)
	}
}

// executeTeardown runs one claimed teardown: it stops + removes the preview's container(s) (found
// by the plorigo.service={route_key} label), reconciles Caddy from Docker truth so the route drops,
// and best-effort removes the preview's isolated network. It is idempotent: a preview whose
// container is already gone reports succeeded. Any hard failure (Docker unavailable, a container
// that won't remove, Caddy failing to reload) is reported as failed.
func executeTeardown(ctx context.Context, out io.Writer, teardown agentv1connect.TeardownServiceClient, ident *identity, runtime teardownRuntime, router deploymentRouter, job *agentv1.PollTeardownJobResponse) {
	teardownID := job.GetTeardownId()
	routeKey := job.GetRouteKey()
	report := func(status, message, errMsg string) {
		st := ident.get()
		if _, err := teardown.ReportTeardownJob(ctx, connect.NewRequest(&agentv1.ReportTeardownJobRequest{
			AgentId:    st.AgentID,
			Credential: st.Credential,
			TeardownId: teardownID,
			Status:     status,
			Message:    message,
			Error:      errMsg,
		})); err != nil {
			fmt.Fprintf(out, "teardown report failed for %s: %v\n", teardownID, err)
		}
	}
	fail := func(msg string) { report(teardownStatusFailed, "", msg) }

	if runtime == nil {
		fail("Docker is not available on this server")
		return
	}

	report(teardownStatusStopping, "removing the preview container", "")
	removed, err := runtime.removeByService(ctx, routeKey, func(l string) { fmt.Fprintln(out, l) })
	if err != nil {
		fail("could not remove the preview container: " + err.Error())
		return
	}

	// Reconcile Caddy from Docker truth so the preview's route drops (its container is gone now, so
	// listManagedRoutes no longer includes it). router is always configured when the agent runs;
	// guard defensively so a missing router doesn't panic a teardown whose container is already gone.
	report(teardownStatusRemoving, "dropping the Caddy route", "")
	if router != nil {
		routes, lerr := runtime.listManagedRoutes(ctx)
		if lerr != nil {
			fail("could not inspect running containers to reconcile Caddy: " + lerr.Error())
			return
		}
		if _, aerr := router.apply(ctx, routes); aerr != nil {
			fail("could not reconcile Caddy after removing the preview: " + aerr.Error())
			return
		}
	}

	// Best-effort: remove the preview's isolated network now that its container is gone. A failure
	// here never fails the teardown — the container and route, the substance of a teardown, are gone.
	if netName := job.GetNetworkName(); netName != "" {
		if nerr := runtime.removeNetwork(ctx, netName); nerr != nil {
			fmt.Fprintf(out, "warning: could not remove preview network %s: %v\n", netName, nerr)
		}
	}

	msg := fmt.Sprintf("removed %d preview container(s) and dropped the route", removed)
	if removed == 0 {
		msg = "the preview was already removed; route reconciled"
	}
	report(teardownStatusSucceeded, msg, "")
	fmt.Fprintf(out, "teardown %s succeeded (%s)\n", teardownID, msg)
}

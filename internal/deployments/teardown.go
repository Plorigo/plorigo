package deployments

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// Teardown statuses (teardown_jobs), mirrored in the agent and the DB CHECK constraint
// (00029_teardown_jobs.sql). queued/assigned are control-plane states; the rest are agent-reported.
const (
	TeardownStatusQueued    = "queued"
	TeardownStatusAssigned  = "assigned"
	TeardownStatusStopping  = "stopping"
	TeardownStatusRemoving  = "removing"
	TeardownStatusSucceeded = "succeeded"
	TeardownStatusFailed    = "failed"
)

// StatusTornDown is the terminal status a preview's deployment rows move to once its container and
// route are removed, so the dashboard stops showing the preview as running (00029_teardown_jobs.sql).
const StatusTornDown = "torndown"

// TeardownJob is one request to remove a preview deployment: its container and Caddy route.
type TeardownJob struct {
	ID            string
	DeploymentID  string
	ServiceID     string
	RouteKey      string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	Status        string
	Message       string
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ClaimedTeardown is a teardown job handed to the agent: the route_key whose container it removes
// and the isolated preview network to clean up.
type ClaimedTeardown struct {
	HasWork     bool
	TeardownID  string
	RouteKey    string
	NetworkName string
}

// ReportTeardownInput is an agent's reported transition for a teardown it is executing.
type ReportTeardownInput struct {
	AgentID    string
	Credential string
	TeardownID string
	Status     string
	Message    string
	Error      string
}

// TeardownPreview enqueues the removal of a preview deployment. It authorizes the caller (same
// privilege as a deploy — a member who can deploy can remove a preview), resolves the preview's
// route_key + server from its deployment row, and inserts the queued teardown + its audit row in
// one tx; the preview's server agent then claims and runs it. The target must be a preview
// deployment. Idempotent at the agent (an already-gone container is success).
func (s *service) TeardownPreview(ctx context.Context, deploymentID string) (TeardownJob, error) {
	if _, err := id.Parse(deploymentID); err != nil {
		return TeardownJob{}, problem.InvalidInput("a valid deployment id is required")
	}
	dep, ok, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return TeardownJob{}, problem.Internalf(err, "teardown preview")
	}
	if !ok {
		return TeardownJob{}, problem.NotFound("deployment %s not found", deploymentID)
	}
	if dep.Kind != KindPreview {
		return TeardownJob{}, problem.InvalidInput("only a preview deployment can be torn down")
	}

	caller := principal.FromContext(ctx)
	// Removing a preview is a mutating action; it takes the same privilege as a deploy.
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentCreate, authz.Resource{Type: "deployment", WorkspaceID: dep.WorkspaceID, ID: dep.ID}); err != nil {
		return TeardownJob{}, err
	}

	// route_key drives the agent's container-label match and Caddy reconcile. Older preview rows
	// predating route_key fall back to the service id (same fallback as PollDeployment).
	routeKey := dep.RouteKey
	if routeKey == "" {
		routeKey = dep.ServiceID
	}

	var created TeardownJob
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		created, txErr = s.store.InsertTeardownJob(ctx, tx, NewTeardownJob{
			DeploymentID:  dep.ID,
			ServiceID:     dep.ServiceID,
			RouteKey:      routeKey,
			EnvironmentID: dep.EnvironmentID,
			ProjectID:     dep.ProjectID,
			WorkspaceID:   dep.WorkspaceID,
			ServerID:      dep.ServerID,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.teardown", "deployment", dep.ID, dep.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return TeardownJob{}, mapErr(err, "teardown preview")
	}
	s.log.Info("preview teardown enqueued", "id", created.ID, "deployment_id", dep.ID, "service_id", dep.ServiceID, "route_key", routeKey, "server_id", dep.ServerID, "workspace_id", dep.WorkspaceID, "actor", caller.UserID)
	return created, nil
}

// ListTeardownsByService returns a service's teardown jobs (newest first), so the dashboard can
// show a preview's removal in progress or its failure.
func (s *service) ListTeardownsByService(ctx context.Context, serviceID string) ([]TeardownJob, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return nil, problem.InvalidInput("a valid service_id is required")
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list teardowns")
	}
	if !ok {
		return nil, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListTeardownsByService(ctx, serviceID)
}

// PollTeardownJob atomically claims the next queued teardown for the agent's server, if any, and
// resolves the preview's isolated network name so the agent can clean it up. Credential-
// authenticated, not policy-authorized (like the deployment gateway).
func (s *service) PollTeardownJob(ctx context.Context, in PollInput) (ClaimedTeardown, error) {
	if in.Credential == "" {
		return ClaimedTeardown{}, problem.InvalidInput("a credential is required")
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return ClaimedTeardown{}, err
	}

	var claimed TeardownJob
	var has bool
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		t, ok, txErr := s.store.ClaimNextTeardownForServer(ctx, tx, serverID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return nil
		}
		claimed, has = t, true
		return nil
	})
	if err != nil {
		return ClaimedTeardown{}, problem.Internalf(err, "poll teardown job")
	}
	if !has {
		return ClaimedTeardown{HasWork: false}, nil
	}
	s.log.Info("teardown claimed", "id", claimed.ID, "route_key", claimed.RouteKey, "server_id", serverID)
	return ClaimedTeardown{
		HasWork:     true,
		TeardownID:  claimed.ID,
		RouteKey:    claimed.RouteKey,
		NetworkName: previewNetworkName(claimed.RouteKey),
	}, nil
}

// ReportTeardownJob records a teardown's status transition. It verifies the teardown belongs to the
// agent's own server before writing anything; on success it also marks the preview's deployment
// rows torn down so the dashboard stops showing the preview as running.
func (s *service) ReportTeardownJob(ctx context.Context, in ReportTeardownInput) error {
	if in.Credential == "" {
		return problem.InvalidInput("a credential is required")
	}
	if _, err := id.Parse(in.TeardownID); err != nil {
		return problem.InvalidInput("a valid teardown id is required")
	}
	if !isAgentReportableTeardownStatus(in.Status) {
		return problem.InvalidInput("status %q is not a valid agent-reported teardown status", in.Status)
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return err
	}
	t, ok, err := s.store.GetTeardownJob(ctx, in.TeardownID)
	if err != nil {
		return problem.Internalf(err, "report teardown job")
	}
	if !ok {
		return problem.NotFound("teardown %s not found", in.TeardownID)
	}
	if t.ServerID != serverID {
		return problem.PermissionDenied("this agent does not own teardown %s", in.TeardownID)
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if _, txErr := s.store.UpdateTeardownStatus(ctx, tx, TeardownStatusUpdate{
			TeardownID: in.TeardownID,
			Status:     in.Status,
			Message:    in.Message,
			Error:      in.Error,
		}); txErr != nil {
			return txErr
		}
		// Once the container and route are gone, retire the preview's deployment rows so the
		// dashboard stops showing it running. Keyed by route_key on the same server, so production
		// and other previews are untouched.
		if in.Status == TeardownStatusSucceeded {
			return s.store.MarkPreviewTornDown(ctx, tx, t.RouteKey, t.ServerID)
		}
		return nil
	})
	if err != nil {
		return problem.Internalf(err, "report teardown job")
	}
	return nil
}

// isAgentReportableTeardownStatus bounds the statuses an agent may report for a teardown.
func isAgentReportableTeardownStatus(status string) bool {
	switch status {
	case TeardownStatusStopping, TeardownStatusRemoving, TeardownStatusSucceeded, TeardownStatusFailed:
		return true
	}
	return false
}

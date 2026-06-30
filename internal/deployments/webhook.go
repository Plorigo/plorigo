package deployments

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// webhookActor is the audit actor for a preview action triggered by a GitHub webhook rather than a
// dashboard user (the action is gated by the webhook's signature verification + repo→service
// mapping, not a user session).
const webhookActor = "github-webhook"

// CreatePreviewForPR creates (or, on a re-push, supersedes) a PR preview for a service, triggered by
// a verified GitHub webhook. It resolves the service's server (the one its latest deployment uses)
// and reuses the same preview-build path as the dashboard's CreatePreview — so a webhook preview is
// identical to a manual one. NOT policy-authorized (the webhook is the gate). Returns the new
// preview deployment id.
func (s *service) CreatePreviewForPR(ctx context.Context, serviceID string, prNumber int32) (string, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return "", problem.InvalidInput("a valid service_id is required")
	}
	if prNumber <= 0 {
		return "", problem.InvalidInput("a valid pull request number is required")
	}
	serverID, ok, err := s.store.LatestServerForService(ctx, serviceID)
	if err != nil {
		return "", problem.Internalf(err, "create pr preview")
	}
	if !ok {
		// A service that has never deployed has no server to preview on. Skip rather than error —
		// the webhook handler treats this as "nothing to do" for this service.
		return "", problem.InvalidInput("service %s has not deployed yet, so it has no server to preview on", serviceID)
	}
	// Webhook-driven previews are unprotected (no password — there's no place to enter one).
	dep, err := s.enqueuePreview(ctx, serviceID, serverID, "", prNumber, 0, "", "", webhookActor)
	if err != nil {
		return "", err
	}
	return dep.ID, nil
}

// TeardownPreviewForPR enqueues teardown of a service's PR preview, triggered by a verified webhook
// (a PR close/merge). It resolves the preview's route_key, finds its latest not-yet-torndown
// deployment, and enqueues a teardown job + audit. It is an idempotent no-op when no active preview
// exists (a re-delivered close, or a PR that never had a preview). NOT policy-authorized.
func (s *service) TeardownPreviewForPR(ctx context.Context, serviceID string, prNumber int32) error {
	if _, err := id.Parse(serviceID); err != nil {
		return problem.InvalidInput("a valid service_id is required")
	}
	if prNumber <= 0 {
		return problem.InvalidInput("a valid pull request number is required")
	}
	// The PR preview's route_key is keyed by PR number (ref is ignored when prNumber > 0), matching
	// what CreatePreviewForPR derived.
	routeKey := previewRouteKey(serviceID, prNumber, "")
	dep, ok, err := s.store.LatestActivePreviewByRouteKey(ctx, serviceID, routeKey)
	if err != nil {
		return problem.Internalf(err, "teardown pr preview")
	}
	if !ok {
		return nil // no active preview for this PR — idempotent no-op
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
		return s.audit.Record(ctx, tx, "deployment.teardown", "deployment", dep.ID, dep.WorkspaceID, webhookActor)
	})
	if err != nil {
		return mapErr(err, "teardown pr preview")
	}
	s.log.Info("preview teardown enqueued (webhook)", "id", created.ID, "deployment_id", dep.ID, "service_id", serviceID, "route_key", routeKey, "pr_number", prNumber)
	return nil
}

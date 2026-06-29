package deployments

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// expiryActor is the audit actor for a teardown the control plane's expiry sweep enqueues (there is
// no user — it is a scheduled system action).
const expiryActor = "preview-expiry"

// ExpirePreviews tears down every RUNNING preview deployment older than ttl by enqueuing a teardown
// job for each (reusing the phase-2 machinery), so abandoned branch/PR previews don't accumulate. It
// is the entry point for the control plane's periodic expiry sweep: not policy-authorized (a system
// action, audited with a system actor), and idempotent — the agent's teardown is a no-op for an
// already-gone container, and once a preview is torn down its rows leave 'running' so a later sweep
// skips them. ttl <= 0 disables expiry (returns 0 without scanning). Returns how many it enqueued.
func (s *service) ExpirePreviews(ctx context.Context, ttl time.Duration) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl)
	expired, err := s.store.ListExpiredPreviews(ctx, cutoff)
	if err != nil {
		return 0, problem.Internalf(err, "expire previews")
	}

	enqueued := 0
	for _, dep := range expired {
		routeKey := dep.RouteKey
		if routeKey == "" {
			routeKey = dep.ServiceID
		}
		txErr := s.tx.WithinTx(ctx, func(tx database.Tx) error {
			if _, err := s.store.InsertTeardownJob(ctx, tx, NewTeardownJob{
				DeploymentID:  dep.ID,
				ServiceID:     dep.ServiceID,
				RouteKey:      routeKey,
				EnvironmentID: dep.EnvironmentID,
				ProjectID:     dep.ProjectID,
				WorkspaceID:   dep.WorkspaceID,
				ServerID:      dep.ServerID,
			}); err != nil {
				return err
			}
			return s.audit.Record(ctx, tx, "deployment.expire", "deployment", dep.ID, dep.WorkspaceID, expiryActor)
		})
		if txErr != nil {
			// One failure must not abort the sweep — log and keep reaping the rest.
			s.log.Warn("could not enqueue preview expiry teardown", "deployment_id", dep.ID, "route_key", routeKey, "error", txErr)
			continue
		}
		enqueued++
	}
	if enqueued > 0 {
		s.log.Info("expired previews", "count", enqueued, "ttl", ttl.String())
	}
	return enqueued, nil
}

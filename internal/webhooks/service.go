package webhooks

import (
	"context"
	"fmt"
	"log/slog"
)

// service is the business logic. It orchestrates ports only — no SQL, no HTTP, no signature
// verification (that happens at the HTTP entry point before this runs). It re-scopes every event to
// the installation's workspace and the repo's services, so a verified delivery can only ever touch
// what that installation legitimately maps to.
type service struct {
	store      Store
	creator    PreviewCreator
	teardowner PreviewTeardowner
	log        *slog.Logger
}

func newService(store Store, creator PreviewCreator, teardowner PreviewTeardowner, log *slog.Logger) *service {
	return &service{store: store, creator: creator, teardowner: teardowner, log: log}
}

var _ Service = (*service)(nil)

// HandlePullRequest maps a verified pull_request event to preview actions: open/synchronize/reopen
// create (or re-push) a preview per matching service; close/merge tears it down. It is idempotent at
// this level — an unknown installation, an unmatched repo, or an unhandled action is a no-op, and
// the create/teardown calls it makes are themselves safe to repeat (a re-push supersedes; a teardown
// of an already-gone preview is a no-op). A per-service failure is logged and skipped so one bad
// service neither drops the others nor fails the whole delivery (which would make GitHub redeliver).
func (s *service) HandlePullRequest(ctx context.Context, e PullRequestEvent) (HandleResult, error) {
	res := HandleResult{Action: e.Action}

	create := false
	switch e.Action {
	case ActionOpened, ActionSynchronize, ActionReopened:
		create = true
	case ActionClosed: // covers merged and plain-closed
		create = false
	default:
		res.Ignored = "unhandled action"
		return res, nil
	}
	if e.InstallationID == "" || e.Owner == "" || e.Repo == "" || e.PRNumber <= 0 {
		res.Ignored = "incomplete event"
		return res, nil
	}

	workspaceID, ok, err := s.store.WorkspaceForInstallation(ctx, e.InstallationID)
	if err != nil {
		return res, fmt.Errorf("resolve installation: %w", err)
	}
	if !ok {
		res.Ignored = "unknown installation"
		return res, nil
	}
	serviceIDs, err := s.store.ServicesForRepo(ctx, workspaceID, e.Owner, e.Repo)
	if err != nil {
		return res, fmt.Errorf("resolve services: %w", err)
	}
	if len(serviceIDs) == 0 {
		res.Ignored = "no matching service"
		return res, nil
	}
	res.MatchedServices = len(serviceIDs)

	for _, serviceID := range serviceIDs {
		if create {
			if _, err := s.creator.CreatePreviewForPR(ctx, serviceID, e.PRNumber); err != nil {
				s.log.Warn("webhook preview create failed", "service_id", serviceID, "pr", e.PRNumber, "error", err)
				continue
			}
			res.Created++
		} else {
			if err := s.teardowner.TeardownPreviewForPR(ctx, serviceID, e.PRNumber); err != nil {
				s.log.Warn("webhook preview teardown failed", "service_id", serviceID, "pr", e.PRNumber, "error", err)
				continue
			}
			res.TornDown++
		}
	}
	s.log.Info("github webhook handled", "action", e.Action, "owner", e.Owner, "repo", e.Repo, "pr", e.PRNumber, "matched", res.MatchedServices, "created", res.Created, "tore_down", res.TornDown)
	return res, nil
}

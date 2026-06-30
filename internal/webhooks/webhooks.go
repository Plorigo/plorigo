// Package webhooks turns verified inbound GitHub webhook deliveries into preview actions: a pull
// request opened/synchronized/reopened auto-creates (or re-pushes) a preview, and a PR
// closed/merged tears it down. It is a PRIVILEGED, INBOUND module — the one place external events
// drive deployments — so the HTTP entry point (internal/app) verifies the GitHub App webhook
// signature BEFORE anything here runs, and this module re-scopes every event to the workspace that
// connected the installation and only the services whose source matches the repo. It owns no
// tables: it reads source_connections + services (sibling reads, modules.md Rule 2) to resolve the
// mapping, and drives the deployments service through consumer-defined ports. See
// docs/architecture/security.md and docs/architecture/deployment-engine.md.
package webhooks

import "context"

// Pull-request actions Plorigo acts on. Any other action (labeled, assigned, edited, …) is ignored.
const (
	ActionOpened      = "opened"
	ActionSynchronize = "synchronize"
	ActionReopened    = "reopened"
	ActionClosed      = "closed"
)

// PullRequestEvent is the subset of a GitHub `pull_request` webhook the handler parses and acts on.
type PullRequestEvent struct {
	Action         string
	InstallationID string
	Owner          string
	Repo           string
	PRNumber       int32
	Merged         bool
}

// HandleResult summarizes what a delivery did, for the HTTP response and logs.
type HandleResult struct {
	Action          string
	MatchedServices int
	Created         int
	TornDown        int
	Ignored         string // a reason when nothing was done (unknown installation, no matching service, …)
}

// PreviewCreator and PreviewTeardowner are the CONSUMER-DEFINED ports the webhook needs from the
// deployments module. *deployments.Service satisfies both structurally — webhooks never imports
// deployments. Both take primitive params (no deployments types) so structural typing holds.
type PreviewCreator interface {
	CreatePreviewForPR(ctx context.Context, serviceID string, prNumber int32) (string, error)
}

// PreviewTeardowner removes a service's PR preview (see PreviewCreator).
type PreviewTeardowner interface {
	TeardownPreviewForPR(ctx context.Context, serviceID string, prNumber int32) error
}

// Service handles one verified pull_request event.
type Service interface {
	HandlePullRequest(ctx context.Context, e PullRequestEvent) (HandleResult, error)
}

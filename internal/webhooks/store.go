package webhooks

import "context"

// Store is the repository port the webhook service needs. Implemented by postgres.go, faked in
// tests. Both methods are sibling-table reads (source_connections, services) that modules.md Rule 2
// permits from a module's postgres.go.
type Store interface {
	// WorkspaceForInstallation maps a GitHub App installation id to its workspace, so a delivery is
	// scoped to the workspace that connected the installation. ok is false (nil error) for an
	// unknown/unconnected installation — the delivery is then ignored.
	WorkspaceForInstallation(ctx context.Context, installationID string) (workspaceID string, ok bool, err error)
	// ServicesForRepo returns the ids of git services in a workspace whose source is owner/repo
	// (case-insensitive). A repo may back more than one service.
	ServicesForRepo(ctx context.Context, workspaceID, owner, repo string) ([]string, error)
}

package projects

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// service is the business logic. It orchestrates ports only — no SQL, no transport.
type service struct {
	tx    TxRunner
	store Store
	audit Recorder
	log   *slog.Logger
}

func newService(tx TxRunner, store Store, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, audit: audit, log: log}
}

func (s *service) Create(ctx context.Context, in CreateInput) (Project, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Project{}, problem.InvalidInput("project name is required")
	}
	if in.WorkspaceID == "" {
		return Project{}, problem.InvalidInput("workspace_id is required")
	}

	// AUTHORIZATION SEAM: a privileged module would call policy.Authorize(ctx, ...)
	// here before mutating. projects is unprivileged in the scaffold. Do NOT copy this
	// template for a privileged module until the policy port exists — see
	// docs/architecture/modules.md.

	slug := slugify(name)
	if slug == "" {
		return Project{}, problem.InvalidInput("project name must contain at least one letter or number")
	}

	actor := in.Actor
	if actor == "" {
		actor = "system"
	}

	candidate := Project{
		WorkspaceID: in.WorkspaceID,
		Name:        name,
		Slug:        slug,
	}

	var created Project
	err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		created, txErr = s.store.InsertProject(ctx, tx, candidate)
		if txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the project row:
		// there is no project without its audit trail.
		return s.audit.Record(ctx, tx, "project.create", "project", created.ID, created.WorkspaceID, actor)
	})
	if err != nil {
		// Preserve domain errors (e.g. AlreadyExists from a unique violation); only
		// genuinely unexpected errors become Internal. Wrapping unconditionally would
		// mask the real kind because ToConnect matches the outermost *problem.Error.
		var pe *problem.Error
		if errors.As(err, &pe) {
			return Project{}, err
		}
		return Project{}, problem.Internalf(err, "create project")
	}

	s.log.Info("project created", "id", created.ID, "workspace_id", created.WorkspaceID)
	return created, nil
}

func (s *service) Get(ctx context.Context, projectID string) (Project, error) {
	if _, err := id.Parse(projectID); err != nil {
		return Project{}, problem.InvalidInput("invalid project id")
	}
	return s.store.GetProject(ctx, projectID)
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Project, error) {
	if workspaceID == "" {
		return nil, problem.InvalidInput("workspace_id is required")
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

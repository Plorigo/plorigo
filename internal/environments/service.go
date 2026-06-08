package environments

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// defaultEnvType is assigned when the caller does not specify a type. "preview" is
// the safest default (preview first, production second — see principles.md).
const defaultEnvType = "preview"

// service is the business logic. It orchestrates ports only — no SQL, no transport.
// Authorization is workspace-scoped, so the owning workspace is resolved from the
// parent project; every mutation authorizes the caller (via the authz.Authorizer
// port) before the WithinTx block and audits inside it (see modules.md, Rule 4).
type service struct {
	tx         TxRunner
	store      Store
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

func (s *service) Create(ctx context.Context, in CreateInput) (Environment, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Environment{}, problem.InvalidInput("environment name is required")
	}
	if _, err := id.Parse(in.ProjectID); err != nil {
		return Environment{}, problem.InvalidInput("a valid project_id is required")
	}
	envType := strings.TrimSpace(in.Type)
	if envType == "" {
		envType = defaultEnvType
	}
	if !validEnvType(envType) {
		return Environment{}, problem.InvalidInput("type must be one of preview, staging, production, custom")
	}

	// Resolve the owning workspace through the parent project — authorization and
	// auditing are workspace-scoped.
	workspaceID, ok, err := s.store.WorkspaceIDForProject(ctx, in.ProjectID)
	if err != nil {
		return Environment{}, problem.Internalf(err, "create environment")
	}
	if !ok {
		return Environment{}, problem.NotFound("project %s not found", in.ProjectID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionEnvironmentCreate, authz.Resource{Type: "environment", WorkspaceID: workspaceID}); err != nil {
		return Environment{}, err
	}

	slug := slugify(name)
	if slug == "" {
		return Environment{}, problem.InvalidInput("environment name must contain at least one letter or number")
	}
	candidate := Environment{ProjectID: in.ProjectID, WorkspaceID: workspaceID, Name: name, Slug: slug, Type: envType}

	var created Environment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if created, txErr = s.store.InsertEnvironment(ctx, tx, candidate); txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the environment row,
		// with the real authenticated actor.
		return s.audit.Record(ctx, tx, "environment.create", "environment", created.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Environment{}, mapErr(err, "create environment")
	}
	created.WorkspaceID = workspaceID // not returned by the insert row; set for completeness
	s.log.Info("environment created", "id", created.ID, "project_id", created.ProjectID, "workspace_id", workspaceID, "actor", caller.UserID)
	return created, nil
}

func (s *service) Get(ctx context.Context, envID string) (Environment, error) {
	if _, err := id.Parse(envID); err != nil {
		return Environment{}, problem.InvalidInput("invalid environment id")
	}
	env, err := s.store.GetEnvironment(ctx, envID)
	if err != nil {
		return Environment{}, err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionEnvironmentRead, authz.Resource{Type: "environment", WorkspaceID: env.WorkspaceID, ID: env.ID}); err != nil {
		return Environment{}, err
	}
	return env, nil
}

func (s *service) ListByProject(ctx context.Context, projectID string) ([]Environment, error) {
	if _, err := id.Parse(projectID); err != nil {
		return nil, problem.InvalidInput("a valid project_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForProject(ctx, projectID)
	if err != nil {
		return nil, problem.Internalf(err, "list environments")
	}
	if !ok {
		return nil, problem.NotFound("project %s not found", projectID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionEnvironmentRead, authz.Resource{Type: "environment", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByProject(ctx, projectID)
}

func validEnvType(t string) bool {
	switch t {
	case "preview", "staging", "production", "custom":
		return true
	}
	return false
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an
// internal error, so a unique violation surfaces as AlreadyExists rather than being
// masked as Internal.
func mapErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pe *problem.Error
	if errors.As(err, &pe) {
		return err
	}
	return problem.Internalf(err, "%s", op)
}

package servers

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

// service is the business logic. It orchestrates ports only — no SQL, no transport.
// Every mutation authorizes the caller (via the authz.Authorizer port) before the
// WithinTx block, and audits inside it (see docs/architecture/modules.md, Rule 4).
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

func (s *service) Create(ctx context.Context, in CreateInput) (Server, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Server{}, problem.InvalidInput("server name is required")
	}
	if in.WorkspaceID == "" {
		return Server{}, problem.InvalidInput("workspace_id is required")
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionServerCreate, authz.Resource{Type: "server", WorkspaceID: in.WorkspaceID}); err != nil {
		return Server{}, err
	}

	slug := slugify(name)
	if slug == "" {
		return Server{}, problem.InvalidInput("server name must contain at least one letter or number")
	}
	candidate := Server{WorkspaceID: in.WorkspaceID, Name: name, Slug: slug}

	var created Server
	err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if created, txErr = s.store.InsertServer(ctx, tx, candidate); txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the server row, with the
		// real authenticated actor.
		return s.audit.Record(ctx, tx, "server.create", "server", created.ID, created.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return Server{}, mapErr(err, "create server")
	}
	s.log.Info("server created", "id", created.ID, "workspace_id", created.WorkspaceID, "actor", caller.UserID)
	return created, nil
}

func (s *service) Get(ctx context.Context, serverID string) (Server, error) {
	if _, err := id.Parse(serverID); err != nil {
		return Server{}, problem.InvalidInput("invalid server id")
	}
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServerRead, authz.Resource{Type: "server", WorkspaceID: srv.WorkspaceID, ID: srv.ID}); err != nil {
		return Server{}, err
	}
	return srv, nil
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Server, error) {
	if workspaceID == "" {
		return nil, problem.InvalidInput("workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServerRead, authz.Resource{Type: "server", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an internal
// error, so a unique violation surfaces as AlreadyExists rather than being masked as
// Internal.
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

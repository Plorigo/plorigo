package envvars

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

// Env var key/value bounds, enforced here and backed by CHECK constraints in the
// migration as defense-in-depth.
const (
	maxKeyLen   = 128
	maxValueLen = 32768
)

// envVarKeyRe is the conventional POSIX-ish shell-variable grammar: an uppercase
// letter or underscore, then uppercase letters, digits, or underscores. Keys are
// rejected (not coerced) when they don't match, mirroring how the sibling modules
// reject out-of-vocabulary input rather than silently rewriting it.
var envVarKeyRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// service is the business logic. It orchestrates ports only — no SQL, no transport.
// Authorization is workspace-scoped, so the owning workspace is resolved through the
// parent environment's project; every mutation authorizes the caller (via the
// authz.Authorizer port) before the WithinTx block and audits inside it (see
// modules.md, Rule 4). Values are non-secret but are never logged, so this module
// stays a safe template for the future secrets module.
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

func (s *service) Set(ctx context.Context, in SetInput) (EnvVar, error) {
	if _, err := id.Parse(in.EnvironmentID); err != nil {
		return EnvVar{}, problem.InvalidInput("a valid environment_id is required")
	}
	key, err := validateKey(in.Key)
	if err != nil {
		return EnvVar{}, err
	}
	if len(in.Value) > maxValueLen {
		return EnvVar{}, problem.InvalidInput("value must be at most %d bytes", maxValueLen)
	}

	// Resolve the owning workspace through the parent environment's project —
	// authorization and auditing are workspace-scoped.
	workspaceID, ok, err := s.store.WorkspaceIDForEnvironment(ctx, in.EnvironmentID)
	if err != nil {
		return EnvVar{}, problem.Internalf(err, "set env var")
	}
	if !ok {
		return EnvVar{}, problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionEnvVarSet, authz.Resource{Type: "env_var", WorkspaceID: workspaceID}); err != nil {
		return EnvVar{}, err
	}

	candidate := EnvVar{EnvironmentID: in.EnvironmentID, WorkspaceID: workspaceID, Key: key, Value: in.Value}

	var saved EnvVar
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if saved, txErr = s.store.UpsertEnvVar(ctx, tx, candidate); txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the env var row, with the
		// real authenticated actor.
		return s.audit.Record(ctx, tx, "env_var.set", "env_var", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return EnvVar{}, mapErr(err, "set env var")
	}
	saved.WorkspaceID = workspaceID
	// Log the key NAME and never the value (redaction habit shared with secrets).
	s.log.Info("env var set", "id", saved.ID, "environment_id", saved.EnvironmentID, "key", saved.Key, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

func (s *service) List(ctx context.Context, environmentID string) ([]EnvVar, error) {
	if _, err := id.Parse(environmentID); err != nil {
		return nil, problem.InvalidInput("a valid environment_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForEnvironment(ctx, environmentID)
	if err != nil {
		return nil, problem.Internalf(err, "list env vars")
	}
	if !ok {
		return nil, problem.NotFound("environment %s not found", environmentID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionEnvVarRead, authz.Resource{Type: "env_var", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	vars, err := s.store.ListByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	for i := range vars {
		vars[i].WorkspaceID = workspaceID
	}
	return vars, nil
}

func (s *service) Delete(ctx context.Context, in DeleteInput) error {
	if _, err := id.Parse(in.EnvironmentID); err != nil {
		return problem.InvalidInput("a valid environment_id is required")
	}
	key, err := validateKey(in.Key)
	if err != nil {
		return err
	}

	workspaceID, ok, err := s.store.WorkspaceIDForEnvironment(ctx, in.EnvironmentID)
	if err != nil {
		return problem.Internalf(err, "delete env var")
	}
	if !ok {
		return problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionEnvVarDelete, authz.Resource{Type: "env_var", WorkspaceID: workspaceID}); err != nil {
		return err
	}

	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteEnvVar(ctx, tx, in.EnvironmentID, key)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			// Nothing was deleted; report NotFound and do not audit a change that did
			// not happen (the tx rolls back).
			return problem.NotFound("env var %q not found", key)
		}
		return s.audit.Record(ctx, tx, "env_var.delete", "env_var", deletedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "delete env var")
	}
	s.log.Info("env var deleted", "environment_id", in.EnvironmentID, "key", key, "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

// validateKey trims and validates an env var key. It rejects (rather than coerces)
// keys outside the conventional grammar, returning an InvalidInput problem.
func validateKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", problem.InvalidInput("env var key is required")
	}
	if len(key) > maxKeyLen {
		return "", problem.InvalidInput("key must be at most %d characters", maxKeyLen)
	}
	if !envVarKeyRe.MatchString(key) {
		return "", problem.InvalidInput("key must match %s (e.g. DATABASE_URL)", envVarKeyRe.String())
	}
	return key, nil
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an
// internal error, so a domain error from the store/audit surfaces unchanged rather
// than being masked as Internal.
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

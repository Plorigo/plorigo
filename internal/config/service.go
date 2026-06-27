package config

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

// Key/value bounds. The key grammar and length mirror the old envvars/secrets modules and
// are backed by CHECK constraints in the migration as defense-in-depth. maxValueLen bounds
// the PLAINTEXT before sealing; the sealed bytes are separately bounded by a CHECK on the
// ciphertext column.
const (
	maxKeyLen   = 128
	maxValueLen = 32768
)

// keyRe is the conventional POSIX-ish shell-variable grammar: an uppercase letter or
// underscore, then uppercase letters, digits, or underscores. Keys are rejected (not
// coerced) when they don't match.
var keyRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// service is the business logic. It orchestrates ports only — no SQL, no transport, and no
// cryptography of its own (it seals through the Sealer port). Authorization is
// workspace-scoped, resolved through the service or the environment's project; every
// mutation authorizes the caller before the WithinTx block and audits inside it (modules.md,
// Rule 4). A secret's plaintext is sealed before storage and is NEVER logged or returned.
type service struct {
	tx         TxRunner
	store      Store
	sealer     Sealer
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, sealer Sealer, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, sealer: sealer, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

func (s *service) Set(ctx context.Context, in SetInput) (Entry, error) {
	typ, err := validateType(in.Type)
	if err != nil {
		return Entry{}, err
	}
	scope, err := validateScope(in.Scope)
	if err != nil {
		return Entry{}, err
	}
	key, err := validateKey(in.Key)
	if err != nil {
		return Entry{}, err
	}
	if len(in.Value) > maxValueLen {
		return Entry{}, problem.InvalidInput("value must be at most %d bytes", maxValueLen)
	}

	workspaceID, scopeID, err := s.resolveScope(ctx, scope, in.ServiceID, in.EnvironmentID)
	if err != nil {
		return Entry{}, err
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionConfigSet, authz.Resource{Type: "config", WorkspaceID: workspaceID}); err != nil {
		return Entry{}, err
	}

	// Variables store plaintext; secrets are sealed BEFORE the store ever sees them, so the
	// store only handles ciphertext and the plaintext is not retained past this call.
	var value *string
	var ciphertext []byte
	if typ == TypeSecret {
		ciphertext, err = s.sealer.Seal([]byte(in.Value))
		if err != nil {
			return Entry{}, problem.Internalf(err, "seal secret")
		}
	} else {
		v := in.Value
		value = &v
	}

	var saved Entry
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if scope == ScopeService {
			saved, txErr = s.store.UpsertServiceConfig(ctx, tx, typ, scopeID, key, value, ciphertext)
		} else {
			saved, txErr = s.store.UpsertEnvironmentConfig(ctx, tx, typ, scopeID, key, value, ciphertext)
		}
		if txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the config row.
		return s.audit.Record(ctx, tx, "config.set", "config", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Entry{}, mapErr(err, "set config")
	}
	saved.WorkspaceID = workspaceID
	// Log the key + metadata, never a value (variable values can be sensitive too).
	s.log.Info("config set", "id", saved.ID, "type", saved.Type, "scope", saved.Scope, "key", saved.Key, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

func (s *service) ListForService(ctx context.Context, serviceID string) ([]Entry, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return nil, problem.InvalidInput("a valid service_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForService(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list config")
	}
	if !ok {
		return nil, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionConfigRead, authz.Resource{Type: "config", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	entries, err := s.store.ListForService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	// Both the service-level and the environment-shared entries belong to the same workspace.
	for i := range entries {
		entries[i].WorkspaceID = workspaceID
	}
	return entries, nil
}

func (s *service) Delete(ctx context.Context, in DeleteInput) error {
	scope, err := validateScope(in.Scope)
	if err != nil {
		return err
	}
	key, err := validateKey(in.Key)
	if err != nil {
		return err
	}

	workspaceID, scopeID, err := s.resolveScope(ctx, scope, in.ServiceID, in.EnvironmentID)
	if err != nil {
		return err
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionConfigDelete, authz.Resource{Type: "config", WorkspaceID: workspaceID}); err != nil {
		return err
	}

	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var deletedID string
		var deleted bool
		var txErr error
		if scope == ScopeService {
			deletedID, deleted, txErr = s.store.DeleteServiceConfig(ctx, tx, scopeID, key)
		} else {
			deletedID, deleted, txErr = s.store.DeleteEnvironmentConfig(ctx, tx, scopeID, key)
		}
		if txErr != nil {
			return txErr
		}
		if !deleted {
			// Nothing was deleted; report NotFound and do not audit a change that did not
			// happen (the tx rolls back).
			return problem.NotFound("config %q not found", key)
		}
		return s.audit.Record(ctx, tx, "config.delete", "config", deletedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "delete config")
	}
	s.log.Info("config deleted", "scope", scope, "key", key, "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

// resolveScope validates the scope target id and resolves the owning workspace, returning
// the workspace id and the scope target id (the service or environment id) for the upsert or
// delete.
func (s *service) resolveScope(ctx context.Context, scope Scope, serviceID, environmentID string) (workspaceID, scopeID string, err error) {
	switch scope {
	case ScopeService:
		if _, perr := id.Parse(serviceID); perr != nil {
			return "", "", problem.InvalidInput("a valid service_id is required for service scope")
		}
		ws, ok, rerr := s.store.WorkspaceIDForService(ctx, serviceID)
		if rerr != nil {
			return "", "", problem.Internalf(rerr, "resolve service")
		}
		if !ok {
			return "", "", problem.NotFound("service %s not found", serviceID)
		}
		return ws, serviceID, nil
	case ScopeEnvironment:
		if _, perr := id.Parse(environmentID); perr != nil {
			return "", "", problem.InvalidInput("a valid environment_id is required for environment scope")
		}
		ws, ok, rerr := s.store.WorkspaceIDForEnvironment(ctx, environmentID)
		if rerr != nil {
			return "", "", problem.Internalf(rerr, "resolve environment")
		}
		if !ok {
			return "", "", problem.NotFound("environment %s not found", environmentID)
		}
		return ws, environmentID, nil
	default:
		return "", "", problem.InvalidInput("scope must be service or environment")
	}
}

func validateType(t Type) (Type, error) {
	switch t {
	case TypeVariable, TypeSecret:
		return t, nil
	default:
		return "", problem.InvalidInput("type must be variable or secret")
	}
}

func validateScope(sc Scope) (Scope, error) {
	switch sc {
	case ScopeService, ScopeEnvironment:
		return sc, nil
	default:
		return "", problem.InvalidInput("scope must be service or environment")
	}
}

// validateKey trims and validates a config key. It rejects (rather than coerces) keys
// outside the conventional grammar, returning an InvalidInput problem.
func validateKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", problem.InvalidInput("config key is required")
	}
	if len(key) > maxKeyLen {
		return "", problem.InvalidInput("key must be at most %d characters", maxKeyLen)
	}
	if !keyRe.MatchString(key) {
		return "", problem.InvalidInput("key must match %s (e.g. DATABASE_URL)", keyRe.String())
	}
	return key, nil
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an internal
// error, so a domain error from the store/audit surfaces unchanged rather than being masked.
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

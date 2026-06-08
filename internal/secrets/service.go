package secrets

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

// Secret key/value bounds. The key grammar and length mirror envvars (and are backed
// by a CHECK constraint in the migration as defense-in-depth). maxValueLen bounds the
// PLAINTEXT before sealing; the sealed bytes are separately bounded by a CHECK on the
// ciphertext column.
const (
	maxKeyLen   = 128
	maxValueLen = 32768
)

// secretKeyRe is the conventional POSIX-ish shell-variable grammar, identical to env
// vars: an uppercase letter or underscore, then uppercase letters, digits, or
// underscores. Keys are rejected (not coerced) when they don't match.
var secretKeyRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// service is the business logic. It orchestrates ports only — no SQL, no transport,
// and no cryptography of its own (it seals through the Sealer port). Authorization is
// workspace-scoped, resolved through the parent environment's project; every mutation
// authorizes the caller before the WithinTx block and audits inside it (modules.md,
// Rule 4). The plaintext value is sealed before storage and is NEVER logged or
// returned — only the key name appears in logs.
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

func (s *service) Set(ctx context.Context, in SetInput) (Secret, error) {
	if _, err := id.Parse(in.EnvironmentID); err != nil {
		return Secret{}, problem.InvalidInput("a valid environment_id is required")
	}
	key, err := validateKey(in.Key)
	if err != nil {
		return Secret{}, err
	}
	if len(in.Value) > maxValueLen {
		return Secret{}, problem.InvalidInput("value must be at most %d bytes", maxValueLen)
	}

	// Resolve the owning workspace through the parent environment's project —
	// authorization and auditing are workspace-scoped.
	workspaceID, ok, err := s.store.WorkspaceIDForEnvironment(ctx, in.EnvironmentID)
	if err != nil {
		return Secret{}, problem.Internalf(err, "set secret")
	}
	if !ok {
		return Secret{}, problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSecretSet, authz.Resource{Type: "secret", WorkspaceID: workspaceID}); err != nil {
		return Secret{}, err
	}

	// Seal the plaintext BEFORE it touches the store — the store only ever sees
	// ciphertext, and the plaintext is not retained past this call.
	ciphertext, err := s.sealer.Seal([]byte(in.Value))
	if err != nil {
		return Secret{}, problem.Internalf(err, "seal secret")
	}

	var saved Secret
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if saved, txErr = s.store.UpsertSecret(ctx, tx, in.EnvironmentID, key, ciphertext); txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the secret row, with the
		// real authenticated actor.
		return s.audit.Record(ctx, tx, "secret.set", "secret", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Secret{}, mapErr(err, "set secret")
	}
	saved.WorkspaceID = workspaceID
	// Log the key NAME and never the value — secrets are write-only and redacted.
	s.log.Info("secret set", "id", saved.ID, "environment_id", saved.EnvironmentID, "key", saved.Key, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

func (s *service) List(ctx context.Context, environmentID string) ([]Secret, error) {
	if _, err := id.Parse(environmentID); err != nil {
		return nil, problem.InvalidInput("a valid environment_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForEnvironment(ctx, environmentID)
	if err != nil {
		return nil, problem.Internalf(err, "list secrets")
	}
	if !ok {
		return nil, problem.NotFound("environment %s not found", environmentID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSecretList, authz.Resource{Type: "secret", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	secs, err := s.store.ListByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	for i := range secs {
		secs[i].WorkspaceID = workspaceID
	}
	return secs, nil
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
		return problem.Internalf(err, "delete secret")
	}
	if !ok {
		return problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSecretDelete, authz.Resource{Type: "secret", WorkspaceID: workspaceID}); err != nil {
		return err
	}

	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteSecret(ctx, tx, in.EnvironmentID, key)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			// Nothing was deleted; report NotFound and do not audit a change that did
			// not happen (the tx rolls back).
			return problem.NotFound("secret %q not found", key)
		}
		return s.audit.Record(ctx, tx, "secret.delete", "secret", deletedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "delete secret")
	}
	s.log.Info("secret deleted", "environment_id", in.EnvironmentID, "key", key, "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

// validateKey trims and validates a secret key. It rejects (rather than coerces) keys
// outside the conventional grammar, returning an InvalidInput problem.
func validateKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", problem.InvalidInput("secret key is required")
	}
	if len(key) > maxKeyLen {
		return "", problem.InvalidInput("key must be at most %d characters", maxKeyLen)
	}
	if !secretKeyRe.MatchString(key) {
		return "", problem.InvalidInput("key must match %s (e.g. STRIPE_SECRET_KEY)", secretKeyRe.String())
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

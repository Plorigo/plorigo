package serversetup

import (
	"context"
	"errors"
	"log/slog"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// service is the business logic. It orchestrates ports only — no SQL, no transport, and no
// cryptography of its own (it seals/opens through the Sealer port and generates keys
// through the KeyGenerator port). Authorization is workspace-scoped, resolved from the
// owning server; every mutation authorizes the caller before the WithinTx block and audits
// the real actor inside it (modules.md, Rule 4). Private key material is sealed before it
// reaches the store and is NEVER logged or returned — only the fingerprint and key name
// appear in logs.
type service struct {
	tx         TxRunner
	store      Store
	keys       KeyGenerator
	sealer     Sealer
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, keys KeyGenerator, sealer Sealer, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, keys: keys, sealer: sealer, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

// Provision generates a fresh management keypair, seals the private key, and stores it as
// the server's credential (replacing any prior one). It returns non-secret metadata
// including the public key to install on the server. Called by the bootstrap runner while
// it still holds the raw bootstrap credential; not exposed as an RPC.
func (s *service) Provision(ctx context.Context, in ProvisionInput) (Credential, error) {
	workspaceID, caller, err := s.authorizeServer(ctx, in.ServerID, authz.ActionServerSetupRun)
	if err != nil {
		return Credential{}, err
	}

	kp, err := s.keys.Generate()
	if err != nil {
		return Credential{}, problem.Internalf(err, "generate management key")
	}
	// Seal the private key BEFORE it touches the store — the store only ever sees
	// ciphertext, and the plaintext key is not retained past this call.
	sealed, err := s.sealer.Seal(kp.PrivatePEM)
	if err != nil {
		return Credential{}, problem.Internalf(err, "seal management key")
	}

	var saved Credential
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if saved, txErr = s.store.Upsert(ctx, tx, UpsertParams{
			ServerID:         in.ServerID,
			Fingerprint:      kp.Fingerprint,
			PublicKey:        kp.AuthorizedKey,
			SealedPrivateKey: sealed,
			CreatedBy:        nilIfEmpty(caller.UserID),
		}); txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "server_setup.key.provision", "ssh_management_key", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Credential{}, mapErr(err, "provision management key")
	}
	saved.WorkspaceID = workspaceID
	// Log the fingerprint (a public identifier) and never the key — the private key is
	// write-only and redacted everywhere.
	s.log.Info("ssh management key provisioned", "id", saved.ID, "server_id", saved.ServerID, "fingerprint", saved.Fingerprint, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

// Rotate replaces the active credential's key material with a fresh keypair. A missing or
// revoked credential yields NotFound, so a rotation never silently strands a server. This is
// in-process only (no RPC): the bootstrap runner must install the new public key on the
// server and only then commit the rotation, so the stored credential never gets ahead of the
// server's authorized_keys (a standalone rotate would leave the control plane holding a key
// the server no longer trusts). It returns the new public key for the runner to install.
func (s *service) Rotate(ctx context.Context, in RotateInput) (Credential, error) {
	workspaceID, caller, err := s.authorizeServer(ctx, in.ServerID, authz.ActionServerSetupKeyRotate)
	if err != nil {
		return Credential{}, err
	}

	kp, err := s.keys.Generate()
	if err != nil {
		return Credential{}, problem.Internalf(err, "generate management key")
	}
	sealed, err := s.sealer.Seal(kp.PrivatePEM)
	if err != nil {
		return Credential{}, problem.Internalf(err, "seal management key")
	}

	var saved Credential
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		cred, ok, txErr := s.store.Rotate(ctx, tx, RotateParams{
			ServerID:         in.ServerID,
			Fingerprint:      kp.Fingerprint,
			PublicKey:        kp.AuthorizedKey,
			SealedPrivateKey: sealed,
		})
		if txErr != nil {
			return txErr
		}
		if !ok {
			return problem.NotFound("server %s has no active management credential to rotate", in.ServerID)
		}
		saved = cred
		return s.audit.Record(ctx, tx, "server_setup.key.rotate", "ssh_management_key", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Credential{}, mapErr(err, "rotate management key")
	}
	saved.WorkspaceID = workspaceID
	s.log.Info("ssh management key rotated", "id", saved.ID, "server_id", saved.ServerID, "fingerprint", saved.Fingerprint, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

// Revoke cuts off the management channel by marking the credential revoked. This is in-process
// only (no RPC): on its own it records intent so the runner refuses to use the key, but it
// does NOT remove the public key from the server's authorized_keys — the bootstrap runner must
// remove the on-server key as part of the same operation for revocation to actually deny
// access. Revoking a missing/already-revoked credential is NotFound and not audited.
func (s *service) Revoke(ctx context.Context, in RevokeInput) error {
	workspaceID, caller, err := s.authorizeServer(ctx, in.ServerID, authz.ActionServerSetupKeyRevoke)
	if err != nil {
		return err
	}

	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		revokedID, ok, txErr := s.store.Revoke(ctx, tx, in.ServerID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return problem.NotFound("server %s has no active management credential to revoke", in.ServerID)
		}
		return s.audit.Record(ctx, tx, "server_setup.key.revoke", "ssh_management_key", revokedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "revoke management key")
	}
	s.log.Info("ssh management key revoked", "server_id", in.ServerID, "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

// Get returns the credential's non-secret metadata. Authorized (read), never audited.
func (s *service) Get(ctx context.Context, serverID string) (Credential, error) {
	workspaceID, _, err := s.authorizeServer(ctx, serverID, authz.ActionServerSetupKeyRead)
	if err != nil {
		return Credential{}, err
	}
	cred, ok, err := s.store.Get(ctx, serverID)
	if err != nil {
		return Credential{}, problem.Internalf(err, "get management key")
	}
	if !ok {
		return Credential{}, problem.NotFound("server %s has no management credential", serverID)
	}
	cred.WorkspaceID = workspaceID
	return cred, nil
}

// MarkUsed stamps last_used_at after a successful SSH connection and audits the use. Called
// by the bootstrap runner; not an RPC.
func (s *service) MarkUsed(ctx context.Context, in UseInput) error {
	workspaceID, caller, err := s.authorizeServer(ctx, in.ServerID, authz.ActionServerSetupRun)
	if err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		usedID, ok, txErr := s.store.MarkUsed(ctx, tx, in.ServerID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return problem.NotFound("server %s has no active management credential", in.ServerID)
		}
		return s.audit.Record(ctx, tx, "server_setup.key.use", "ssh_management_key", usedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "mark management key used")
	}
	return nil
}

// RecordFailedAuth audits a failed SSH authentication against a server. There may be no
// usable credential, so it mutates no key row — it only writes the audit event (with a
// short, redacted reason in the log). Called by the bootstrap runner; not an RPC.
func (s *service) RecordFailedAuth(ctx context.Context, in FailedAuthInput) error {
	workspaceID, caller, err := s.authorizeServer(ctx, in.ServerID, authz.ActionServerSetupRun)
	if err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		return s.audit.Record(ctx, tx, "server_setup.failed_auth", "server", in.ServerID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "record failed auth")
	}
	// Reason is a short, non-sensitive hint; the audit row carries the actor/scope and the
	// log carries the hint. Neither carries any credential.
	s.log.Warn("ssh management auth failed", "server_id", in.ServerID, "reason", in.Reason, "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

// OpenPrivateKey opens the sealed private key for an active credential so the SSH runner can
// connect. It is in-process only — there is deliberately no RPC for it, and the opened bytes
// are never logged. Authorized like a setup run.
func (s *service) OpenPrivateKey(ctx context.Context, serverID string) ([]byte, error) {
	if _, _, err := s.authorizeServer(ctx, serverID, authz.ActionServerSetupRun); err != nil {
		return nil, err
	}
	sealed, ok, err := s.store.GetSealed(ctx, serverID)
	if err != nil {
		return nil, problem.Internalf(err, "open management key")
	}
	if !ok {
		return nil, problem.NotFound("server %s has no active management credential", serverID)
	}
	plaintext, err := s.sealer.Open(sealed)
	if err != nil {
		return nil, problem.Internalf(err, "open management key")
	}
	return plaintext, nil
}

// authorizeServer validates the server id, resolves its owning workspace, and authorizes
// the caller for action against that workspace — the shared preamble for every method.
func (s *service) authorizeServer(ctx context.Context, serverID string, action authz.Action) (string, principal.Principal, error) {
	if _, err := id.Parse(serverID); err != nil {
		return "", principal.Principal{}, problem.InvalidInput("a valid server_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForServer(ctx, serverID)
	if err != nil {
		return "", principal.Principal{}, problem.Internalf(err, "resolve server workspace")
	}
	if !ok {
		return "", principal.Principal{}, problem.NotFound("server %s not found", serverID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, action, authz.Resource{Type: "ssh_management_key", WorkspaceID: workspaceID}); err != nil {
		return "", principal.Principal{}, err
	}
	return workspaceID, caller, nil
}

// nilIfEmpty maps an empty actor to a NULL created_by so the column is honest about
// "unknown" rather than storing a blank id.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as Internal, so a
// domain error from the store/audit surfaces unchanged rather than masked.
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

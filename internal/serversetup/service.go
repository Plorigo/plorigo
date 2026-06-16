package serversetup

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

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

	// Setup-run collaborators (the dashboard-managed bootstrap over SSH).
	dialer          SSHDialer
	agents          AgentProvisioner
	controlPlaneURL string
	// Tunables for the async bootstrap; defaulted in newService.
	heartbeatAttempts int
	heartbeatDelay    time.Duration
	setupTimeout      time.Duration
}

func newService(tx TxRunner, store Store, keys KeyGenerator, sealer Sealer, authorizer authz.Authorizer, audit Recorder, log *slog.Logger, dialer SSHDialer, agents AgentProvisioner, controlPlaneURL string) *service {
	return &service{
		tx: tx, store: store, keys: keys, sealer: sealer, authorizer: authorizer, audit: audit, log: log,
		dialer: dialer, agents: agents, controlPlaneURL: controlPlaneURL,
		// ~2 minutes of heartbeat polling (the agent's online window is 90s), and a generous
		// overall ceiling since the installer pulls packages over the network.
		heartbeatAttempts: 40,
		heartbeatDelay:    3 * time.Second,
		setupTimeout:      15 * time.Minute,
	}
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
// revoked credential yields NotFound, so a rotation never silently strands a server. The
// on-server install of the new public key is the bootstrap runner's job; this updates the
// stored credential and returns the new public key for it to install.
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

// Revoke cuts off the management channel by marking the credential revoked. Removing the
// public key from the server's authorized_keys is the bootstrap runner's job; this records
// the intent so the runner refuses to use it. Revoking a missing/already-revoked credential
// is NotFound and not audited.
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

// StartSetup begins an asynchronous dashboard-managed bootstrap over SSH. It validates the
// request, creates a run (auditing start vs retry), then dispatches the bootstrap in the
// background. The one-time bootstrap credential is used only by that goroutine and never stored.
func (s *service) StartSetup(ctx context.Context, in StartSetupInput) (SetupRun, error) {
	if strings.TrimSpace(in.Host) == "" {
		return SetupRun{}, problem.InvalidInput("a host is required")
	}
	if strings.TrimSpace(in.Username) == "" {
		return SetupRun{}, problem.InvalidInput("a username is required")
	}
	if in.Auth.Password == "" && len(in.Auth.PrivateKey) == 0 {
		return SetupRun{}, problem.InvalidInput("a bootstrap password or private key is required")
	}
	workspaceID, caller, err := s.authorizeServer(ctx, in.ServerID, authz.ActionServerSetupRun)
	if err != nil {
		return SetupRun{}, err
	}

	priorRuns, err := s.store.CountSetupRuns(ctx, in.ServerID)
	if err != nil {
		return SetupRun{}, problem.Internalf(err, "start setup")
	}
	action := "server_setup.run.start"
	if priorRuns > 0 {
		action = "server_setup.run.retry"
	}

	var run SetupRun
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if run, txErr = s.store.InsertSetupRun(ctx, tx, in.ServerID, workspaceID, nilIfEmpty(caller.UserID)); txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, action, "server_setup_run", run.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return SetupRun{}, mapErr(err, "start setup")
	}
	run.WorkspaceID = workspaceID
	s.log.Info("server setup run started", "setup_run_id", run.ID, "server_id", in.ServerID, "workspace_id", workspaceID, "actor", caller.UserID)

	go s.runSetup(in, run, caller, workspaceID)
	return run, nil
}

// runSetup performs the bootstrap in the background. It carries the caller's principal (so its
// authorized actions run as the initiating user) on a context detached from the request but
// bounded by an overall ceiling. The bootstrap credential lives only on the stack here.
func (s *service) runSetup(in StartSetupInput, run SetupRun, caller principal.Principal, workspaceID string) {
	ctx, cancel := context.WithTimeout(principal.NewContext(context.Background(), caller), s.setupTimeout)
	defer cancel()

	emit := func(step, kind, status, message string) {
		if _, err := s.store.AppendSetupEvent(ctx, NewSetupEvent{SetupRunID: run.ID, Step: step, Kind: kind, Status: status, Message: message}); err != nil {
			s.log.Warn("append setup event failed", "setup_run_id", run.ID, "err", err)
		}
	}
	if _, _, err := s.store.SetSetupRunStatus(ctx, run.ID, "running", ""); err != nil {
		s.log.Warn("set setup run running failed", "setup_run_id", run.ID, "err", err)
	}

	emit("connect", "status", "started", "Connecting to the server over SSH…")
	pinned, _, _ := s.store.HostKeyFingerprint(ctx, in.ServerID)
	exec, fingerprint, err := s.dialer.Dial(ctx, DialTarget{
		Host: in.Host, Port: in.Port, Username: in.Username,
		Password: in.Auth.Password, PrivateKey: in.Auth.PrivateKey, PrivateKeyPassphrase: in.Auth.PrivateKeyPassphrase,
		PinnedHostKeyFingerprint: pinned,
	})
	if err != nil {
		reason := connectReason(err)
		if errors.Is(err, ErrAuth) {
			_ = s.recordAudit(ctx, "server_setup.failed_auth", "server", in.ServerID, workspaceID, caller.UserID)
		}
		emit("connect", "status", "failed", reason)
		s.finishRun(ctx, run.ID, "failed", reason, workspaceID, caller.UserID)
		return
	}
	defer func() { _ = exec.Close() }()

	if pinned == "" {
		if err := s.store.SetHostKeyFingerprint(ctx, in.ServerID, fingerprint); err != nil {
			s.log.Warn("pin host key failed", "server_id", in.ServerID, "err", err)
		}
		emit("connect", "log", "", "Pinned the server's host key: "+fingerprint)
	}
	emit("connect", "status", "ok", "Connected. Host key "+fingerprint+".")

	runner := &Runner{
		exec: exec,
		emit: emit,
		audit: func(c context.Context, act string) {
			_ = s.recordAudit(c, act, "server_setup_run", run.ID, workspaceID, caller.UserID)
		},
		agents: s.agents,
		provisionKey: func(c context.Context) (string, error) {
			cred, perr := s.Provision(c, ProvisionInput{ServerID: in.ServerID})
			if perr != nil {
				return "", perr
			}
			return cred.PublicKey, nil
		},
		workspaceID:       workspaceID,
		serverID:          in.ServerID,
		controlPlaneURL:   s.controlPlaneURL,
		heartbeatAttempts: s.heartbeatAttempts,
		heartbeatDelay:    s.heartbeatDelay,
		sleep:             func(d time.Duration) { time.Sleep(d) },
	}
	if reason := runner.Run(ctx); reason != "" {
		s.finishRun(ctx, run.ID, "failed", reason, workspaceID, caller.UserID)
		return
	}
	s.finishRun(ctx, run.ID, "succeeded", "", workspaceID, caller.UserID)
}

// finishRun records the terminal status + audit on a context that survives the run's timeout,
// so a timed-out or canceled run still persists its outcome.
func (s *service) finishRun(ctx context.Context, runID, status, reason, workspaceID, actor string) {
	wctx := context.WithoutCancel(ctx)
	if _, _, err := s.store.SetSetupRunStatus(wctx, runID, status, reason); err != nil {
		s.log.Warn("set setup run terminal status failed", "setup_run_id", runID, "status", status, "err", err)
	}
	action := "server_setup.run.succeeded"
	if status == "failed" {
		action = "server_setup.run.failed"
	}
	_ = s.recordAudit(wctx, action, "server_setup_run", runID, workspaceID, actor)
	s.log.Info("server setup run finished", "setup_run_id", runID, "status", status)
}

func (s *service) recordAudit(ctx context.Context, action, targetType, targetID, workspaceID, actor string) error {
	return s.tx.WithinTx(ctx, func(tx database.Tx) error {
		return s.audit.Record(ctx, tx, action, targetType, targetID, workspaceID, actor)
	})
}

// GetSetupRun returns a run's status. Authorized (read) against the run's workspace.
func (s *service) GetSetupRun(ctx context.Context, setupRunID string) (SetupRun, error) {
	if _, err := id.Parse(setupRunID); err != nil {
		return SetupRun{}, problem.InvalidInput("a valid setup_run_id is required")
	}
	run, ok, err := s.store.GetSetupRun(ctx, setupRunID)
	if err != nil {
		return SetupRun{}, problem.Internalf(err, "get setup run")
	}
	if !ok {
		return SetupRun{}, problem.NotFound("setup run %s not found", setupRunID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServerSetupKeyRead, authz.Resource{Type: "server_setup_run", WorkspaceID: run.WorkspaceID}); err != nil {
		return SetupRun{}, err
	}
	return run, nil
}

// ListSetupEvents returns a run's ordered events after a seq cursor. Authorized (read).
func (s *service) ListSetupEvents(ctx context.Context, setupRunID string, afterSeq int64) ([]SetupEvent, error) {
	if _, err := id.Parse(setupRunID); err != nil {
		return nil, problem.InvalidInput("a valid setup_run_id is required")
	}
	if afterSeq < 0 {
		afterSeq = 0
	}
	run, ok, err := s.store.GetSetupRun(ctx, setupRunID)
	if err != nil {
		return nil, problem.Internalf(err, "list setup events")
	}
	if !ok {
		return nil, problem.NotFound("setup run %s not found", setupRunID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServerSetupKeyRead, authz.Resource{Type: "server_setup_run", WorkspaceID: run.WorkspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListSetupEvents(ctx, setupRunID, afterSeq)
}

// connectReason maps a dial error to a plain-English, credential-free reason.
func connectReason(err error) string {
	switch {
	case errors.Is(err, ErrHostKeyMismatch):
		return "the server's host key changed since it was first pinned — refusing to connect. If you rebuilt the server, revoke its access and retry to re-pin."
	case errors.Is(err, ErrAuth):
		return "authentication failed. Check the username and the password or private key, then retry."
	default:
		return "could not connect to the server over SSH. Check the host, port, and that the machine is reachable, then retry."
	}
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

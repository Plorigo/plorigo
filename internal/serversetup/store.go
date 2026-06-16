package serversetup

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/sshkeys"
)

// UpsertParams provisions (or re-provisions) a server's credential, replacing key material.
type UpsertParams struct {
	ServerID         string
	Fingerprint      string
	PublicKey        string
	SealedPrivateKey []byte
	CreatedBy        *string
}

// RotateParams replaces the key material of an existing active credential.
type RotateParams struct {
	ServerID         string
	Fingerprint      string
	PublicKey        string
	SealedPrivateKey []byte
}

// Store is the repository port. Implemented by postgres.go, faked in tests. Mutations take
// a database.Tx so they commit with the audit row. Methods that target the ACTIVE
// credential return ok=false (nil error) when no active row matched, so a no-op is reported
// as NotFound rather than audited as a change. The store is the only place ciphertext
// lives; the service seals before calling Upsert/Rotate and opens GetSealed's bytes itself.
type Store interface {
	Upsert(ctx context.Context, tx database.Tx, p UpsertParams) (Credential, error)
	Rotate(ctx context.Context, tx database.Tx, p RotateParams) (cred Credential, ok bool, err error)
	Revoke(ctx context.Context, tx database.Tx, serverID string) (revokedID string, ok bool, err error)
	MarkUsed(ctx context.Context, tx database.Tx, serverID string) (usedID string, ok bool, err error)
	Get(ctx context.Context, serverID string) (cred Credential, ok bool, err error)
	// GetSealed returns the sealed private-key bytes of the ACTIVE credential. ok is false
	// when there is no active credential.
	GetSealed(ctx context.Context, serverID string) (sealed []byte, ok bool, err error)
	// WorkspaceIDForServer resolves a server's owning workspace (servers belong directly to
	// a workspace), so this server-scoped module authorizes and audits against it. ok is
	// false when the server does not exist.
	WorkspaceIDForServer(ctx context.Context, serverID string) (workspaceID string, ok bool, err error)

	// --- Dashboard-managed setup runs ---

	// InsertSetupRun creates a queued run; it takes a tx so it commits with the start audit.
	InsertSetupRun(ctx context.Context, tx database.Tx, serverID, workspaceID string, startedBy *string) (SetupRun, error)
	// CountSetupRuns returns how many runs a server already has (to audit start vs retry).
	CountSetupRuns(ctx context.Context, serverID string) (int64, error)
	// SetSetupRunStatus advances a run's status (plain write; terminal-state audits commit
	// separately). ok is false when no run matched.
	SetSetupRunStatus(ctx context.Context, setupRunID, status, failureReason string) (run SetupRun, ok bool, err error)
	GetSetupRun(ctx context.Context, setupRunID string) (run SetupRun, ok bool, err error)
	// AppendSetupEvent appends one ordered, redacted status/log line (plain write).
	AppendSetupEvent(ctx context.Context, e NewSetupEvent) (SetupEvent, error)
	ListSetupEvents(ctx context.Context, setupRunID string, afterSeq int64) ([]SetupEvent, error)

	// --- Host-key TOFU pin (on the servers row) ---

	// HostKeyFingerprint returns the server's pinned fingerprint ("" if unpinned). ok is
	// false when the server does not exist.
	HostKeyFingerprint(ctx context.Context, serverID string) (fingerprint string, ok bool, err error)
	SetHostKeyFingerprint(ctx context.Context, serverID, fingerprint string) error
}

// NewSetupEvent is an append-only status/log line. Message is plain-English and redacted —
// never a raw credential, private key, or registration token.
type NewSetupEvent struct {
	SetupRunID string
	Step       string
	Kind       string // status | log
	Status     string // started | ok | failed | skipped
	Message    string
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; a port so the
// service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what serversetup needs from the audit module.
// *audit.Service satisfies it structurally — serversetup never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// Sealer is the CONSUMER-DEFINED port for sealing and opening private-key material at rest.
// *crypto.Box satisfies it structurally — the master key stays in that package. Unlike
// secrets, this module also Opens: the SSH runner needs the private key in-process to
// connect, but it is never returned through the API.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

// KeyGenerator produces a fresh SSH management keypair. The default implementation wraps
// sshkeys.Generate; tests inject a fake to make fingerprints and key bytes deterministic.
type KeyGenerator interface {
	Generate() (sshkeys.KeyPair, error)
}

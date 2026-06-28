package backups

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testServiceID = "11111111-1111-1111-1111-111111111111"
	testBackupID  = "22222222-2222-2222-2222-222222222222"
	testServerID  = "33333333-3333-3333-3333-333333333333"
	testCred      = "agent-credential"
)

// fakeStore is an in-memory Store. Knobs configure each resolution; recorded values let tests
// assert what was written.
type fakeStore struct {
	target        ServiceTarget
	targetOK      bool
	runningServer string
	running       bool
	creds         DBCredentials
	claim         Backup
	claimOK       bool
	get           Backup
	getOK         bool
	agentServer   string
	agentOK       bool

	inserted Backup
	updated  StatusUpdate
}

func (f *fakeStore) InsertBackup(_ context.Context, _ database.Tx, b NewBackup) (Backup, error) {
	f.inserted = Backup{ID: testBackupID, ServiceID: b.ServiceID, WorkspaceID: b.WorkspaceID, ServerID: b.ServerID, Status: StatusQueued}
	return f.inserted, nil
}
func (f *fakeStore) GetBackup(context.Context, string) (Backup, bool, error) {
	return f.get, f.getOK, nil
}
func (f *fakeStore) ListByService(context.Context, string) ([]Backup, error) {
	return []Backup{f.get}, nil
}
func (f *fakeStore) ClaimNextForServer(context.Context, database.Tx, string) (Backup, bool, error) {
	return f.claim, f.claimOK, nil
}
func (f *fakeStore) UpdateStatus(_ context.Context, _ database.Tx, u StatusUpdate) (Backup, error) {
	f.updated = u
	return Backup{ID: u.BackupID, Status: u.Status}, nil
}
func (f *fakeStore) ServiceTarget(context.Context, string) (ServiceTarget, bool, error) {
	return f.target, f.targetOK, nil
}
func (f *fakeStore) RunningServerForService(context.Context, string) (string, bool, error) {
	return f.runningServer, f.running, nil
}
func (f *fakeStore) AgentServerByCredential(context.Context, []byte) (string, string, bool, error) {
	return "agent-1", f.agentServer, f.agentOK, nil
}
func (f *fakeStore) DBCredentialsForService(context.Context, string) (DBCredentials, error) {
	return f.creds, nil
}

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

type allowAll struct{}

func (allowAll) Authorize(context.Context, principal.Principal, authz.Action, authz.Resource) error {
	return nil
}

type denyAll struct{}

func (denyAll) Authorize(context.Context, principal.Principal, authz.Action, authz.Resource) error {
	return problem.PermissionDenied("denied")
}

type fakeRecorder struct{ calls int }

func (f *fakeRecorder) Record(context.Context, database.Tx, string, string, string, string, string) error {
	f.calls++
	return nil
}

func newSvc(store Store, az authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, az, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func postgresTarget() ServiceTarget {
	return ServiceTarget{ID: testServiceID, Name: "db", WorkspaceID: "ws", EnvironmentID: "env", ProjectID: "proj", SourceKind: "template", TemplateID: "postgres"}
}

func TestCreateBackup_Success(t *testing.T) {
	store := &fakeStore{target: postgresTarget(), targetOK: true, runningServer: testServerID, running: true}
	rec := &fakeRecorder{}
	b, err := newSvc(store, allowAll{}, rec).CreateBackup(context.Background(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.ServerID != testServerID || b.Status != StatusQueued {
		t.Errorf("backup = %+v, want queued on the running server", b)
	}
	if rec.calls != 1 {
		t.Errorf("audit calls = %d, want 1 (create must be audited)", rec.calls)
	}
}

func TestCreateBackup_RejectsNonDatabaseService(t *testing.T) {
	target := postgresTarget()
	target.SourceKind = "git"
	target.TemplateID = ""
	store := &fakeStore{target: target, targetOK: true, running: true, runningServer: testServerID}
	_, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID)
	if err == nil {
		t.Fatal("expected an error backing up a non-database service")
	}
}

func TestCreateBackup_RequiresRunningDatabase(t *testing.T) {
	store := &fakeStore{target: postgresTarget(), targetOK: true, running: false}
	_, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID)
	if err == nil {
		t.Fatal("expected an error backing up a database that isn't running")
	}
}

func TestCreateBackup_Unauthorized(t *testing.T) {
	store := &fakeStore{target: postgresTarget(), targetOK: true, runningServer: testServerID, running: true}
	if _, err := newSvc(store, denyAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID); err == nil {
		t.Fatal("expected a permission error")
	}
}

func TestCreateBackup_NotFound(t *testing.T) {
	store := &fakeStore{targetOK: false}
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID); err == nil {
		t.Fatal("expected a not-found error for a missing service")
	}
}

func TestPollBackupJob_NoWork(t *testing.T) {
	store := &fakeStore{agentServer: testServerID, agentOK: true, claimOK: false}
	out, err := newSvc(store, allowAll{}, &fakeRecorder{}).PollBackupJob(context.Background(), PollInput{Credential: testCred})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.HasWork {
		t.Errorf("HasWork = true, want false when the queue is empty")
	}
}

func TestPollBackupJob_ClaimsAndResolvesCredentials(t *testing.T) {
	store := &fakeStore{
		agentServer: testServerID, agentOK: true,
		claim: Backup{ID: testBackupID, ServiceID: testServiceID}, claimOK: true,
		creds: DBCredentials{User: "plorigo", Password: "secret", Database: "app"},
	}
	out, err := newSvc(store, allowAll{}, &fakeRecorder{}).PollBackupJob(context.Background(), PollInput{Credential: testCred})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.HasWork || out.BackupID != testBackupID || out.Kind != KindBackup || out.Engine != EnginePostgres {
		t.Errorf("claimed = %+v, want work for the postgres backup", out)
	}
	if out.PgUser != "plorigo" || out.PgPassword != "secret" || out.PgDatabase != "app" {
		t.Errorf("credentials = %+v, want the resolved POSTGRES_* values", out)
	}
}

func TestPollBackupJob_UnknownCredential(t *testing.T) {
	store := &fakeStore{agentOK: false}
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).PollBackupJob(context.Background(), PollInput{Credential: testCred}); err == nil {
		t.Fatal("expected a permission error for an unknown agent credential")
	}
}

func TestReportBackupJob_OwnershipEnforced(t *testing.T) {
	store := &fakeStore{
		agentServer: testServerID, agentOK: true,
		get: Backup{ID: testBackupID, ServerID: "another-server"}, getOK: true,
	}
	err := newSvc(store, allowAll{}, &fakeRecorder{}).ReportBackupJob(context.Background(), ReportInput{
		Credential: testCred, BackupID: testBackupID, Status: StatusSucceeded,
	})
	if err == nil {
		t.Fatal("expected a permission error when the agent doesn't own the backup")
	}
}

func TestReportBackupJob_UpdatesStatus(t *testing.T) {
	store := &fakeStore{
		agentServer: testServerID, agentOK: true,
		get: Backup{ID: testBackupID, ServerID: testServerID}, getOK: true,
	}
	err := newSvc(store, allowAll{}, &fakeRecorder{}).ReportBackupJob(context.Background(), ReportInput{
		Credential: testCred, BackupID: testBackupID, Status: StatusSucceeded, ArtifactURI: "/data/x.sql", SizeBytes: 42, Checksum: "abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.updated.Status != StatusSucceeded || store.updated.SizeBytes != 42 {
		t.Errorf("update = %+v, want succeeded with size 42", store.updated)
	}
}

func TestReportBackupJob_RejectsBadStatus(t *testing.T) {
	store := &fakeStore{agentServer: testServerID, agentOK: true, get: Backup{ID: testBackupID, ServerID: testServerID}, getOK: true}
	err := newSvc(store, allowAll{}, &fakeRecorder{}).ReportBackupJob(context.Background(), ReportInput{
		Credential: testCred, BackupID: testBackupID, Status: "queued", // not an agent-reportable status
	})
	if err == nil {
		t.Fatal("expected an error for a non-agent-reportable status")
	}
}

func TestIsAgentReportableStatus(t *testing.T) {
	for _, s := range []string{StatusDumping, StatusUploading, StatusVerifying, StatusSucceeded, StatusFailed} {
		if !isAgentReportableStatus(s) {
			t.Errorf("isAgentReportableStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{StatusQueued, StatusAssigned, "bogus", ""} {
		if isAgentReportableStatus(s) {
			t.Errorf("isAgentReportableStatus(%q) = true, want false", s)
		}
	}
}

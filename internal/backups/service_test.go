package backups

import (
	"context"
	"io"
	"log/slog"
	"strings"
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
	testRestoreID = "44444444-4444-4444-4444-444444444444"
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

	restoreClaim   RestoreJob
	restoreClaimOK bool
	restoreGet     RestoreJob
	restoreGetOK   bool

	inserted        Backup
	updated         StatusUpdate
	insertedRestore NewRestore
	updatedRestore  RestoreStatusUpdate
}

func (f *fakeStore) InsertBackup(_ context.Context, _ database.Tx, b NewBackup) (Backup, error) {
	f.inserted = Backup{ID: testBackupID, ServiceID: b.ServiceID, WorkspaceID: b.WorkspaceID, ServerID: b.ServerID, Status: StatusQueued, Label: b.Label, TriggerSource: b.TriggerSource}
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
func (f *fakeStore) InsertRestore(_ context.Context, _ database.Tx, r NewRestore) (RestoreJob, error) {
	f.insertedRestore = r
	return RestoreJob{ID: "restore-1", BackupID: r.BackupID, ServiceID: r.ServiceID, ServerID: r.ServerID, Status: RestoreStatusQueued}, nil
}
func (f *fakeStore) GetRestore(context.Context, string) (RestoreJob, bool, error) {
	return f.restoreGet, f.restoreGetOK, nil
}
func (f *fakeStore) ListRestoresByService(context.Context, string) ([]RestoreJob, error) {
	return []RestoreJob{f.restoreGet}, nil
}
func (f *fakeStore) ClaimNextRestoreForServer(context.Context, database.Tx, string) (RestoreJob, bool, error) {
	return f.restoreClaim, f.restoreClaimOK, nil
}
func (f *fakeStore) UpdateRestoreStatus(_ context.Context, _ database.Tx, u RestoreStatusUpdate) (RestoreJob, error) {
	f.updatedRestore = u
	return RestoreJob{ID: u.RestoreID, Status: u.Status}, nil
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
	b, err := newSvc(store, allowAll{}, rec).CreateBackup(context.Background(), testServiceID, "  nightly  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.ServerID != testServerID || b.Status != StatusQueued {
		t.Errorf("backup = %+v, want queued on the running server", b)
	}
	// The label is trimmed and persisted, and a dashboard-initiated backup is triggered manually —
	// the identifying info the row carries.
	if store.inserted.Label != "nightly" || store.inserted.TriggerSource != TriggerManual {
		t.Errorf("inserted = %+v, want label %q trigger %q", store.inserted, "nightly", TriggerManual)
	}
	if rec.calls != 1 {
		t.Errorf("audit calls = %d, want 1 (create must be audited)", rec.calls)
	}
}

func TestCreateBackup_RejectsOverlongLabel(t *testing.T) {
	store := &fakeStore{target: postgresTarget(), targetOK: true, runningServer: testServerID, running: true}
	long := strings.Repeat("x", maxLabelLen+1)
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID, long); err == nil {
		t.Fatal("expected an error for a label longer than the limit")
	}
}

func TestCreateBackup_RejectsNonDatabaseService(t *testing.T) {
	target := postgresTarget()
	target.SourceKind = "git"
	target.TemplateID = ""
	store := &fakeStore{target: target, targetOK: true, running: true, runningServer: testServerID}
	_, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID, "")
	if err == nil {
		t.Fatal("expected an error backing up a non-database service")
	}
}

func TestCreateBackup_RequiresRunningDatabase(t *testing.T) {
	store := &fakeStore{target: postgresTarget(), targetOK: true, running: false}
	_, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID, "")
	if err == nil {
		t.Fatal("expected an error backing up a database that isn't running")
	}
}

func TestCreateBackup_Unauthorized(t *testing.T) {
	store := &fakeStore{target: postgresTarget(), targetOK: true, runningServer: testServerID, running: true}
	if _, err := newSvc(store, denyAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID, ""); err == nil {
		t.Fatal("expected a permission error")
	}
}

func TestCreateBackup_NotFound(t *testing.T) {
	store := &fakeStore{targetOK: false}
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).CreateBackup(context.Background(), testServiceID, ""); err == nil {
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

func succeededBackup() Backup {
	return Backup{ID: testBackupID, ServiceID: testServiceID, WorkspaceID: "ws", EnvironmentID: "env", ProjectID: "proj", ServerID: testServerID, Status: StatusSucceeded, ArtifactURI: "/data/x.sql"}
}

func TestRestoreBackup_Success(t *testing.T) {
	store := &fakeStore{get: succeededBackup(), getOK: true, runningServer: testServerID, running: true}
	rec := &fakeRecorder{}
	r, err := newSvc(store, allowAll{}, rec).RestoreBackup(context.Background(), testBackupID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Status != RestoreStatusQueued || store.insertedRestore.ArtifactURI != "/data/x.sql" {
		t.Errorf("restore = %+v, inserted = %+v, want queued carrying the source artifact", r, store.insertedRestore)
	}
	if rec.calls != 1 {
		t.Errorf("audit calls = %d, want 1", rec.calls)
	}
}

func TestRestoreBackup_RejectsUnsucceededBackup(t *testing.T) {
	b := succeededBackup()
	b.Status = StatusDumping
	store := &fakeStore{get: b, getOK: true, runningServer: testServerID, running: true}
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).RestoreBackup(context.Background(), testBackupID); err == nil {
		t.Fatal("expected an error restoring a backup that hasn't succeeded")
	}
}

func TestRestoreBackup_RejectsDifferentServer(t *testing.T) {
	store := &fakeStore{get: succeededBackup(), getOK: true, runningServer: "another-server", running: true}
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).RestoreBackup(context.Background(), testBackupID); err == nil {
		t.Fatal("expected an error when the database runs on a different server than the backup")
	}
}

func TestRestoreBackup_RequiresRunningDatabase(t *testing.T) {
	store := &fakeStore{get: succeededBackup(), getOK: true, running: false}
	if _, err := newSvc(store, allowAll{}, &fakeRecorder{}).RestoreBackup(context.Background(), testBackupID); err == nil {
		t.Fatal("expected an error restoring into a database that isn't running")
	}
}

func TestPollRestoreJob_ClaimsAndResolves(t *testing.T) {
	store := &fakeStore{
		agentServer: testServerID, agentOK: true,
		restoreClaim: RestoreJob{ID: "restore-1", ServiceID: testServiceID, ArtifactURI: "/data/x.sql"}, restoreClaimOK: true,
		creds: DBCredentials{User: "plorigo", Password: "secret", Database: "app"},
	}
	out, err := newSvc(store, allowAll{}, &fakeRecorder{}).PollRestoreJob(context.Background(), PollInput{Credential: testCred})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.HasWork || out.RestoreID != "restore-1" || out.ArtifactURI != "/data/x.sql" || out.PgDatabase != "app" {
		t.Errorf("claimed restore = %+v, want work with creds + artifact", out)
	}
}

func TestReportRestoreJob_OwnershipEnforced(t *testing.T) {
	store := &fakeStore{agentServer: testServerID, agentOK: true, restoreGet: RestoreJob{ID: testRestoreID, ServerID: "another"}, restoreGetOK: true}
	err := newSvc(store, allowAll{}, &fakeRecorder{}).ReportRestoreJob(context.Background(), ReportRestoreInput{
		Credential: testCred, RestoreID: testRestoreID, Status: RestoreStatusSucceeded,
	})
	if err == nil {
		t.Fatal("expected a permission error when the agent doesn't own the restore")
	}
}

func TestReportRestoreJob_UpdatesStatus(t *testing.T) {
	store := &fakeStore{agentServer: testServerID, agentOK: true, restoreGet: RestoreJob{ID: testRestoreID, ServerID: testServerID}, restoreGetOK: true}
	err := newSvc(store, allowAll{}, &fakeRecorder{}).ReportRestoreJob(context.Background(), ReportRestoreInput{
		Credential: testCred, RestoreID: testRestoreID, Status: RestoreStatusSucceeded, Message: "done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.updatedRestore.Status != RestoreStatusSucceeded {
		t.Errorf("update = %+v, want succeeded", store.updatedRestore)
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

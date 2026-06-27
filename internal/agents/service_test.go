package agents

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testServerID  = "11111111-1111-1111-1111-111111111111"
	testAgentID   = "22222222-2222-2222-2222-222222222222"
	testWorkspace = "ws-1"
)

type fakeStore struct {
	ws    string
	wsOK  bool
	wsErr error

	insertTokErr error
	insertedTok  RegistrationTokenRow

	consumed   ConsumedToken
	consumeOK  bool
	consumeErr error

	upserted    AgentUpsert
	upsertAgent Agent
	upsertErr   error

	hbAgent Agent
	hbOK    bool
	hbErr   error
	hbFacts HeartbeatFacts

	list []Agent
}

func (f *fakeStore) WorkspaceIDForServer(_ context.Context, _ string) (string, bool, error) {
	return f.ws, f.wsOK, f.wsErr
}
func (f *fakeStore) InsertRegistrationToken(_ context.Context, _ database.Tx, t RegistrationTokenRow) error {
	f.insertedTok = t
	return f.insertTokErr
}
func (f *fakeStore) ConsumeRegistrationToken(_ context.Context, _ database.Tx, _ []byte) (ConsumedToken, bool, error) {
	return f.consumed, f.consumeOK, f.consumeErr
}
func (f *fakeStore) UpsertAgent(_ context.Context, _ database.Tx, a AgentUpsert) (Agent, error) {
	f.upserted = a
	if f.upsertErr != nil {
		return Agent{}, f.upsertErr
	}
	ag := f.upsertAgent
	ag.ServerID = a.ServerID
	ag.WorkspaceID = a.WorkspaceID
	if ag.ID == "" {
		ag.ID = testAgentID
	}
	return ag, nil
}
func (f *fakeStore) Heartbeat(_ context.Context, _ []byte, facts HeartbeatFacts) (Agent, bool, error) {
	f.hbFacts = facts
	return f.hbAgent, f.hbOK, f.hbErr
}
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Agent, error) {
	return f.list, nil
}

type fakeRecorder struct {
	called    bool
	action    string
	recordErr error
}

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.called = true
	f.action = action
	return f.recordErr
}

type fakeAuthz struct{ err error }

func (f fakeAuthz) Authorize(_ context.Context, _ principal.Principal, _ authz.Action, _ authz.Resource) error {
	return f.err
}

// fakeTx runs fn with a nil tx; the fakes ignore the tx value.
type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, authorizer, rec, slog.Default())
}

func validPubKey() []byte { return make([]byte, ed25519.PublicKeySize) }

func TestCreateRegistrationToken_MintsAndAudits(t *testing.T) {
	store := &fakeStore{ws: testWorkspace, wsOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	tok, err := svc.CreateRegistrationToken(authedCtx(), testServerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(tok.Raw, "plrt_") {
		t.Errorf("token = %q, want plrt_ prefix", tok.Raw)
	}
	if len(store.insertedTok.TokenHash) == 0 {
		t.Error("expected a token hash to be stored")
	}
	if store.insertedTok.WorkspaceID != testWorkspace || store.insertedTok.CreatedBy != "user-1" {
		t.Errorf("stored token = %+v, want workspace/creator set", store.insertedTok)
	}
	if !rec.called || rec.action != "agent.registration_token.create" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreateRegistrationToken_DeniedWhenUnauthorized(t *testing.T) {
	store := &fakeStore{ws: testWorkspace, wsOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)

	_, err := svc.CreateRegistrationToken(authedCtx(), testServerID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied mint must not write an audit event")
	}
	if len(store.insertedTok.TokenHash) != 0 {
		t.Error("a denied mint must not insert a token")
	}
}

func TestCreateRegistrationToken_ServerNotFound(t *testing.T) {
	store := &fakeStore{wsOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})

	_, err := svc.CreateRegistrationToken(authedCtx(), testServerID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestCreateRegistrationToken_InvalidServerID(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateRegistrationToken(authedCtx(), "not-a-uuid")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestRegister_HappyPath(t *testing.T) {
	store := &fakeStore{consumeOK: true, consumed: ConsumedToken{ServerID: testServerID, WorkspaceID: testWorkspace}}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	reg, err := svc.Register(context.Background(), RegisterInput{RegistrationToken: "plrt_abc", PublicKey: validPubKey(), AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(reg.Credential, "plag_") {
		t.Errorf("credential = %q, want plag_ prefix", reg.Credential)
	}
	if reg.AgentID != testAgentID {
		t.Errorf("agent id = %q, want %q", reg.AgentID, testAgentID)
	}
	if len(store.upserted.CredentialHash) == 0 || len(store.upserted.PublicKey) != ed25519.PublicKeySize {
		t.Errorf("upsert missing key material: %+v", store.upserted)
	}
	if store.upserted.ServerID != testServerID {
		t.Errorf("upsert server = %q, want %q", store.upserted.ServerID, testServerID)
	}
	if !rec.called || rec.action != "agent.register" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestRegister_InvalidPublicKey(t *testing.T) {
	store := &fakeStore{consumeOK: true}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Register(context.Background(), RegisterInput{RegistrationToken: "plrt_abc", PublicKey: []byte("short")})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
	if store.upserted.ServerID != "" {
		t.Error("an invalid key must not upsert an agent")
	}
}

func TestRegister_RequiresToken(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Register(context.Background(), RegisterInput{PublicKey: validPubKey()})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestRegister_InvalidToken(t *testing.T) {
	store := &fakeStore{consumeOK: false}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)
	_, err := svc.Register(context.Background(), RegisterInput{RegistrationToken: "plrt_bad", PublicKey: validPubKey()})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if store.upserted.ServerID != "" {
		t.Error("an invalid token must not upsert an agent")
	}
	if rec.called {
		t.Error("an invalid token must not write an audit event")
	}
}

func TestHeartbeat_HappyPath(t *testing.T) {
	store := &fakeStore{hbOK: true, hbAgent: Agent{ID: testAgentID, ServerID: testServerID}}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	dockerUp := true
	res, err := svc.Heartbeat(context.Background(), HeartbeatInput{
		Credential:      "plag_x",
		AgentVersion:    "v1",
		DockerAvailable: &dockerUp,
		DockerVersion:   "27.1.1",
		OS:              "linux",
		Arch:            "amd64",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextInterval != heartbeatInterval {
		t.Errorf("next interval = %v, want %v", res.NextInterval, heartbeatInterval)
	}
	// The reported facts must reach the store, so liveness and compatibility are recorded together.
	if store.hbFacts.AgentVersion != "v1" || store.hbFacts.DockerVersion != "27.1.1" || store.hbFacts.OS != "linux" || store.hbFacts.Arch != "amd64" {
		t.Errorf("forwarded facts = %+v, want version/docker/os/arch set", store.hbFacts)
	}
	if store.hbFacts.DockerAvailable == nil || !*store.hbFacts.DockerAvailable {
		t.Error("expected DockerAvailable=true to be forwarded to the store")
	}
}

func TestHeartbeat_UnknownCredential(t *testing.T) {
	store := &fakeStore{hbOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Heartbeat(context.Background(), HeartbeatInput{Credential: "plag_bad"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestHeartbeat_RequiresCredential(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Heartbeat(context.Background(), HeartbeatInput{})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestAgentStatus(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-10 * time.Second)
	stale := now.Add(-10 * time.Minute)

	cases := []struct {
		name string
		last *time.Time
		want string
	}{
		{"never connected", nil, StatusAwaiting},
		{"recent heartbeat", &recent, StatusOnline},
		{"stale heartbeat", &stale, StatusOffline},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Agent{LastSeenAt: c.last}.Status(now)
			if got != c.want {
				t.Errorf("Status = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAgentReadiness(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-10 * time.Second)
	stale := now.Add(-10 * time.Minute)
	up, down := true, false
	const gib = int64(1) << 30
	const mib = int64(1) << 20

	// base is a fully-ready server: online, modern Docker, Caddy serving, healthy resources,
	// Linux, and extended facts present (CPUCount > 0). Each case mutates one thing.
	base := func() Agent {
		return Agent{
			LastSeenAt:        &recent,
			DockerAvailable:   &up,
			DockerVersion:     "24.0.7",
			OS:                "linux",
			Arch:              "amd64",
			CaddyAvailable:    &up,
			CaddyRunning:      true,
			CaddyVersion:      "2.7.6",
			DiskTotalBytes:    50 * gib,
			DiskFreeBytes:     40 * gib,
			MemTotalBytes:     4 * gib,
			MemAvailableBytes: 2 * gib,
			CPUCount:          2,
		}
	}

	cases := []struct {
		name          string
		mutate        func(a *Agent)
		allowNonLinux bool // dev relaxes the Linux-only host requirement
		wantState     string
		wantReason    bool // a non-empty, actionable reason is expected
	}{
		{"ready", nil, false, ReadinessReady, false},
		{"offline agent", func(a *Agent) { a.LastSeenAt = &stale }, false, ReadinessUnknown, true},
		{"never connected", func(a *Agent) { a.LastSeenAt = nil }, false, ReadinessUnknown, true},
		{"unsupported OS", func(a *Agent) { a.OS = "windows" }, false, ReadinessBlocked, true},
		// In dev, a non-Linux host (a contributor's macOS workstation) is no longer a hard
		// blocker, so an otherwise-ready agent reports ready.
		{"non-linux host allowed in dev", func(a *Agent) { a.OS = "darwin" }, true, ReadinessReady, false},
		{"docker missing", func(a *Agent) { a.DockerAvailable = &down }, false, ReadinessBlocked, true},
		{"docker too old", func(a *Agent) { a.DockerVersion = "19.03.5" }, false, ReadinessDegraded, true},
		{"docker not yet reported", func(a *Agent) { a.DockerAvailable = nil; a.DockerVersion = "" }, false, ReadinessDegraded, true},
		{"caddy missing", func(a *Agent) { a.CaddyAvailable = &down }, false, ReadinessBlocked, true},
		{"occupied ports (caddy installed, not running)", func(a *Agent) { a.CaddyRunning = false }, false, ReadinessBlocked, true},
		{"critically low disk", func(a *Agent) { a.DiskFreeBytes = 512 * mib }, false, ReadinessBlocked, true},
		{"low disk", func(a *Agent) { a.DiskFreeBytes = 3 * gib }, false, ReadinessDegraded, true},
		{"low memory", func(a *Agent) { a.MemAvailableBytes = 128 * mib }, false, ReadinessDegraded, true},
		// An agent that predates the extended facts (CPUCount == 0) is judged on Docker
		// alone — its zeroed Caddy/disk/memory fields must never falsely block it.
		{"older agent without extended facts", func(a *Agent) {
			a.CPUCount = 0
			a.CaddyAvailable, a.CaddyRunning = nil, false
			a.DiskTotalBytes, a.DiskFreeBytes = 0, 0
			a.MemTotalBytes, a.MemAvailableBytes = 0, 0
		}, false, ReadinessReady, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := base()
			if c.mutate != nil {
				c.mutate(&a)
			}
			state, reason := a.Readiness(now, c.allowNonLinux)
			if state != c.wantState {
				t.Errorf("state = %q, want %q (reason %q)", state, c.wantState, reason)
			}
			if (reason != "") != c.wantReason {
				t.Errorf("reason = %q, want non-empty=%v", reason, c.wantReason)
			}
		})
	}
}

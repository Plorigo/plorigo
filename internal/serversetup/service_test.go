package serversetup

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
	"github.com/plorigo/plorigo/internal/platform/sshkeys"
)

const (
	testServerID  = "33333333-3333-3333-3333-333333333333"
	testCredID    = "11111111-1111-1111-1111-111111111111"
	testWorkspace = "ws-1"
)

type fakeStore struct {
	wsID  string
	wsOK  bool
	wsErr error

	upsertErr error
	upserted  UpsertParams

	rotateOK  bool
	rotateErr error
	rotated   RotateParams

	revokeOK  bool
	revokeErr error

	usedOK  bool
	usedErr error

	getCred Credential
	getOK   bool
	getErr  error

	sealed    []byte
	sealedOK  bool
	sealedErr error
}

// newFakeStore resolves testServerID to testWorkspace and succeeds on every active-row
// operation by default, so a test only overrides the part it exercises.
func newFakeStore() *fakeStore {
	return &fakeStore{
		wsID: testWorkspace, wsOK: true,
		rotateOK: true, revokeOK: true, usedOK: true, getOK: true, sealedOK: true,
		getCred: Credential{ID: testCredID, ServerID: testServerID, Fingerprint: "SHA256:get", PublicKey: "ssh-ed25519 GET", RotationState: "active"},
		sealed:  []byte("sealed:PRIVATE"),
	}
}

func (f *fakeStore) Upsert(_ context.Context, _ database.Tx, p UpsertParams) (Credential, error) {
	if f.upsertErr != nil {
		return Credential{}, f.upsertErr
	}
	f.upserted = p
	return Credential{ID: testCredID, ServerID: p.ServerID, Fingerprint: p.Fingerprint, PublicKey: p.PublicKey, CreatedBy: p.CreatedBy, RotationState: "active"}, nil
}

func (f *fakeStore) Rotate(_ context.Context, _ database.Tx, p RotateParams) (Credential, bool, error) {
	if f.rotateErr != nil {
		return Credential{}, false, f.rotateErr
	}
	f.rotated = p
	if !f.rotateOK {
		return Credential{}, false, nil
	}
	return Credential{ID: testCredID, ServerID: p.ServerID, Fingerprint: p.Fingerprint, PublicKey: p.PublicKey, RotationState: "active"}, true, nil
}

func (f *fakeStore) Revoke(_ context.Context, _ database.Tx, _ string) (string, bool, error) {
	if f.revokeErr != nil {
		return "", false, f.revokeErr
	}
	if !f.revokeOK {
		return "", false, nil
	}
	return testCredID, true, nil
}

func (f *fakeStore) MarkUsed(_ context.Context, _ database.Tx, _ string) (string, bool, error) {
	if f.usedErr != nil {
		return "", false, f.usedErr
	}
	if !f.usedOK {
		return "", false, nil
	}
	return testCredID, true, nil
}

func (f *fakeStore) Get(_ context.Context, _ string) (Credential, bool, error) {
	return f.getCred, f.getOK, f.getErr
}

func (f *fakeStore) GetSealed(_ context.Context, _ string) ([]byte, bool, error) {
	return f.sealed, f.sealedOK, f.sealedErr
}

func (f *fakeStore) WorkspaceIDForServer(_ context.Context, _ string) (string, bool, error) {
	return f.wsID, f.wsOK, f.wsErr
}

type fakeRecorder struct {
	called      bool
	action      string
	targetType  string
	actor       string
	workspaceID string
	err         error
}

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, targetType, _, workspaceID, actor string) error {
	f.called = true
	f.action = action
	f.targetType = targetType
	f.workspaceID = workspaceID
	f.actor = actor
	return f.err
}

type fakeAuthz struct {
	err       error
	gotAction authz.Action
	gotWS     string
	calls     int
}

func (f *fakeAuthz) Authorize(_ context.Context, _ principal.Principal, action authz.Action, res authz.Resource) error {
	f.calls++
	f.gotAction = action
	f.gotWS = res.WorkspaceID
	return f.err
}

// fakeSealer transforms recognizably so tests can prove the store receives ciphertext (the
// sealer's output), never the raw key, and that Open reverses Seal.
type fakeSealer struct {
	sealed  []byte
	opened  []byte
	sealErr error
	openErr error
}

func (f *fakeSealer) Seal(plaintext []byte) ([]byte, error) {
	if f.sealErr != nil {
		return nil, f.sealErr
	}
	f.sealed = plaintext
	return append([]byte("sealed:"), plaintext...), nil
}

func (f *fakeSealer) Open(sealed []byte) ([]byte, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	f.opened = sealed
	return bytes.TrimPrefix(sealed, []byte("sealed:")), nil
}

type fakeKeyGen struct {
	err   error
	calls int
	keys  []sshkeys.KeyPair
}

func (f *fakeKeyGen) Generate() (sshkeys.KeyPair, error) {
	if f.err != nil {
		return sshkeys.KeyPair{}, f.err
	}
	f.calls++
	if len(f.keys) > 0 {
		return f.keys[(f.calls-1)%len(f.keys)], nil
	}
	return sshkeys.KeyPair{PrivatePEM: []byte("PRIVATE"), AuthorizedKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:default"}, nil
}

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, keys KeyGenerator, sealer Sealer, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, keys, sealer, authorizer, rec, slog.Default())
}

func isKind(err error, k problem.Kind) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == k
}

func TestProvision_SealsKeyAndAudits(t *testing.T) {
	store := newFakeStore()
	keys := &fakeKeyGen{}
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	svc := newSvc(store, keys, sealer, &fakeAuthz{}, rec)

	cred, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: testServerID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The generated private key went through the Sealer, and the STORE received the
	// SEALER'S OUTPUT — the service never hands the store the raw key.
	if string(sealer.sealed) != "PRIVATE" {
		t.Errorf("sealer received %q, want the raw private key PRIVATE", sealer.sealed)
	}
	if string(store.upserted.SealedPrivateKey) != "sealed:PRIVATE" {
		t.Errorf("stored key = %q, want the Sealer's ciphertext", store.upserted.SealedPrivateKey)
	}
	if store.upserted.Fingerprint != "SHA256:default" || store.upserted.PublicKey != "ssh-ed25519 AAAA" {
		t.Errorf("store got fingerprint=%q public=%q, want the generated keypair", store.upserted.Fingerprint, store.upserted.PublicKey)
	}
	if cred.Fingerprint != "SHA256:default" || cred.WorkspaceID != testWorkspace {
		t.Errorf("credential = %+v, want generated fingerprint and resolved workspace", cred)
	}
	if cred.CreatedBy == nil || *cred.CreatedBy != "user-1" {
		t.Errorf("created_by = %v, want user-1", cred.CreatedBy)
	}
	if !rec.called || rec.action != "server_setup.key.provision" || rec.targetType != "ssh_management_key" {
		t.Errorf("audit not recorded correctly: called=%v action=%q target=%q", rec.called, rec.action, rec.targetType)
	}
	if rec.actor != "user-1" || rec.workspaceID != testWorkspace {
		t.Errorf("audit actor/workspace = %q/%q, want user-1/%s", rec.actor, rec.workspaceID, testWorkspace)
	}
}

func TestProvision_DeniedDoesNotGenerateSealOrStore(t *testing.T) {
	store := newFakeStore()
	keys := &fakeKeyGen{}
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	az := &fakeAuthz{err: problem.PermissionDenied("nope")}
	svc := newSvc(store, keys, sealer, az, rec)

	_, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: testServerID})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if az.gotAction != authz.ActionServerSetupRun {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupRun)
	}
	if keys.calls != 0 {
		t.Error("a denied provision must not generate a key")
	}
	if sealer.sealed != nil {
		t.Error("a denied provision must not seal")
	}
	if store.upserted.ServerID != "" {
		t.Error("a denied provision must not upsert")
	}
	if rec.called {
		t.Error("a denied provision must not audit")
	}
}

func TestProvision_ServerNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: testServerID})
	if !isKind(err, problem.KindNotFound) {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestProvision_InvalidServerID(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: "not-a-uuid"})
	if !isKind(err, problem.KindInvalidInput) {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestProvision_SealFailureDoesNotStore(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{sealErr: errors.New("boom")}, &fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: testServerID})
	if !isKind(err, problem.KindInternal) {
		t.Errorf("got %v, want Internal", err)
	}
	if store.upserted.ServerID != "" {
		t.Error("a failed seal must not reach the store")
	}
}

func TestProvision_KeygenFailure(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeKeyGen{err: errors.New("boom")}, &fakeSealer{}, &fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: testServerID})
	if !isKind(err, problem.KindInternal) {
		t.Errorf("got %v, want Internal", err)
	}
}

func TestRotate_GeneratesNewKeyAndAudits(t *testing.T) {
	store := newFakeStore()
	keys := &fakeKeyGen{keys: []sshkeys.KeyPair{{PrivatePEM: []byte("NEW"), AuthorizedKey: "ssh-ed25519 NEW", Fingerprint: "SHA256:new"}}}
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	az := &fakeAuthz{}
	svc := newSvc(store, keys, sealer, az, rec)

	cred, err := svc.Rotate(authedCtx(), RotateInput{ServerID: testServerID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az.gotAction != authz.ActionServerSetupKeyRotate {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupKeyRotate)
	}
	if store.rotated.Fingerprint != "SHA256:new" || string(store.rotated.SealedPrivateKey) != "sealed:NEW" {
		t.Errorf("rotate stored fingerprint=%q key=%q, want the freshly generated, sealed key", store.rotated.Fingerprint, store.rotated.SealedPrivateKey)
	}
	if cred.Fingerprint != "SHA256:new" || cred.WorkspaceID != testWorkspace {
		t.Errorf("credential = %+v, want new fingerprint + workspace", cred)
	}
	if !rec.called || rec.action != "server_setup.key.rotate" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestRotate_NotFoundWhenNoActiveCredential(t *testing.T) {
	store := newFakeStore()
	store.rotateOK = false // no active row matched (missing or revoked)
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, rec)
	_, err := svc.Rotate(authedCtx(), RotateInput{ServerID: testServerID})
	if !isKind(err, problem.KindNotFound) {
		t.Errorf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("a rotate that matched no active credential must not audit")
	}
}

func TestRotate_DeniedDoesNotGenerateOrStore(t *testing.T) {
	store := newFakeStore()
	keys := &fakeKeyGen{}
	rec := &fakeRecorder{}
	svc := newSvc(store, keys, &fakeSealer{}, &fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.Rotate(authedCtx(), RotateInput{ServerID: testServerID})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if keys.calls != 0 || store.rotated.ServerID != "" || rec.called {
		t.Error("a denied rotate must not generate, store, or audit")
	}
}

func TestRevoke_AuditsOnSuccess(t *testing.T) {
	rec := &fakeRecorder{}
	az := &fakeAuthz{}
	svc := newSvc(newFakeStore(), &fakeKeyGen{}, &fakeSealer{}, az, rec)
	if err := svc.Revoke(authedCtx(), RevokeInput{ServerID: testServerID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az.gotAction != authz.ActionServerSetupKeyRevoke {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupKeyRevoke)
	}
	if !rec.called || rec.action != "server_setup.key.revoke" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestRevoke_NotFoundWhenNoActiveCredential(t *testing.T) {
	store := newFakeStore()
	store.revokeOK = false
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, rec)
	err := svc.Revoke(authedCtx(), RevokeInput{ServerID: testServerID})
	if !isKind(err, problem.KindNotFound) {
		t.Errorf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("revoking nothing must not audit")
	}
}

func TestGet_AuthorizesAndReturnsMetadata(t *testing.T) {
	rec := &fakeRecorder{}
	az := &fakeAuthz{}
	svc := newSvc(newFakeStore(), &fakeKeyGen{}, &fakeSealer{}, az, rec)
	cred, err := svc.Get(authedCtx(), testServerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az.gotAction != authz.ActionServerSetupKeyRead {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupKeyRead)
	}
	if cred.Fingerprint != "SHA256:get" || cred.WorkspaceID != testWorkspace {
		t.Errorf("credential = %+v, want metadata + resolved workspace", cred)
	}
	if rec.called {
		t.Error("a read must not audit")
	}
}

func TestGet_NotFound(t *testing.T) {
	store := newFakeStore()
	store.getOK = false
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Get(authedCtx(), testServerID)
	if !isKind(err, problem.KindNotFound) {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestMarkUsed_AuditsUse(t *testing.T) {
	rec := &fakeRecorder{}
	az := &fakeAuthz{}
	svc := newSvc(newFakeStore(), &fakeKeyGen{}, &fakeSealer{}, az, rec)
	if err := svc.MarkUsed(authedCtx(), UseInput{ServerID: testServerID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az.gotAction != authz.ActionServerSetupRun {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupRun)
	}
	if !rec.called || rec.action != "server_setup.key.use" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestMarkUsed_NotFoundWhenNoActiveCredential(t *testing.T) {
	store := newFakeStore()
	store.usedOK = false
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, rec)
	err := svc.MarkUsed(authedCtx(), UseInput{ServerID: testServerID})
	if !isKind(err, problem.KindNotFound) {
		t.Errorf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("marking-used nothing must not audit")
	}
}

func TestRecordFailedAuth_AuditsAgainstServer(t *testing.T) {
	rec := &fakeRecorder{}
	az := &fakeAuthz{}
	svc := newSvc(newFakeStore(), &fakeKeyGen{}, &fakeSealer{}, az, rec)
	if err := svc.RecordFailedAuth(authedCtx(), FailedAuthInput{ServerID: testServerID, Reason: "host key mismatch"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az.gotAction != authz.ActionServerSetupRun {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupRun)
	}
	if !rec.called || rec.action != "server_setup.failed_auth" || rec.targetType != "server" {
		t.Errorf("audit not recorded correctly: called=%v action=%q target=%q", rec.called, rec.action, rec.targetType)
	}
}

func TestOpenPrivateKey_RoundTripsAndIsAuthorized(t *testing.T) {
	store := newFakeStore() // sealed = "sealed:PRIVATE"
	sealer := &fakeSealer{}
	az := &fakeAuthz{}
	svc := newSvc(store, &fakeKeyGen{}, sealer, az, &fakeRecorder{})
	key, err := svc.OpenPrivateKey(authedCtx(), testServerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(key) != "PRIVATE" {
		t.Errorf("opened key = %q, want the original PRIVATE", key)
	}
	if az.gotAction != authz.ActionServerSetupRun {
		t.Errorf("authorized action = %q, want %q", az.gotAction, authz.ActionServerSetupRun)
	}
}

func TestOpenPrivateKey_NotFoundWhenNoActiveCredential(t *testing.T) {
	store := newFakeStore()
	store.sealedOK = false
	svc := newSvc(store, &fakeKeyGen{}, &fakeSealer{}, &fakeAuthz{}, &fakeRecorder{})
	_, err := svc.OpenPrivateKey(authedCtx(), testServerID)
	if !isKind(err, problem.KindNotFound) {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestOpenPrivateKey_DeniedDoesNotOpen(t *testing.T) {
	sealer := &fakeSealer{}
	svc := newSvc(newFakeStore(), &fakeKeyGen{}, sealer, &fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.OpenPrivateKey(authedCtx(), testServerID)
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if sealer.opened != nil {
		t.Error("a denied open must not decrypt the key")
	}
}

// TestAuthorizesAgainstResolvedWorkspace proves the cross-workspace guard: the service
// authorizes against the workspace it RESOLVED from the server, so a caller who lacks a
// role there is denied (and nothing is mutated). The policy module turns "not a member of
// this workspace" into PermissionDenied; here the fake authorizer stands in for that.
func TestAuthorizesAgainstResolvedWorkspace(t *testing.T) {
	store := newFakeStore()
	store.wsID = "ws-other"
	keys := &fakeKeyGen{}
	rec := &fakeRecorder{}
	az := &fakeAuthz{err: problem.PermissionDenied("you are not a member of this workspace")}
	svc := newSvc(store, keys, &fakeSealer{}, az, rec)

	_, err := svc.Provision(authedCtx(), ProvisionInput{ServerID: testServerID})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if az.gotWS != "ws-other" {
		t.Errorf("authorized against workspace %q, want the server's resolved workspace ws-other", az.gotWS)
	}
	if keys.calls != 0 || store.upserted.ServerID != "" || rec.called {
		t.Error("a cross-workspace denial must not generate, store, or audit")
	}
}

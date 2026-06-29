package deployments

import (
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/plorigo/plorigo/internal/platform/problem"
)

func TestCreatePreview_PasswordStoresBcryptHash(t *testing.T) {
	store := previewStore()
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	if _, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{
		ServiceID: testServiceID, ServerID: testServerID, Branch: "feat", Password: "hunter2", PasswordUser: "alice",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := store.insertedPreview
	if p.AuthUser != "alice" {
		t.Errorf("auth user = %q, want alice", p.AuthUser)
	}
	if p.AuthHash == "" || bcrypt.CompareHashAndPassword([]byte(p.AuthHash), []byte("hunter2")) != nil {
		t.Errorf("stored hash %q does not verify the password (or is plaintext)", p.AuthHash)
	}
	if p.AuthHash == "hunter2" {
		t.Error("the plaintext password must never be stored")
	}
}

func TestCreatePreview_NoPasswordUnprotected(t *testing.T) {
	store := previewStore()
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	if _, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, Branch: "feat"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedPreview.AuthHash != "" || store.insertedPreview.AuthUser != "" {
		t.Error("no password must leave the preview unprotected")
	}
}

func TestCreatePreview_PasswordDefaultsUsername(t *testing.T) {
	store := previewStore()
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	if _, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, Branch: "feat", Password: "p"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedPreview.AuthUser != "preview" {
		t.Errorf("default username = %q, want preview", store.insertedPreview.AuthUser)
	}
}

func TestCreatePreview_RejectsUnsafeUsername(t *testing.T) {
	store := previewStore()
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	// A username with a newline could otherwise inject Caddyfile directives when rendered.
	_, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{
		ServiceID: testServiceID, ServerID: testServerID, Branch: "feat", Password: "p", PasswordUser: "evil\n\treverse_proxy",
	})
	wantKind(t, err, problem.KindInvalidInput)
	if store.insertedPreview.AuthHash != "" {
		t.Error("a rejected username must not enqueue a preview")
	}
}

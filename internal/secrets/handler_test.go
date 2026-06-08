package secrets

import (
	"bytes"
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

type fakeService struct {
	list []Secret
	err  error
}

func (f *fakeService) Set(_ context.Context, in SetInput) (Secret, error) {
	if f.err != nil {
		return Secret{}, f.err
	}
	// Note: a Secret carries no value — write-only by construction.
	return Secret{ID: "id1", EnvironmentID: in.EnvironmentID, Key: in.Key, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (f *fakeService) List(_ context.Context, _ string) ([]Secret, error) {
	return f.list, f.err
}
func (f *fakeService) Delete(_ context.Context, _ DeleteInput) error {
	return f.err
}

func TestHandler_SetSecret(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.SetSecret(context.Background(),
		connect.NewRequest(&controlplanev1.SetSecretRequest{EnvironmentId: "e1", Key: "STRIPE_SECRET_KEY", Value: "sk_live_123"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Msg.GetSecret().GetKey(); got != "STRIPE_SECRET_KEY" {
		t.Errorf("key = %q, want STRIPE_SECRET_KEY", got)
	}
}

// TestHandler_SetSecret_ResponseOmitsValue guards the write-only contract on the wire:
// the SetSecret response must never carry the plaintext, even serialized. The Secret
// proto has no value field, so this also fails to compile if one is ever added back.
func TestHandler_SetSecret_ResponseOmitsValue(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.SetSecret(context.Background(),
		connect.NewRequest(&controlplanev1.SetSecretRequest{EnvironmentId: "e1", Key: "TOKEN", Value: "super-secret-value"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := proto.Marshal(resp.Msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte("super-secret-value")) {
		t.Error("SetSecret response leaked the secret value; secrets are write-only")
	}
}

func TestHandler_ListSecrets(t *testing.T) {
	h := &handler{svc: &fakeService{list: []Secret{{ID: "id1", Key: "A"}, {ID: "id2", Key: "B"}}}}
	resp, err := h.ListSecrets(context.Background(),
		connect.NewRequest(&controlplanev1.ListSecretsRequest{EnvironmentId: "e1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(resp.Msg.GetSecrets()); n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestHandler_DeleteSecret(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.DeleteSecret(context.Background(),
		connect.NewRequest(&controlplanev1.DeleteSecretRequest{EnvironmentId: "e1", Key: "TOKEN"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a non-nil response")
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.ListSecrets(context.Background(),
		connect.NewRequest(&controlplanev1.ListSecretsRequest{EnvironmentId: "e1"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

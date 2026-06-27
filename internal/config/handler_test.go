package config

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
	list []Entry
	err  error
}

func (f *fakeService) Set(_ context.Context, in SetInput) (Entry, error) {
	if f.err != nil {
		return Entry{}, f.err
	}
	// Echo the input back as the saved entry; a secret's value is dropped (write-only).
	value := in.Value
	if in.Type == TypeSecret {
		value = ""
	}
	return Entry{
		ID: "id1", Type: in.Type, Scope: in.Scope, ServiceID: in.ServiceID, EnvironmentID: in.EnvironmentID,
		Key: in.Key, Value: value, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}
func (f *fakeService) ListForService(_ context.Context, _ string) ([]Entry, error) {
	return f.list, f.err
}
func (f *fakeService) Delete(_ context.Context, _ DeleteInput) error { return f.err }

func TestHandler_SetConfig_VariableRoundTrips(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.SetConfig(context.Background(), connect.NewRequest(&controlplanev1.SetConfigRequest{
		Type: controlplanev1.ConfigType_CONFIG_TYPE_VARIABLE, Scope: controlplanev1.ConfigScope_CONFIG_SCOPE_SERVICE,
		ServiceId: "s1", Key: "PORT", Value: "8080",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := resp.Msg.GetEntry()
	if got.GetKey() != "PORT" || got.GetValue() != "8080" {
		t.Errorf("entry = %+v, want PORT=8080 readable", got)
	}
	if got.GetType() != controlplanev1.ConfigType_CONFIG_TYPE_VARIABLE || got.GetScope() != controlplanev1.ConfigScope_CONFIG_SCOPE_SERVICE {
		t.Errorf("type/scope not round-tripped: %v / %v", got.GetType(), got.GetScope())
	}
}

// Guards the write-only contract on the wire: a SetConfig response for a SECRET must never
// carry the plaintext, even serialized.
func TestHandler_SetConfig_SecretResponseOmitsValue(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.SetConfig(context.Background(), connect.NewRequest(&controlplanev1.SetConfigRequest{
		Type: controlplanev1.ConfigType_CONFIG_TYPE_SECRET, Scope: controlplanev1.ConfigScope_CONFIG_SCOPE_ENVIRONMENT,
		EnvironmentId: "e1", Key: "TOKEN", Value: "super-secret-value",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GetEntry().GetValue() != "" {
		t.Error("secret entry must not carry a value")
	}
	raw, err := proto.Marshal(resp.Msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte("super-secret-value")) {
		t.Error("SetConfig response leaked the secret value; secrets are write-only")
	}
}

func TestHandler_ListConfig(t *testing.T) {
	h := &handler{svc: &fakeService{list: []Entry{
		{ID: "id1", Type: TypeVariable, Scope: ScopeService, Key: "A", Value: "1"},
		{ID: "id2", Type: TypeSecret, Scope: ScopeEnvironment, Key: "B"},
	}}}
	resp, err := h.ListConfig(context.Background(), connect.NewRequest(&controlplanev1.ListConfigRequest{ServiceId: "s1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries := resp.Msg.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.GetType() == controlplanev1.ConfigType_CONFIG_TYPE_SECRET && e.GetValue() != "" {
			t.Errorf("secret %q leaked a value in the list", e.GetKey())
		}
	}
}

func TestHandler_DeleteConfig(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.DeleteConfig(context.Background(), connect.NewRequest(&controlplanev1.DeleteConfigRequest{
		Scope: controlplanev1.ConfigScope_CONFIG_SCOPE_SERVICE, ServiceId: "s1", Key: "TOKEN",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a non-nil response")
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.ListConfig(context.Background(), connect.NewRequest(&controlplanev1.ListConfigRequest{ServiceId: "s1"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

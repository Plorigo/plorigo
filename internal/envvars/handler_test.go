package envvars

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

type fakeService struct {
	list []EnvVar
	err  error
}

func (f *fakeService) Set(_ context.Context, in SetInput) (EnvVar, error) {
	if f.err != nil {
		return EnvVar{}, f.err
	}
	return EnvVar{ID: "id1", ServiceID: in.ServiceID, Key: in.Key, Value: in.Value, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (f *fakeService) List(_ context.Context, _ string) ([]EnvVar, error) {
	return f.list, f.err
}
func (f *fakeService) Delete(_ context.Context, _ DeleteInput) error {
	return f.err
}
func (f *fakeService) SetWithinTx(_ context.Context, _ database.Tx, _ string, _ map[string]string) error {
	return f.err
}

func TestHandler_SetEnvVar(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.SetEnvVar(context.Background(),
		connect.NewRequest(&controlplanev1.SetEnvVarRequest{ServiceId: "e1", Key: "PORT", Value: "8080"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Msg.GetEnvVar().GetKey(); got != "PORT" {
		t.Errorf("key = %q, want PORT", got)
	}
	if got := resp.Msg.GetEnvVar().GetValue(); got != "8080" {
		t.Errorf("value = %q, want 8080", got)
	}
}

func TestHandler_ListEnvVars(t *testing.T) {
	h := &handler{svc: &fakeService{list: []EnvVar{{ID: "id1", Key: "A", Value: "1"}, {ID: "id2", Key: "B", Value: "2"}}}}
	resp, err := h.ListEnvVars(context.Background(),
		connect.NewRequest(&controlplanev1.ListEnvVarsRequest{ServiceId: "e1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(resp.Msg.GetEnvVars()); n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestHandler_DeleteEnvVar(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.DeleteEnvVar(context.Background(),
		connect.NewRequest(&controlplanev1.DeleteEnvVarRequest{ServiceId: "e1", Key: "PORT"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a non-nil response")
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.ListEnvVars(context.Background(),
		connect.NewRequest(&controlplanev1.ListEnvVarsRequest{ServiceId: "e1"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

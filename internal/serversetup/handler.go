package serversetup

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC ServerSetupService to the domain Service: it maps proto
// <-> domain and domain errors -> connect codes. No business logic lives here. It exposes
// only the user-driven lifecycle (inspect / rotate / revoke); Provision, MarkUsed,
// RecordFailedAuth, and OpenPrivateKey are in-process bootstrap-runner concerns with no RPC.
type handler struct {
	svc Service
}

var _ controlplanev1connect.ServerSetupServiceHandler = (*handler)(nil)

func (h *handler) StartSetup(ctx context.Context, req *connect.Request[controlplanev1.StartSetupRequest]) (*connect.Response[controlplanev1.StartSetupResponse], error) {
	m := req.Msg
	run, err := h.svc.StartSetup(ctx, StartSetupInput{
		ServerID: m.GetServerId(),
		Host:     m.GetHost(),
		Port:     int(m.GetPort()),
		Username: m.GetUsername(),
		Auth: BootstrapAuth{
			Password:             m.GetPassword(),
			PrivateKey:           []byte(m.GetPrivateKey()),
			PrivateKeyPassphrase: m.GetPrivateKeyPassphrase(),
		},
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.StartSetupResponse{Run: toProtoRun(run)}), nil
}

func (h *handler) GetSetupRun(ctx context.Context, req *connect.Request[controlplanev1.GetSetupRunRequest]) (*connect.Response[controlplanev1.GetSetupRunResponse], error) {
	run, err := h.svc.GetSetupRun(ctx, req.Msg.GetSetupRunId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetSetupRunResponse{Run: toProtoRun(run)}), nil
}

func (h *handler) ListSetupEvents(ctx context.Context, req *connect.Request[controlplanev1.ListSetupEventsRequest]) (*connect.Response[controlplanev1.ListSetupEventsResponse], error) {
	events, err := h.svc.ListSetupEvents(ctx, req.Msg.GetSetupRunId(), int64(req.Msg.GetAfterSeq()))
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.ServerSetupEvent, 0, len(events))
	for _, e := range events {
		out = append(out, toProtoEvent(e))
	}
	return connect.NewResponse(&controlplanev1.ListSetupEventsResponse{Events: out}), nil
}

func toProtoRun(r SetupRun) *controlplanev1.ServerSetupRun {
	return &controlplanev1.ServerSetupRun{
		Id:            r.ID,
		ServerId:      r.ServerID,
		Status:        r.Status,
		FailureReason: r.FailureReason,
		CreatedAt:     r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     r.UpdatedAt.UTC().Format(time.RFC3339),
		FinishedAt:    formatTimePtr(r.FinishedAt),
	}
}

func toProtoEvent(e SetupEvent) *controlplanev1.ServerSetupEvent {
	return &controlplanev1.ServerSetupEvent{
		Seq:       uint64(e.Seq),
		Step:      e.Step,
		Kind:      e.Kind,
		Status:    e.Status,
		Message:   e.Message,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *handler) GetManagementKey(ctx context.Context, req *connect.Request[controlplanev1.GetManagementKeyRequest]) (*connect.Response[controlplanev1.GetManagementKeyResponse], error) {
	cred, err := h.svc.Get(ctx, req.Msg.GetServerId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetManagementKeyResponse{Key: toProto(cred)}), nil
}

func (h *handler) RotateManagementKey(ctx context.Context, req *connect.Request[controlplanev1.RotateManagementKeyRequest]) (*connect.Response[controlplanev1.RotateManagementKeyResponse], error) {
	cred, err := h.svc.Rotate(ctx, RotateInput{ServerID: req.Msg.GetServerId()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RotateManagementKeyResponse{Key: toProto(cred)}), nil
}

func (h *handler) RevokeManagementKey(ctx context.Context, req *connect.Request[controlplanev1.RevokeManagementKeyRequest]) (*connect.Response[controlplanev1.RevokeManagementKeyResponse], error) {
	if err := h.svc.Revoke(ctx, RevokeInput{ServerID: req.Msg.GetServerId()}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RevokeManagementKeyResponse{}), nil
}

// toProto maps a Credential to its wire form. There is deliberately no private-key field —
// the key is write-only and never returned; only the public key and fingerprint appear.
func toProto(c Credential) *controlplanev1.SSHManagementKey {
	return &controlplanev1.SSHManagementKey{
		ServerId:      c.ServerID,
		Fingerprint:   c.Fingerprint,
		PublicKey:     c.PublicKey,
		RotationState: c.RotationState,
		LastUsedAt:    formatTimePtr(c.LastUsedAt),
		RotatedAt:     formatTimePtr(c.RotatedAt),
		RevokedAt:     formatTimePtr(c.RevokedAt),
		CreatedBy:     deref(c.CreatedBy),
		CreatedAt:     c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

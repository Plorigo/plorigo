package secrets

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC SecretService to the domain Service: it maps proto
// <-> domain and domain errors -> connect codes. No business logic lives here. The
// secret value flows IN on SetSecret and is never mapped back out.
type handler struct {
	svc Service
}

var _ controlplanev1connect.SecretServiceHandler = (*handler)(nil)

func (h *handler) SetSecret(ctx context.Context, req *connect.Request[controlplanev1.SetSecretRequest]) (*connect.Response[controlplanev1.SetSecretResponse], error) {
	sec, err := h.svc.Set(ctx, SetInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Key:           req.Msg.GetKey(),
		Value:         req.Msg.GetValue(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.SetSecretResponse{Secret: toProto(sec)}), nil
}

func (h *handler) ListSecrets(ctx context.Context, req *connect.Request[controlplanev1.ListSecretsRequest]) (*connect.Response[controlplanev1.ListSecretsResponse], error) {
	secs, err := h.svc.List(ctx, req.Msg.GetEnvironmentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Secret, 0, len(secs))
	for _, sec := range secs {
		out = append(out, toProto(sec))
	}
	return connect.NewResponse(&controlplanev1.ListSecretsResponse{Secrets: out}), nil
}

func (h *handler) DeleteSecret(ctx context.Context, req *connect.Request[controlplanev1.DeleteSecretRequest]) (*connect.Response[controlplanev1.DeleteSecretResponse], error) {
	if err := h.svc.Delete(ctx, DeleteInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Key:           req.Msg.GetKey(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DeleteSecretResponse{}), nil
}

// toProto maps a Secret to its wire form. There is deliberately no value field — a
// secret value is write-only and never returned.
func toProto(s Secret) *controlplanev1.Secret {
	return &controlplanev1.Secret{
		Id:            s.ID,
		EnvironmentId: s.EnvironmentID,
		Key:           s.Key,
		CreatedAt:     s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

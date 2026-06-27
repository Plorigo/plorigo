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
// only the read-only inspect; Provision, Rotate, Revoke, MarkUsed, RecordFailedAuth, and
// OpenPrivateKey are in-process bootstrap-runner concerns with no RPC (rotate/revoke have to
// install/remove the key on the server, which the runner owns — see serversetup.proto).
type handler struct {
	svc Service
}

var _ controlplanev1connect.ServerSetupServiceHandler = (*handler)(nil)

func (h *handler) GetManagementKey(ctx context.Context, req *connect.Request[controlplanev1.GetManagementKeyRequest]) (*connect.Response[controlplanev1.GetManagementKeyResponse], error) {
	cred, err := h.svc.Get(ctx, req.Msg.GetServerId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetManagementKeyResponse{Key: toProto(cred)}), nil
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

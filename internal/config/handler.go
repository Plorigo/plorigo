package config

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC ConfigService to the domain Service: it maps proto <->
// domain and domain errors -> connect codes. No business logic lives here. A secret value
// flows IN on SetConfig and is never mapped back out (toProto blanks it).
type handler struct {
	svc Service
}

var _ controlplanev1connect.ConfigServiceHandler = (*handler)(nil)

func (h *handler) SetConfig(ctx context.Context, req *connect.Request[controlplanev1.SetConfigRequest]) (*connect.Response[controlplanev1.SetConfigResponse], error) {
	e, err := h.svc.Set(ctx, SetInput{
		Type:          typeFromProto(req.Msg.GetType()),
		Scope:         scopeFromProto(req.Msg.GetScope()),
		ServiceID:     req.Msg.GetServiceId(),
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Key:           req.Msg.GetKey(),
		Value:         req.Msg.GetValue(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.SetConfigResponse{Entry: toProto(e)}), nil
}

func (h *handler) ListConfig(ctx context.Context, req *connect.Request[controlplanev1.ListConfigRequest]) (*connect.Response[controlplanev1.ListConfigResponse], error) {
	entries, err := h.svc.ListForService(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.ConfigEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, toProto(e))
	}
	return connect.NewResponse(&controlplanev1.ListConfigResponse{Entries: out}), nil
}

func (h *handler) DeleteConfig(ctx context.Context, req *connect.Request[controlplanev1.DeleteConfigRequest]) (*connect.Response[controlplanev1.DeleteConfigResponse], error) {
	if err := h.svc.Delete(ctx, DeleteInput{
		Scope:         scopeFromProto(req.Msg.GetScope()),
		ServiceID:     req.Msg.GetServiceId(),
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Key:           req.Msg.GetKey(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DeleteConfigResponse{}), nil
}

// toProto maps an Entry to its wire form. A secret value is write-only, so it is blanked
// here as a backstop (the store already returns no value for secrets).
func toProto(e Entry) *controlplanev1.ConfigEntry {
	value := e.Value
	if e.Type == TypeSecret {
		value = ""
	}
	return &controlplanev1.ConfigEntry{
		Id:            e.ID,
		Type:          typeToProto(e.Type),
		Scope:         scopeToProto(e.Scope),
		ServiceId:     e.ServiceID,
		EnvironmentId: e.EnvironmentID,
		Key:           e.Key,
		Value:         value,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     e.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// typeFromProto / scopeFromProto map the wire enums to domain values, returning "" for an
// unspecified enum so the service rejects it with InvalidInput.
func typeFromProto(t controlplanev1.ConfigType) Type {
	switch t {
	case controlplanev1.ConfigType_CONFIG_TYPE_VARIABLE:
		return TypeVariable
	case controlplanev1.ConfigType_CONFIG_TYPE_SECRET:
		return TypeSecret
	default:
		return ""
	}
}

func scopeFromProto(s controlplanev1.ConfigScope) Scope {
	switch s {
	case controlplanev1.ConfigScope_CONFIG_SCOPE_SERVICE:
		return ScopeService
	case controlplanev1.ConfigScope_CONFIG_SCOPE_ENVIRONMENT:
		return ScopeEnvironment
	default:
		return ""
	}
}

func typeToProto(t Type) controlplanev1.ConfigType {
	if t == TypeSecret {
		return controlplanev1.ConfigType_CONFIG_TYPE_SECRET
	}
	return controlplanev1.ConfigType_CONFIG_TYPE_VARIABLE
}

func scopeToProto(s Scope) controlplanev1.ConfigScope {
	if s == ScopeEnvironment {
		return controlplanev1.ConfigScope_CONFIG_SCOPE_ENVIRONMENT
	}
	return controlplanev1.ConfigScope_CONFIG_SCOPE_SERVICE
}

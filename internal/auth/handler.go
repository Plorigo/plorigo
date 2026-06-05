package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC AuthService to the domain Service. It also owns the
// session cookie — Register/Login set it, Logout clears it — and maps domain errors
// to connect codes. No business logic lives here.
type handler struct {
	svc    Service
	cookie CookieConfig
}

var _ controlplanev1connect.AuthServiceHandler = (*handler)(nil)

func (h *handler) Register(ctx context.Context, req *connect.Request[controlplanev1.RegisterRequest]) (*connect.Response[controlplanev1.RegisterResponse], error) {
	res, err := h.svc.Register(ctx, RegisterInput{
		Email:     req.Msg.GetEmail(),
		Password:  req.Msg.GetPassword(),
		UserAgent: req.Header().Get("User-Agent"),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	resp := connect.NewResponse(&controlplanev1.RegisterResponse{User: userToProto(res.User)})
	h.setSession(resp.Header(), res.SessionToken)
	return resp, nil
}

func (h *handler) Login(ctx context.Context, req *connect.Request[controlplanev1.LoginRequest]) (*connect.Response[controlplanev1.LoginResponse], error) {
	res, err := h.svc.Login(ctx, LoginInput{
		Email:     req.Msg.GetEmail(),
		Password:  req.Msg.GetPassword(),
		UserAgent: req.Header().Get("User-Agent"),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	resp := connect.NewResponse(&controlplanev1.LoginResponse{User: userToProto(res.User)})
	h.setSession(resp.Header(), res.SessionToken)
	return resp, nil
}

func (h *handler) Logout(ctx context.Context, req *connect.Request[controlplanev1.LogoutRequest]) (*connect.Response[controlplanev1.LogoutResponse], error) {
	if err := h.svc.Logout(ctx, cookieValue(req.Header(), h.cookie.Name)); err != nil {
		return nil, problem.ToConnect(err)
	}
	resp := connect.NewResponse(&controlplanev1.LogoutResponse{})
	h.clearSession(resp.Header())
	return resp, nil
}

func (h *handler) RequestPasswordReset(ctx context.Context, req *connect.Request[controlplanev1.RequestPasswordResetRequest]) (*connect.Response[controlplanev1.RequestPasswordResetResponse], error) {
	if err := h.svc.RequestPasswordReset(ctx, req.Msg.GetEmail()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RequestPasswordResetResponse{}), nil
}

func (h *handler) ResetPassword(ctx context.Context, req *connect.Request[controlplanev1.ResetPasswordRequest]) (*connect.Response[controlplanev1.ResetPasswordResponse], error) {
	if err := h.svc.ResetPassword(ctx, req.Msg.GetToken(), req.Msg.GetNewPassword()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ResetPasswordResponse{}), nil
}

func (h *handler) RequestEmailVerification(ctx context.Context, _ *connect.Request[controlplanev1.RequestEmailVerificationRequest]) (*connect.Response[controlplanev1.RequestEmailVerificationResponse], error) {
	p := principal.FromContext(ctx)
	if !p.IsAuthenticated() {
		return nil, unauthenticated()
	}
	if err := h.svc.RequestEmailVerification(ctx, p.UserID); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RequestEmailVerificationResponse{}), nil
}

func (h *handler) VerifyEmail(ctx context.Context, req *connect.Request[controlplanev1.VerifyEmailRequest]) (*connect.Response[controlplanev1.VerifyEmailResponse], error) {
	if err := h.svc.VerifyEmail(ctx, req.Msg.GetToken()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.VerifyEmailResponse{}), nil
}

func (h *handler) CurrentUser(ctx context.Context, _ *connect.Request[controlplanev1.CurrentUserRequest]) (*connect.Response[controlplanev1.CurrentUserResponse], error) {
	p := principal.FromContext(ctx)
	if !p.IsAuthenticated() {
		return nil, unauthenticated()
	}
	u, err := h.svc.CurrentUser(ctx, p.UserID)
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CurrentUserResponse{User: userToProto(u)}), nil
}

func (h *handler) CreateAPIToken(ctx context.Context, req *connect.Request[controlplanev1.CreateAPITokenRequest]) (*connect.Response[controlplanev1.CreateAPITokenResponse], error) {
	p := principal.FromContext(ctx)
	if !p.IsAuthenticated() {
		return nil, unauthenticated()
	}
	res, err := h.svc.CreateAPIToken(ctx, p.UserID, req.Msg.GetName())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateAPITokenResponse{
		Token:    res.Token,
		ApiToken: apiTokenToProto(res.Meta),
	}), nil
}

func (h *handler) ListAPITokens(ctx context.Context, _ *connect.Request[controlplanev1.ListAPITokensRequest]) (*connect.Response[controlplanev1.ListAPITokensResponse], error) {
	p := principal.FromContext(ctx)
	if !p.IsAuthenticated() {
		return nil, unauthenticated()
	}
	toks, err := h.svc.ListAPITokens(ctx, p.UserID)
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.APIToken, 0, len(toks))
	for _, t := range toks {
		out = append(out, apiTokenToProto(t))
	}
	return connect.NewResponse(&controlplanev1.ListAPITokensResponse{ApiTokens: out}), nil
}

func (h *handler) RevokeAPIToken(ctx context.Context, req *connect.Request[controlplanev1.RevokeAPITokenRequest]) (*connect.Response[controlplanev1.RevokeAPITokenResponse], error) {
	p := principal.FromContext(ctx)
	if !p.IsAuthenticated() {
		return nil, unauthenticated()
	}
	if err := h.svc.RevokeAPIToken(ctx, p.UserID, req.Msg.GetId()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RevokeAPITokenResponse{}), nil
}

func (h *handler) setSession(header http.Header, token string) {
	header.Set("Set-Cookie", (&http.Cookie{
		Name:     h.cookie.Name,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: h.cookie.SameSite,
		MaxAge:   h.cookie.MaxAgeSeconds,
	}).String())
}

func (h *handler) clearSession(header http.Header) {
	header.Set("Set-Cookie", (&http.Cookie{
		Name:     h.cookie.Name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: h.cookie.SameSite,
		MaxAge:   -1,
	}).String())
}

func cookieValue(header http.Header, name string) string {
	r := http.Request{Header: header}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func unauthenticated() error {
	return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
}

func userToProto(u User) *controlplanev1.User {
	return &controlplanev1.User{
		Id:            u.ID,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func apiTokenToProto(t APIToken) *controlplanev1.APIToken {
	return &controlplanev1.APIToken{
		Id:          t.ID,
		Name:        t.Name,
		TokenPrefix: t.TokenPrefix,
		CreatedAt:   t.CreatedAt.UTC().Format(time.RFC3339),
		LastUsedAt:  formatPtr(t.LastUsedAt),
		ExpiresAt:   formatPtr(t.ExpiresAt),
	}
}

func formatPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

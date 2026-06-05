package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/principal"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

func newTestHandler(cfg Config) *handler {
	svc, _, _, _, _ := newTestService(cfg)
	return &handler{svc: svc, cookie: CookieConfig{Name: "plorigo_session", SameSite: http.SameSiteLaxMode, MaxAgeSeconds: 3600}}
}

func TestHandlerRegisterSetsSessionCookie(t *testing.T) {
	h := newTestHandler(Config{AllowOpenRegistration: true})
	resp, err := h.Register(context.Background(), connect.NewRequest(&controlplanev1.RegisterRequest{Email: "a@b.com", Password: "supersecret"}))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	sc := resp.Header().Get("Set-Cookie")
	for _, want := range []string{"plorigo_session=", "HttpOnly", "SameSite=Lax", "Max-Age=3600"} {
		if !strings.Contains(sc, want) {
			t.Fatalf("Set-Cookie = %q, missing %q", sc, want)
		}
	}
}

func TestHandlerLogoutClearsCookie(t *testing.T) {
	h := newTestHandler(Config{AllowOpenRegistration: true})
	if _, err := h.Register(context.Background(), connect.NewRequest(&controlplanev1.RegisterRequest{Email: "a@b.com", Password: "supersecret"})); err != nil {
		t.Fatalf("Register: %v", err)
	}
	resp, err := h.Logout(context.Background(), connect.NewRequest(&controlplanev1.LogoutRequest{}))
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	sc := resp.Header().Get("Set-Cookie")
	if !strings.Contains(sc, "plorigo_session=") || !strings.Contains(sc, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want a cleared cookie (Max-Age=0)", sc)
	}
}

func TestHandlerCurrentUserRequiresAuth(t *testing.T) {
	h := newTestHandler(Config{AllowOpenRegistration: true})
	_, err := h.CurrentUser(context.Background(), connect.NewRequest(&controlplanev1.CurrentUserRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", connect.CodeOf(err))
	}
}

func TestHandlerCurrentUserWithPrincipal(t *testing.T) {
	h := newTestHandler(Config{AllowOpenRegistration: true})
	rr, err := h.Register(context.Background(), connect.NewRequest(&controlplanev1.RegisterRequest{Email: "a@b.com", Password: "supersecret"}))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx := principal.NewContext(context.Background(), principal.Principal{UserID: rr.Msg.GetUser().GetId(), Method: principal.MethodSession})
	resp, err := h.CurrentUser(ctx, connect.NewRequest(&controlplanev1.CurrentUserRequest{}))
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if got := resp.Msg.GetUser().GetEmail(); got != "a@b.com" {
		t.Fatalf("email = %q, want a@b.com", got)
	}
}

func TestHandlerMapsInvalidInputToConnectCode(t *testing.T) {
	h := newTestHandler(Config{AllowOpenRegistration: true})
	_, err := h.Register(context.Background(), connect.NewRequest(&controlplanev1.RegisterRequest{Email: "a@b.com", Password: "short"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", connect.CodeOf(err))
	}
}

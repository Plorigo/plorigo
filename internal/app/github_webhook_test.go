package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plorigo/plorigo/internal/githubapp"
)

// stubGitHubApp is a minimal githubapp.Service for the webhook tests: WebhookSecret returns a fixed
// secret (empty = not configured, fails closed). The other methods are unused here.
type stubGitHubApp struct{ secret string }

func (s stubGitHubApp) Current(context.Context) (githubapp.Credentials, error) {
	return githubapp.Credentials{WebhookSecret: s.secret, Configured: s.secret != ""}, nil
}
func (s stubGitHubApp) AppCredentials(context.Context) (string, string, bool) { return "", "", false }
func (s stubGitHubApp) WebhookSecret(context.Context) string                  { return s.secret }
func (s stubGitHubApp) AppConfig(context.Context) (string, string, bool)      { return "", "", false }
func (s stubGitHubApp) InstallURL(context.Context, string) (string, bool)     { return "", false }
func (s stubGitHubApp) BeginRegistration(context.Context, githubapp.BeginRegistrationInput) (githubapp.BeginRegistrationResult, error) {
	return githubapp.BeginRegistrationResult{}, nil
}
func (s stubGitHubApp) CompleteRegistration(context.Context, githubapp.CompleteRegistrationInput) (githubapp.CompleteRegistrationResult, error) {
	return githubapp.CompleteRegistrationResult{}, nil
}

// sign computes the X-Hub-Signature-256 header GitHub sends, for the webhook handler tests.
func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postWebhook(t *testing.T, a *App, event, sig, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/github/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	w := httptest.NewRecorder()
	a.githubWebhookHandler().ServeHTTP(w, req)
	return w
}

// A forged or absent signature is rejected before the payload is parsed or dispatched — so these
// paths never touch the webhooks service or the DB, and a literal App is enough.
func TestGitHubWebhookHandler_RejectsBadSignature(t *testing.T) {
	a := &App{githubapp: stubGitHubApp{secret: "s3cr3t"}}
	if w := postWebhook(t, a, "pull_request", "sha256=deadbeef", `{"action":"opened"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: code = %d, want 401", w.Code)
	}
	if w := postWebhook(t, a, "pull_request", "", `{"action":"opened"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature: code = %d, want 401", w.Code)
	}
}

// With no secret configured, verification fails closed — even a "correctly" signed body is rejected.
func TestGitHubWebhookHandler_FailsClosedWithoutSecret(t *testing.T) {
	a := &App{githubapp: stubGitHubApp{}}
	body := `{"action":"opened"}`
	if w := postWebhook(t, a, "pull_request", sign("", body), body); w.Code != http.StatusUnauthorized {
		t.Fatalf("no secret configured: code = %d, want 401 (fail closed)", w.Code)
	}
}

// A valid signature on a ping is acknowledged (200) without needing the webhooks service.
func TestGitHubWebhookHandler_PingAcknowledged(t *testing.T) {
	secret := "s3cr3t"
	a := &App{githubapp: stubGitHubApp{secret: secret}}
	body := `{"zen":"hello"}`
	if w := postWebhook(t, a, "ping", sign(secret, body), body); w.Code != http.StatusOK {
		t.Fatalf("ping: code = %d, want 200", w.Code)
	}
}

// A valid signature on an event we don't act on is acknowledged (204) so GitHub doesn't redeliver.
func TestGitHubWebhookHandler_UnhandledEventNoContent(t *testing.T) {
	secret := "s3cr3t"
	a := &App{githubapp: stubGitHubApp{secret: secret}}
	body := `{"ref":"refs/heads/main"}`
	if w := postWebhook(t, a, "push", sign(secret, body), body); w.Code != http.StatusNoContent {
		t.Fatalf("unhandled event: code = %d, want 204", w.Code)
	}
}

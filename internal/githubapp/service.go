package githubapp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// stateTTL bounds the manifest-registration handshake, like the OAuth/install state.
const stateTTL = 10 * time.Minute

// regState is the sealed payload carried across the manifest redirect: it binds the flow to the
// initiating workspace + user, with a nonce echoed back as the `state` param and an expiry.
type regState struct {
	WorkspaceID string `json:"w"`
	UserID      string `json:"u"`
	Nonce       string `json:"n"`
	ExpiresAt   int64  `json:"e"`
}

type service struct {
	tx         TxRunner
	store      Store
	box        Sealer
	gh         ManifestConverter
	authorizer authz.Authorizer
	audit      Recorder
	env        EnvConfig
	baseURL    string
	webhookURL string
	log        *slog.Logger

	mu     sync.Mutex
	cached *Credentials // resolved creds cache; cleared on (re)registration
}

func newService(tx TxRunner, store Store, box Sealer, gh ManifestConverter, authorizer authz.Authorizer, audit Recorder, env EnvConfig, baseURL, webhookURL string, log *slog.Logger) *service {
	return &service{tx: tx, store: store, box: box, gh: gh, authorizer: authorizer, audit: audit, env: env, baseURL: baseURL, webhookURL: webhookURL, log: log}
}

var _ Service = (*service)(nil)

// Current resolves the active App credentials: env first (operator-set, takes precedence), else the
// sealed stored config (opened in-process), else not configured. The result is cached until the next
// (re)registration, so repeated resolver calls don't re-read/unseal on every use.
func (s *service) Current(ctx context.Context) (Credentials, error) {
	s.mu.Lock()
	if s.cached != nil {
		c := *s.cached
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	if s.env.configured() {
		return s.cache(Credentials{
			AppID: s.env.AppID, Slug: s.env.Slug, PrivateKeyPEM: s.env.PrivateKeyPEM,
			WebhookSecret: s.env.WebhookSecret, Configured: true, Source: "env",
		}), nil
	}

	stored, ok, err := s.store.GetSealedConfig(ctx)
	if err != nil {
		return Credentials{}, problem.Internalf(err, "load github app config")
	}
	if !ok {
		return s.cache(Credentials{Configured: false}), nil
	}
	pem, err := s.box.Open(stored.SealedPrivateKey)
	if err != nil {
		return Credentials{}, problem.Internalf(err, "open github app key")
	}
	secret, err := s.box.Open(stored.SealedWebhookSecret)
	if err != nil {
		return Credentials{}, problem.Internalf(err, "open github webhook secret")
	}
	clientSecret := ""
	if len(stored.SealedClientSecret) > 0 {
		cs, oerr := s.box.Open(stored.SealedClientSecret)
		if oerr != nil {
			return Credentials{}, problem.Internalf(oerr, "open github client secret")
		}
		clientSecret = string(cs)
	}
	return s.cache(Credentials{
		AppID: stored.AppID, Slug: stored.AppSlug, PrivateKeyPEM: string(pem),
		WebhookSecret: string(secret), ClientID: stored.ClientID, ClientSecret: clientSecret,
		Configured: true, Source: "stored",
	}), nil
}

func (s *service) cache(c Credentials) Credentials {
	s.mu.Lock()
	cp := c
	s.cached = &cp
	s.mu.Unlock()
	return c
}

func (s *service) clearCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// AppCredentials supplies the app id + private key PEM to the github client (its AppCredentials
// hook). ok is false when no App is configured or resolution failed (the client then reports the App
// as not configured, which is the safe default).
func (s *service) AppCredentials(ctx context.Context) (appID, privateKeyPEM string, ok bool) {
	c, err := s.Current(ctx)
	if err != nil {
		s.log.Error("resolve github app credentials", "err", err)
		return "", "", false
	}
	if !c.Configured {
		return "", "", false
	}
	return c.AppID, c.PrivateKeyPEM, true
}

// WebhookSecret returns the active webhook secret (empty when not configured, so verification fails
// closed).
func (s *service) WebhookSecret(ctx context.Context) string {
	c, err := s.Current(ctx)
	if err != nil {
		s.log.Error("resolve github webhook secret", "err", err)
		return ""
	}
	return c.WebhookSecret
}

// AppConfig reports the non-secret App identity for the UI + install flow.
func (s *service) AppConfig(ctx context.Context) (appID, slug string, configured bool) {
	c, err := s.Current(ctx)
	if err != nil {
		s.log.Error("resolve github app config", "err", err)
		return "", "", false
	}
	return c.AppID, c.Slug, c.Configured
}

// InstallURL builds the GitHub App installation URL carrying state, or ok=false when no App is
// configured (no slug to install).
func (s *service) InstallURL(ctx context.Context, state string) (string, bool) {
	_, slug, configured := s.AppConfig(ctx)
	if !configured || slug == "" {
		return "", false
	}
	return "https://github.com/apps/" + slug + "/installations/new?state=" + url.QueryEscape(state), true
}

// BeginRegistration authorizes the caller (owner of the workspace), refuses when the App is set via
// env, and returns the manifest + sealed state to drive GitHub's create-from-manifest page.
func (s *service) BeginRegistration(ctx context.Context, in BeginRegistrationInput) (BeginRegistrationResult, error) {
	if _, err := id.Parse(in.WorkspaceID); err != nil {
		return BeginRegistrationResult{}, problem.InvalidInput("a valid workspace_id is required")
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionGitHubAppRegister, authz.Resource{Type: "github_app", WorkspaceID: in.WorkspaceID}); err != nil {
		return BeginRegistrationResult{}, err
	}
	if s.env.configured() {
		return BeginRegistrationResult{}, problem.InvalidInput("the GitHub App is configured via environment variables; unset GITHUB_APP_* to register it from the dashboard")
	}

	manifestJSON, err := buildManifest(s.baseURL, s.webhookURL)
	if err != nil {
		return BeginRegistrationResult{}, problem.Internalf(err, "build manifest")
	}
	nonce, err := newNonce()
	if err != nil {
		return BeginRegistrationResult{}, problem.Internalf(err, "begin registration")
	}
	payload, err := json.Marshal(regState{WorkspaceID: in.WorkspaceID, UserID: caller.UserID, Nonce: nonce, ExpiresAt: time.Now().Add(stateTTL).Unix()})
	if err != nil {
		return BeginRegistrationResult{}, problem.Internalf(err, "begin registration")
	}
	sealed, err := s.box.Seal(payload)
	if err != nil {
		return BeginRegistrationResult{}, problem.Internalf(err, "seal registration state")
	}

	formAction := "https://github.com/settings/apps/new?state=" + url.QueryEscape(nonce)
	if org := strings.TrimSpace(in.Org); org != "" {
		formAction = "https://github.com/organizations/" + url.PathEscape(org) + "/settings/apps/new?state=" + url.QueryEscape(nonce)
	}
	return BeginRegistrationResult{ManifestJSON: manifestJSON, FormAction: formAction, State: base64.RawURLEncoding.EncodeToString(sealed)}, nil
}

// CompleteRegistration verifies the sealed state, exchanges the manifest code for the new App's
// credentials, seals and stores them (replacing any prior App), and audits. Returns the new App's
// slug for the UI.
func (s *service) CompleteRegistration(ctx context.Context, in CompleteRegistrationInput) (CompleteRegistrationResult, error) {
	if strings.TrimSpace(in.Code) == "" {
		return CompleteRegistrationResult{}, problem.InvalidInput("missing manifest code")
	}
	st, err := s.openState(in.CookieState)
	if err != nil {
		return CompleteRegistrationResult{}, err
	}
	if in.State == "" || st.Nonce != in.State {
		return CompleteRegistrationResult{}, problem.InvalidInput("registration state mismatch; please try again")
	}
	if time.Now().Unix() > st.ExpiresAt {
		return CompleteRegistrationResult{}, problem.InvalidInput("the registration request expired; please try again")
	}
	caller := principal.FromContext(ctx)
	if !caller.IsAuthenticated() || caller.UserID != st.UserID {
		return CompleteRegistrationResult{}, problem.PermissionDenied("this registration was started by a different session")
	}
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionGitHubAppRegister, authz.Resource{Type: "github_app", WorkspaceID: st.WorkspaceID}); err != nil {
		return CompleteRegistrationResult{}, err
	}
	if s.env.configured() {
		return CompleteRegistrationResult{}, problem.InvalidInput("the GitHub App is configured via environment variables; unset GITHUB_APP_* to register it from the dashboard")
	}

	conv, err := s.gh.ConvertManifest(ctx, in.Code)
	if err != nil {
		return CompleteRegistrationResult{}, mapGitHubErr(err)
	}
	sealedKey, err := s.box.Seal([]byte(conv.PrivateKeyPEM))
	if err != nil {
		return CompleteRegistrationResult{}, problem.Internalf(err, "seal app key")
	}
	sealedSecret, err := s.box.Seal([]byte(conv.WebhookSecret))
	if err != nil {
		return CompleteRegistrationResult{}, problem.Internalf(err, "seal webhook secret")
	}
	sealedClientSecret, err := s.box.Seal([]byte(conv.ClientSecret))
	if err != nil {
		return CompleteRegistrationResult{}, problem.Internalf(err, "seal client secret")
	}
	appID := strconv.FormatInt(conv.AppID, 10)

	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if txErr := s.store.UpsertConfig(ctx, tx, ConfigWrite{
			AppID: appID, AppSlug: conv.Slug, ClientID: conv.ClientID,
			SealedPrivateKey: sealedKey, SealedWebhookSecret: sealedSecret, SealedClientSecret: sealedClientSecret,
			CreatedBy: caller.UserID,
		}); txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "github_app.register", "github_app", appID, st.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return CompleteRegistrationResult{}, problem.Internalf(err, "store github app")
	}
	s.clearCache()
	// Log the non-secret identity only — never the key or webhook secret.
	s.log.Info("github app registered", "app_id", appID, "slug", conv.Slug, "workspace_id", st.WorkspaceID, "actor", caller.UserID)
	return CompleteRegistrationResult{Slug: conv.Slug, AppID: appID}, nil
}

func (s *service) openState(cookie string) (regState, error) {
	if strings.TrimSpace(cookie) == "" {
		return regState{}, problem.InvalidInput("missing registration state; please try again")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return regState{}, problem.InvalidInput("invalid registration state; please try again")
	}
	plain, err := s.box.Open(sealed)
	if err != nil {
		return regState{}, problem.InvalidInput("invalid registration state; please try again")
	}
	var st regState
	if err := json.Unmarshal(plain, &st); err != nil {
		return regState{}, problem.InvalidInput("invalid registration state; please try again")
	}
	return st, nil
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// mapGitHubErr turns the github client's error into a plain-English problem (the conversion call can
// fail on an expired/used code or a transport error).
func mapGitHubErr(err error) error {
	return problem.Internalf(err, "register github app")
}

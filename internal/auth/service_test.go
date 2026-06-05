package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/passwd"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// ---- fakes -----------------------------------------------------------------

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

type fakeAudit struct{ actions []string }

func (f *fakeAudit) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.actions = append(f.actions, action)
	return nil
}

type sentEmail struct{ to, subject, body string }

type fakeMailer struct{ sent []sentEmail }

func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	f.sent = append(f.sent, sentEmail{to, subject, body})
	return nil
}

type fakeWorkspace struct{ calls int }

func (f *fakeWorkspace) CreateInitialWorkspace(_ context.Context, _ database.Tx, userID, _, _ string) (string, error) {
	f.calls++
	return "ws-" + userID, nil
}

type fakeTok struct {
	id, userID, purpose string
	consumed            bool
}

type fakeAPI struct {
	hash, userID string
	meta         APIToken
	revoked      bool
}

type fakeStore struct {
	usersByEmail map[string]*storedUser
	usersByID    map[string]*storedUser
	sessions     map[string]string // string(hash) -> userID
	apis         []*fakeAPI
	tokens       map[string]*fakeTok // string(hash) -> token
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		usersByEmail: map[string]*storedUser{},
		usersByID:    map[string]*storedUser{},
		sessions:     map[string]string{},
		tokens:       map[string]*fakeTok{},
	}
}

func (s *fakeStore) CreateUser(_ context.Context, _ database.Tx, email, passwordHash string) (User, error) {
	if _, ok := s.usersByEmail[email]; ok {
		return User{}, problem.AlreadyExists("an account with that email already exists")
	}
	u := &storedUser{User: User{ID: id.New().String(), Email: email, CreatedAt: time.Now()}, PasswordHash: passwordHash}
	s.usersByEmail[email] = u
	s.usersByID[u.ID] = u
	return u.User, nil
}

func (s *fakeStore) GetUserByEmail(_ context.Context, email string) (storedUser, error) {
	u, ok := s.usersByEmail[email]
	if !ok {
		return storedUser{}, errNoUser
	}
	return *u, nil
}

func (s *fakeStore) GetUserByID(_ context.Context, userID string) (User, error) {
	u, ok := s.usersByID[userID]
	if !ok {
		return User{}, errNoUser
	}
	return u.User, nil
}

func (s *fakeStore) CountUsers(context.Context) (int64, error) { return int64(len(s.usersByID)), nil }

func (s *fakeStore) SetPassword(_ context.Context, _ database.Tx, userID, passwordHash string) error {
	if u, ok := s.usersByID[userID]; ok {
		u.PasswordHash = passwordHash
	}
	return nil
}

func (s *fakeStore) SetEmailVerified(_ context.Context, _ database.Tx, userID string) error {
	if u, ok := s.usersByID[userID]; ok {
		u.EmailVerified = true
	}
	return nil
}

func (s *fakeStore) CreateSession(_ context.Context, _ database.Tx, userID string, tokenHash []byte, _ string, _ time.Time) error {
	s.sessions[string(tokenHash)] = userID
	return nil
}

func (s *fakeStore) SessionUser(_ context.Context, tokenHash []byte) (string, error) {
	if uid, ok := s.sessions[string(tokenHash)]; ok {
		return uid, nil
	}
	return "", errNoSession
}

func (s *fakeStore) RevokeSession(_ context.Context, _ database.Tx, tokenHash []byte) error {
	delete(s.sessions, string(tokenHash))
	return nil
}

func (s *fakeStore) RevokeAllSessions(_ context.Context, _ database.Tx, userID string) error {
	for h, uid := range s.sessions {
		if uid == userID {
			delete(s.sessions, h)
		}
	}
	return nil
}

func (s *fakeStore) CreateAPIToken(_ context.Context, _ database.Tx, userID, name string, tokenHash []byte, prefix string, _ *time.Time) (APIToken, error) {
	meta := APIToken{ID: id.New().String(), Name: name, TokenPrefix: prefix, CreatedAt: time.Now()}
	s.apis = append(s.apis, &fakeAPI{hash: string(tokenHash), userID: userID, meta: meta})
	return meta, nil
}

func (s *fakeStore) APITokenUser(_ context.Context, tokenHash []byte) (string, error) {
	for _, a := range s.apis {
		if a.hash == string(tokenHash) && !a.revoked {
			return a.userID, nil
		}
	}
	return "", errNoAPIToken
}

func (s *fakeStore) ListAPITokens(_ context.Context, userID string) ([]APIToken, error) {
	out := []APIToken{}
	for _, a := range s.apis {
		if a.userID == userID && !a.revoked {
			out = append(out, a.meta)
		}
	}
	return out, nil
}

func (s *fakeStore) RevokeAPIToken(_ context.Context, _ database.Tx, userID, tokenID string) error {
	for _, a := range s.apis {
		if a.userID == userID && a.meta.ID == tokenID {
			a.revoked = true
		}
	}
	return nil
}

func (s *fakeStore) CreateUserToken(_ context.Context, _ database.Tx, userID, purpose string, tokenHash []byte, _ time.Time) error {
	s.tokens[string(tokenHash)] = &fakeTok{id: id.New().String(), userID: userID, purpose: purpose}
	return nil
}

func (s *fakeStore) GetUserToken(_ context.Context, tokenHash []byte, purpose string) (userToken, error) {
	t, ok := s.tokens[string(tokenHash)]
	if !ok || t.consumed || t.purpose != purpose {
		return userToken{}, errNoToken
	}
	return userToken{TokenID: t.id, UserID: t.userID}, nil
}

func (s *fakeStore) ConsumeUserToken(_ context.Context, _ database.Tx, tokenID string) error {
	for _, t := range s.tokens {
		if t.id == tokenID {
			t.consumed = true
		}
	}
	return nil
}

// ---- helpers ---------------------------------------------------------------

func newTestService(cfg Config) (*service, *fakeStore, *fakeAudit, *fakeMailer, *fakeWorkspace) {
	store := newFakeStore()
	audit := &fakeAudit{}
	mailer := &fakeMailer{}
	ws := &fakeWorkspace{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newService(cfg, fakeTx{}, store, audit, mailer, ws, log), store, audit, mailer, ws
}

func isKind(err error, k problem.Kind) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == k
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func extractToken(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("no token in email body: %q", body)
	}
	tok := body[i+len("token="):]
	if j := strings.IndexAny(tok, " \n\t"); j >= 0 {
		tok = tok[:j]
	}
	return tok
}

// ---- tests -----------------------------------------------------------------

func TestRegisterCreatesUserWorkspaceSessionAudit(t *testing.T) {
	svc, store, audit, _, ws := newTestService(Config{AllowOpenRegistration: true})
	res, err := svc.Register(context.Background(), RegisterInput{Email: "Alice@Example.com", Password: "supersecret"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if res.SessionToken == "" {
		t.Fatal("expected a session token")
	}
	if res.User.Email != "alice@example.com" {
		t.Fatalf("email not normalized: %q", res.User.Email)
	}
	if ws.calls != 1 {
		t.Fatalf("bootstrap workspace calls = %d, want 1", ws.calls)
	}
	su := store.usersByID[res.User.ID]
	if su.PasswordHash == "" || su.PasswordHash == "supersecret" {
		t.Fatal("password must be stored hashed, not in plaintext")
	}
	if err := passwd.Verify("supersecret", su.PasswordHash); err != nil {
		t.Fatalf("stored hash does not verify: %v", err)
	}
	if !contains(audit.actions, "user.register") {
		t.Fatalf("missing user.register audit, got %v", audit.actions)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	svc, _, _, _, _ := newTestService(Config{AllowOpenRegistration: true})
	_, err := svc.Register(context.Background(), RegisterInput{Email: "a@b.com", Password: "short"})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc, _, _, _, _ := newTestService(Config{AllowOpenRegistration: true})
	mustRegister(t, svc, "a@b.com", "supersecret")
	_, err := svc.Register(context.Background(), RegisterInput{Email: "a@b.com", Password: "supersecret"})
	if !isKind(err, problem.KindAlreadyExists) {
		t.Fatalf("got %v, want AlreadyExists", err)
	}
}

func TestRegisterClosedAfterFirstUser(t *testing.T) {
	svc, _, _, _, _ := newTestService(Config{AllowOpenRegistration: false})
	mustRegister(t, svc, "first@b.com", "supersecret") // bootstrap user allowed
	_, err := svc.Register(context.Background(), RegisterInput{Email: "second@b.com", Password: "supersecret"})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
}

func TestLogin(t *testing.T) {
	svc, _, _, _, _ := newTestService(Config{AllowOpenRegistration: true})
	mustRegister(t, svc, "a@b.com", "supersecret")

	if _, err := svc.Login(context.Background(), LoginInput{Email: "a@b.com", Password: "wrong"}); !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("wrong password: got %v, want InvalidInput", err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Email: "missing@b.com", Password: "whatever"}); !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("unknown user: got %v, want uniform InvalidInput", err)
	}
	res, err := svc.Login(context.Background(), LoginInput{Email: "a@b.com", Password: "supersecret"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.SessionToken == "" {
		t.Fatal("expected a session token")
	}
}

func TestResolveSession(t *testing.T) {
	svc, _, _, _, _ := newTestService(Config{AllowOpenRegistration: true})
	res := mustRegister(t, svc, "a@b.com", "supersecret")

	p, err := svc.ResolveSession(context.Background(), res.SessionToken)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if p.UserID != res.User.ID || p.Method != principal.MethodSession {
		t.Fatalf("principal = %+v", p)
	}
	if p2, _ := svc.ResolveSession(context.Background(), "bogus"); p2.IsAuthenticated() {
		t.Fatal("bogus token must resolve to anonymous")
	}
	if p3, _ := svc.ResolveSession(context.Background(), ""); p3.IsAuthenticated() {
		t.Fatal("empty token must resolve to anonymous")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	svc, _, audit, _, _ := newTestService(Config{AllowOpenRegistration: true})
	res := mustRegister(t, svc, "a@b.com", "supersecret")
	if err := svc.Logout(context.Background(), res.SessionToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if p, _ := svc.ResolveSession(context.Background(), res.SessionToken); p.IsAuthenticated() {
		t.Fatal("session should be revoked after logout")
	}
	if !contains(audit.actions, "user.logout") {
		t.Fatalf("missing user.logout audit, got %v", audit.actions)
	}
}

func TestPasswordResetRevokesSessionsAndIsSingleUse(t *testing.T) {
	svc, _, audit, mailer, _ := newTestService(Config{AllowOpenRegistration: true, BaseURL: "http://localhost:5173"})
	res := mustRegister(t, svc, "a@b.com", "supersecret")

	if err := svc.RequestPasswordReset(context.Background(), "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	// Unknown address must not error and must not send mail (no enumeration).
	if err := svc.RequestPasswordReset(context.Background(), "missing@b.com"); err != nil {
		t.Fatalf("reset for unknown email: %v", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("emails sent = %d, want 1", len(mailer.sent))
	}
	token := extractToken(t, mailer.sent[0].body)

	if err := svc.ResetPassword(context.Background(), token, "newsupersecret"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if p, _ := svc.ResolveSession(context.Background(), res.SessionToken); p.IsAuthenticated() {
		t.Fatal("reset must revoke existing sessions")
	}
	if _, err := svc.Login(context.Background(), LoginInput{Email: "a@b.com", Password: "newsupersecret"}); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if err := svc.ResetPassword(context.Background(), token, "anothersecret"); !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("token reuse: got %v, want InvalidInput", err)
	}
	if !contains(audit.actions, "user.password_reset") {
		t.Fatalf("missing user.password_reset audit, got %v", audit.actions)
	}
}

func TestEmailVerification(t *testing.T) {
	svc, store, _, mailer, _ := newTestService(Config{AllowOpenRegistration: true, RequireEmailVerification: true, BaseURL: "http://x"})
	res := mustRegister(t, svc, "a@b.com", "supersecret")
	if len(mailer.sent) != 1 {
		t.Fatalf("verification emails = %d, want 1", len(mailer.sent))
	}
	token := extractToken(t, mailer.sent[0].body)
	if err := svc.VerifyEmail(context.Background(), token); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if !store.usersByID[res.User.ID].EmailVerified {
		t.Fatal("email should be verified")
	}
	if err := svc.VerifyEmail(context.Background(), token); !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("verify reuse: got %v, want InvalidInput", err)
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	svc, _, audit, _, _ := newTestService(Config{AllowOpenRegistration: true})
	res := mustRegister(t, svc, "a@b.com", "supersecret")
	uid := res.User.ID

	nt, err := svc.CreateAPIToken(context.Background(), uid, "ci")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if !strings.HasPrefix(nt.Token, "plk_") {
		t.Fatalf("raw token = %q, want plk_ prefix", nt.Token)
	}
	p, err := svc.ResolveAPIToken(context.Background(), nt.Token)
	if err != nil {
		t.Fatalf("ResolveAPIToken: %v", err)
	}
	if p.UserID != uid || p.Method != principal.MethodAPIToken {
		t.Fatalf("principal = %+v", p)
	}
	if list, _ := svc.ListAPITokens(context.Background(), uid); len(list) != 1 {
		t.Fatalf("token list length = %d, want 1", len(list))
	}
	if err := svc.RevokeAPIToken(context.Background(), uid, nt.Meta.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	if p, _ := svc.ResolveAPIToken(context.Background(), nt.Token); p.IsAuthenticated() {
		t.Fatal("revoked token must not resolve")
	}
	if !contains(audit.actions, "apitoken.create") || !contains(audit.actions, "apitoken.revoke") {
		t.Fatalf("missing apitoken audit, got %v", audit.actions)
	}
}

func mustRegister(t *testing.T, svc *service, email, password string) Authenticated {
	t.Helper()
	res, err := svc.Register(context.Background(), RegisterInput{Email: email, Password: password})
	if err != nil {
		t.Fatalf("Register(%s): %v", email, err)
	}
	return res
}

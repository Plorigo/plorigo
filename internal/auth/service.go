package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/passwd"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	minPasswordLen       = 8
	purposeEmailVerify   = "email_verify"
	purposePasswordReset = "password_reset"
)

const (
	defaultSessionTTL = 720 * time.Hour // 30 days
	resetTTL          = 1 * time.Hour
	verifyTTL         = 24 * time.Hour
)

// service is the auth business logic. It orchestrates ports only.
type service struct {
	cfg       Config
	tx        TxRunner
	store     Store
	audit     Recorder
	mailer    Mailer
	workspace WorkspaceBootstrapper
	log       *slog.Logger
}

func newService(cfg Config, tx TxRunner, store Store, audit Recorder, mailer Mailer, ws WorkspaceBootstrapper, log *slog.Logger) *service {
	return &service{cfg: cfg, tx: tx, store: store, audit: audit, mailer: mailer, workspace: ws, log: log}
}

var _ Service = (*service)(nil)

// errEmailTaken is an internal control-flow signal: the email is already registered.
// It never reaches the client — Register turns it into the same generic result a real
// signup returns (anti-enumeration) and emails the address owner instead.
var errEmailTaken = errors.New("auth: email already registered")

func (s *service) Register(ctx context.Context, in RegisterInput) (Registered, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return Registered{}, err
	}
	if len(in.Password) < minPasswordLen {
		return Registered{}, problem.InvalidInput("password must be at least %d characters", minPasswordLen)
	}
	hash, err := passwd.Hash(in.Password)
	if err != nil {
		return Registered{}, problem.Internalf(err, "hash password")
	}

	var created User
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		// Closed-registration gate, serialized inside the tx so two concurrent
		// first-registrations can't both win the bootstrap.
		if !s.cfg.AllowOpenRegistration {
			if e := s.store.AcquireRegistrationLock(ctx, tx); e != nil {
				return e
			}
			n, e := s.store.CountUsersTx(ctx, tx)
			if e != nil {
				return e
			}
			if n > 0 {
				return problem.PermissionDenied("registration is closed")
			}
		}

		user, e := s.store.CreateUser(ctx, tx, email, hash)
		if e != nil {
			// Anti-enumeration: a duplicate email is not surfaced as an error. Roll the
			// tx back (nothing created) and signal the caller to send the heads-up email.
			var pe *problem.Error
			if errors.As(e, &pe) && pe.Kind == problem.KindAlreadyExists {
				return errEmailTaken
			}
			return e
		}
		// Every new user owns a personal workspace so they always land somewhere. The
		// bootstrapper skips authorization by design and audits the creation itself.
		if _, e = s.workspace.CreateInitialWorkspace(ctx, tx, user.ID, workspaceNameForEmail(email), user.ID); e != nil {
			return e
		}
		if e = s.audit.Record(ctx, tx, "user.register", "user", user.ID, "", user.ID); e != nil {
			return e
		}
		created = user
		return nil
	})

	// Identical result whether the email was new or already taken.
	result := Registered{EmailVerificationRequired: s.cfg.RequireEmailVerification}
	switch {
	case errors.Is(err, errEmailTaken):
		s.sendEmail(ctx, email, "You already have a Plorigo account",
			"Someone just tried to create a Plorigo account with this email, but you already have one.\n\n"+
				"If this was you, log in:\n"+s.appURL("/login")+"\n\nForgot your password?\n"+s.appURL("/forgot"))
		return result, nil
	case err != nil:
		return Registered{}, mapErr(err, "register")
	}

	// New account: registration never establishes a session — the user logs in next
	// (and must verify first when required).
	s.maybeSendVerification(ctx, created)
	s.log.Info("user registered", "user_id", created.ID)
	return result, nil
}

// SeedUser idempotently ensures a verified user with a personal workspace exists,
// for LOCAL DEVELOPMENT login. Re-running it resets the password and verifies the
// address so a seeded login always works. It deliberately bypasses the public
// registration gates (open-registration, email verification, anti-enumeration),
// so it is NOT on the RPC Service interface — only the Module exposes it, and only
// a dev-guarded caller (cmd/seed via App.SeedUser) ever invokes it.
func (s *service) SeedUser(ctx context.Context, email, password string) (User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if len(password) < minPasswordLen {
		return User{}, problem.InvalidInput("password must be at least %d characters", minPasswordLen)
	}
	hash, err := passwd.Hash(password)
	if err != nil {
		return User{}, problem.Internalf(err, "hash password")
	}

	// Existing account: reset its password and verify it so the seeded login works.
	if existing, e := s.store.GetUserByEmail(ctx, email); e == nil {
		err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
			if txErr := s.store.SetPassword(ctx, tx, existing.ID, hash); txErr != nil {
				return txErr
			}
			if !existing.EmailVerified {
				if txErr := s.store.SetEmailVerified(ctx, tx, existing.ID); txErr != nil {
					return txErr
				}
			}
			return s.audit.Record(ctx, tx, "user.seed", "user", existing.ID, "", existing.ID)
		})
		if err != nil {
			return User{}, mapErr(err, "seed user")
		}
		u := existing.User
		u.EmailVerified = true
		return u, nil
	} else if !errors.Is(e, errNoUser) {
		return User{}, mapErr(e, "seed user")
	}

	// New account: create the user, their personal workspace, and mark verified.
	var created User
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		user, e := s.store.CreateUser(ctx, tx, email, hash)
		if e != nil {
			return e
		}
		if _, e = s.workspace.CreateInitialWorkspace(ctx, tx, user.ID, workspaceNameForEmail(email), user.ID); e != nil {
			return e
		}
		if e = s.store.SetEmailVerified(ctx, tx, user.ID); e != nil {
			return e
		}
		if e = s.audit.Record(ctx, tx, "user.seed", "user", user.ID, "", user.ID); e != nil {
			return e
		}
		created = user
		return nil
	})
	if err != nil {
		return User{}, mapErr(err, "seed user")
	}
	created.EmailVerified = true
	return created, nil
}

func (s *service) Login(ctx context.Context, in LoginInput) (Authenticated, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	su, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, errNoUser) {
			// Spend comparable work so a missing user is not detectably faster.
			_, _ = passwd.Hash(in.Password)
			return Authenticated{}, errInvalidCredentials()
		}
		return Authenticated{}, problem.Internalf(err, "login")
	}
	if su.PasswordHash == "" || passwd.Verify(in.Password, su.PasswordHash) != nil {
		return Authenticated{}, errInvalidCredentials()
	}
	// Enforce email verification when the deployment requires it. Checked AFTER the
	// password so only a caller who already proved they own the account learns it is
	// unverified — this branch can't be used to enumerate accounts.
	if s.cfg.RequireEmailVerification && !su.EmailVerified {
		return Authenticated{}, problem.PermissionDenied("please verify your email before signing in")
	}

	rawSession, sessionHash, err := newOpaqueToken()
	if err != nil {
		return Authenticated{}, problem.Internalf(err, "generate session")
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.CreateSession(ctx, tx, su.ID, sessionHash, in.UserAgent, s.sessionExpiry()); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "user.login", "user", su.ID, "", su.ID)
	})
	if err != nil {
		return Authenticated{}, mapErr(err, "login")
	}
	return Authenticated{User: su.User, SessionToken: rawSession}, nil
}

func (s *service) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	actor := principal.FromContext(ctx).UserID
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.RevokeSession(ctx, tx, hashToken(sessionToken)); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "user.logout", "user", actor, "", actor)
	}), "logout")
}

func (s *service) CurrentUser(ctx context.Context, userID string) (User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, errNoUser) {
			return User{}, problem.NotFound("user not found")
		}
		return User{}, problem.Internalf(err, "current user")
	}
	return u, nil
}

func (s *service) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	su, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, errNoUser) {
			return nil // never reveal whether the address has an account
		}
		return problem.Internalf(err, "request password reset")
	}
	raw, hash, err := newOpaqueToken()
	if err != nil {
		return problem.Internalf(err, "generate reset token")
	}
	if err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.CreateUserToken(ctx, tx, su.ID, purposePasswordReset, hash, time.Now().Add(resetTTL)); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "user.password_reset_requested", "user", su.ID, "", su.ID)
	}); err != nil {
		return mapErr(err, "request password reset")
	}
	s.sendEmail(ctx, su.Email, "Reset your Plorigo password",
		"Reset your Plorigo password:\n\n"+s.link("/reset", raw)+"\n\nThis link expires in 1 hour. If you didn't request this, ignore this email.")
	return nil
}

func (s *service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < minPasswordLen {
		return problem.InvalidInput("password must be at least %d characters", minPasswordLen)
	}
	ut, err := s.store.GetUserToken(ctx, hashToken(token), purposePasswordReset)
	if err != nil {
		if errors.Is(err, errNoToken) {
			return problem.InvalidInput("this reset link is invalid or has expired")
		}
		return problem.Internalf(err, "reset password")
	}
	hash, err := passwd.Hash(newPassword)
	if err != nil {
		return problem.Internalf(err, "hash password")
	}
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.SetPassword(ctx, tx, ut.UserID, hash); e != nil {
			return e
		}
		if e := s.store.ConsumeUserToken(ctx, tx, ut.TokenID); e != nil {
			return e
		}
		// A reset invalidates every existing session for safety.
		if e := s.store.RevokeAllSessions(ctx, tx, ut.UserID); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "user.password_reset", "user", ut.UserID, "", ut.UserID)
	}), "reset password")
}

func (s *service) RequestEmailVerification(ctx context.Context, userID string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return mapErr(err, "request email verification")
	}
	if u.EmailVerified {
		return nil
	}
	if err := s.issueVerification(ctx, u); err != nil {
		return mapErr(err, "request email verification")
	}
	return nil
}

func (s *service) VerifyEmail(ctx context.Context, token string) error {
	ut, err := s.store.GetUserToken(ctx, hashToken(token), purposeEmailVerify)
	if err != nil {
		if errors.Is(err, errNoToken) {
			return problem.InvalidInput("this verification link is invalid or has expired")
		}
		return problem.Internalf(err, "verify email")
	}
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.SetEmailVerified(ctx, tx, ut.UserID); e != nil {
			return e
		}
		if e := s.store.ConsumeUserToken(ctx, tx, ut.TokenID); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "user.email_verified", "user", ut.UserID, "", ut.UserID)
	}), "verify email")
}

func (s *service) CreateAPIToken(ctx context.Context, userID, name string) (NewAPIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return NewAPIToken{}, problem.InvalidInput("a token name is required")
	}
	raw, prefix, hash, err := newAPIToken()
	if err != nil {
		return NewAPIToken{}, problem.Internalf(err, "generate api token")
	}
	var meta APIToken
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var e error
		if meta, e = s.store.CreateAPIToken(ctx, tx, userID, name, hash, prefix, nil); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "apitoken.create", "api_token", meta.ID, "", userID)
	})
	if err != nil {
		return NewAPIToken{}, mapErr(err, "create api token")
	}
	return NewAPIToken{Token: raw, Meta: meta}, nil
}

func (s *service) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	return s.store.ListAPITokens(ctx, userID)
}

func (s *service) RevokeAPIToken(ctx context.Context, userID, tokenID string) error {
	if _, err := id.Parse(tokenID); err != nil {
		return problem.InvalidInput("invalid token id")
	}
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.RevokeAPIToken(ctx, tx, userID, tokenID); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "apitoken.revoke", "api_token", tokenID, "", userID)
	}), "revoke api token")
}

func (s *service) ResolveSession(ctx context.Context, sessionToken string) (principal.Principal, error) {
	if sessionToken == "" {
		return principal.Principal{}, nil
	}
	userID, err := s.store.SessionUser(ctx, hashToken(sessionToken))
	if err != nil {
		if errors.Is(err, errNoSession) {
			return principal.Principal{}, nil
		}
		return principal.Principal{}, err
	}
	return principal.Principal{UserID: userID, Method: principal.MethodSession}, nil
}

func (s *service) ResolveAPIToken(ctx context.Context, bearer string) (principal.Principal, error) {
	if bearer == "" {
		return principal.Principal{}, nil
	}
	userID, err := s.store.APITokenUser(ctx, hashToken(bearer))
	if err != nil {
		if errors.Is(err, errNoAPIToken) {
			return principal.Principal{}, nil
		}
		return principal.Principal{}, err
	}
	return principal.Principal{UserID: userID, Method: principal.MethodAPIToken}, nil
}

// issueVerification creates a verification token and emails the link.
func (s *service) issueVerification(ctx context.Context, u User) error {
	raw, hash, err := newOpaqueToken()
	if err != nil {
		return problem.Internalf(err, "generate verification token")
	}
	if err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
		return s.store.CreateUserToken(ctx, tx, u.ID, purposeEmailVerify, hash, time.Now().Add(verifyTTL))
	}); err != nil {
		return err
	}
	s.sendEmail(ctx, u.Email, "Verify your Plorigo email",
		"Verify your Plorigo email:\n\n"+s.link("/verify", raw)+"\n\nThis link expires in 24 hours.")
	return nil
}

// maybeSendVerification emails a verification link after registration when the
// deployment requires it. Failures are logged, not surfaced.
func (s *service) maybeSendVerification(ctx context.Context, u User) {
	if !s.cfg.RequireEmailVerification || u.EmailVerified {
		return
	}
	if err := s.issueVerification(ctx, u); err != nil {
		s.log.Error("send verification email", "err", err)
	}
}

// sendEmail delivers a message best-effort; a failure is logged, never returned,
// so it cannot be used to probe which addresses exist. The body (which carries a
// single-use link) is never written to the audit trail.
func (s *service) sendEmail(ctx context.Context, to, subject, body string) {
	if err := s.mailer.Send(ctx, to, subject, body); err != nil {
		s.log.Error("send email", "subject", subject, "err", err)
	}
}

func (s *service) appURL(path string) string {
	return strings.TrimRight(s.cfg.BaseURL, "/") + path
}

func (s *service) link(path, token string) string {
	return s.appURL(path) + "?token=" + url.QueryEscape(token)
}

func (s *service) sessionExpiry() time.Time {
	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return time.Now().Add(ttl)
}

func normalizeEmail(s string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(s))
	if e == "" || strings.ContainsAny(e, " \t") || strings.Count(e, "@") != 1 {
		return "", problem.InvalidInput("a valid email is required")
	}
	return e, nil
}

func workspaceNameForEmail(email string) string {
	name := email
	if i := strings.IndexByte(email, '@'); i > 0 {
		name = email[:i]
	}
	return name + "'s workspace"
}

func errInvalidCredentials() error {
	return problem.InvalidInput("invalid email or password")
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an
// internal error, so unexpected failures never leak as the wrong kind.
func mapErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pe *problem.Error
	if errors.As(err, &pe) {
		return err
	}
	return problem.Internalf(err, "%s", op)
}

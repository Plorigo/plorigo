// Package mailer delivers transactional email (verification, password reset).
//
// When no SMTP host is configured the log mailer is used: it writes the message —
// including the single-use link — to the control-plane log, so a self-hoster
// without SMTP can still complete the flow by copying the link. That is the only
// place a link is emitted; raw tokens never reach the audit trail. The SMTP mailer
// sends for real. See docs/architecture/auth.md.
package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

// Mailer sends a plain-text email. It matches the consumer Mailer ports declared
// by the modules that send mail (e.g. auth).
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Config selects and configures the mailer.
type Config struct {
	SMTPHost string
	SMTPPort string
	Username string
	Password string
	From     string
}

// New returns an SMTP mailer when a host is configured, otherwise a log mailer.
func New(cfg Config, log *slog.Logger) Mailer {
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		return &logMailer{log: log}
	}
	return &smtpMailer{cfg: cfg}
}

type logMailer struct{ log *slog.Logger }

func (m *logMailer) Send(_ context.Context, to, subject, body string) error {
	// Deliberate: with no SMTP configured, surfacing the message (and its link) in
	// the log is the delivery channel for dev / self-host. Configure SMTP_HOST in
	// production. Documented in docs/architecture/auth.md.
	m.log.Info("email (no SMTP configured — logging instead)", "to", to, "subject", subject, "body", body)
	return nil
}

type smtpMailer struct{ cfg Config }

func (m *smtpMailer) Send(_ context.Context, to, subject, body string) error {
	from := m.cfg.From
	if from == "" {
		from = m.cfg.Username
	}
	msg := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n\r\n" +
		body + "\r\n"
	var auth smtp.Auth
	if m.cfg.Username != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.SMTPHost)
	}
	if err := smtp.SendMail(m.cfg.SMTPHost+":"+m.cfg.SMTPPort, auth, from, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

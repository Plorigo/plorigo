// Package config loads typed configuration from the environment (12-factor).
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config is the process configuration shared by all three binaries.
type Config struct {
	MasterKey   string
	DatabaseURL string
	Port        string
	Dev         bool

	// BaseURL is the dashboard origin used to build links in emails. In dev the
	// dashboard runs on the Vite server; in the single binary it is the control
	// plane's own public URL.
	BaseURL string
	// AllowOpenRegistration lets anyone register; when false only the first
	// (bootstrap) user and invited users may.
	AllowOpenRegistration bool
	// RequireEmailVerification sends a verification email on registration.
	RequireEmailVerification bool

	// SMTP is optional; when SMTPHost is empty, emails are logged instead of sent.
	SMTPHost  string
	SMTPPort  string
	SMTPUser  string
	SMTPPass  string
	EmailFrom string
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	// Secure by default: the app is in dev mode ONLY when PLORIGO_ENV explicitly names a
	// dev environment. Unset / typo / "production" all mean production, so a deploy that
	// forgets the var still gets Secure cookies + the CSRF guard, never the reverse.
	env := strings.ToLower(strings.TrimSpace(os.Getenv("PLORIGO_ENV")))
	return Config{
		MasterKey:   os.Getenv("APP_MASTER_KEY"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        envOr("PORT", "8080"),
		Dev:         env == "dev" || env == "development" || env == "local",

		BaseURL:                  envOr("PLORIGO_BASE_URL", "http://localhost:5173"),
		AllowOpenRegistration:    envBool("PLORIGO_ALLOW_OPEN_REGISTRATION", true),
		RequireEmailVerification: envBool("PLORIGO_REQUIRE_EMAIL_VERIFICATION", false),

		SMTPHost:  os.Getenv("SMTP_HOST"),
		SMTPPort:  envOr("SMTP_PORT", "587"),
		SMTPUser:  os.Getenv("SMTP_USERNAME"),
		SMTPPass:  os.Getenv("SMTP_PASSWORD"),
		EmailFrom: envOr("EMAIL_FROM", "no-reply@plorigo.local"),
	}
}

// Validate checks the requirements for running the control plane.
func (c Config) Validate() error {
	var missing []string
	if c.MasterKey == "" {
		missing = append(missing, "APP_MASTER_KEY")
	}
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

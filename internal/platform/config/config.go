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
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	return Config{
		MasterKey:   os.Getenv("APP_MASTER_KEY"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        envOr("PORT", "8080"),
		Dev:         envOr("PLORIGO_ENV", "dev") != "production",
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

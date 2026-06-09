// Command seed creates or refreshes a LOCAL development login user so you can sign
// in without registering through the email flow each time. It is DEV-ONLY: it
// reuses the control plane's config and refuses to run unless PLORIGO_ENV marks a
// dev environment, so it can never create an account in production.
//
// Run it in the same environment as your dev stack (so DATABASE_URL/APP_MASTER_KEY
// match the database the app uses) — typically via `make seed`. Credentials come
// from PLORIGO_SEED_EMAIL / PLORIGO_SEED_PASSWORD, defaulting to a dev account.
// Re-running it resets the password and verifies the address, so it's idempotent.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/plorigo/plorigo/internal/app"
	"github.com/plorigo/plorigo/internal/platform/config"
)

func main() {
	email := envOr("PLORIGO_SEED_EMAIL", "dev@plorigo.local")
	password := envOr("PLORIGO_SEED_PASSWORD", "devpassword")

	ctx := context.Background()
	a, err := app.New(ctx, config.Load())
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed: startup failed:", err)
		os.Exit(1)
	}
	defer a.Close()

	user, err := a.SeedUser(ctx, email, password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Printf("seeded dev login — email: %s  password: %s\n", user.Email, password)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Package app is the central assembly of the control plane: it constructs the
// platform (config, DB, server), builds the modules, wires cross-module ports, and
// runs the HTTP server. cmd/controlplane is a thin shell over this.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/backups"
	"github.com/plorigo/plorigo/internal/config"
	"github.com/plorigo/plorigo/internal/deployments"
	"github.com/plorigo/plorigo/internal/domains"
	"github.com/plorigo/plorigo/internal/environments"
	platformconfig "github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/log"
	"github.com/plorigo/plorigo/internal/platform/server"
	"github.com/plorigo/plorigo/internal/projects"
	"github.com/plorigo/plorigo/internal/readiness"
	"github.com/plorigo/plorigo/internal/servers"
	"github.com/plorigo/plorigo/internal/serversetup"
	"github.com/plorigo/plorigo/internal/services"
	"github.com/plorigo/plorigo/internal/sources"
	"github.com/plorigo/plorigo/internal/webhooks"
)

// App is the assembled control plane.
type App struct {
	cfg platformconfig.Config
	log *slog.Logger
	db  *database.DB
	srv *server.Server

	// modules
	agents       *agents.Module
	auth         *auth.Module
	projects     *projects.Module
	environments *environments.Module
	config       *config.Module
	servers      *servers.Module
	serversetup  *serversetup.Module
	deployments  *deployments.Module
	domains      *domains.Module
	sources      *sources.Module
	services     *services.Module
	backups      *backups.Module
	readiness    *readiness.Module
	webhooks     *webhooks.Module
}

// New validates config, opens the DB pool, builds modules, and prepares the server.
func New(ctx context.Context, cfg platformconfig.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger := log.New(cfg.Dev)

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	a := &App{cfg: cfg, log: logger, db: db}
	if err := a.buildModules(); err != nil {
		db.Close()
		return nil, err
	}
	a.srv = server.New(":"+cfg.Port, a.router(), logger)
	return a, nil
}

// Run serves until ctx is cancelled, then shuts down and closes the DB pool.
func (a *App) Run(ctx context.Context) error {
	defer a.db.Close()
	return a.srv.Run(ctx)
}

// SeedUser creates or refreshes a local development login. It is DEV-ONLY: it
// refuses to run unless PLORIGO_ENV marks a dev environment (config.Dev), so it can
// never mint an account in production even if invoked there. Used by cmd/seed; the
// running server never calls it. The pool is closed by the caller (cmd/seed exits).
func (a *App) SeedUser(ctx context.Context, email, password string) (auth.User, error) {
	if !a.cfg.Dev {
		return auth.User{}, fmt.Errorf("seeding is only allowed in dev (set PLORIGO_ENV=dev); refusing in a non-dev environment")
	}
	return a.auth.SeedUser(ctx, email, password)
}

// Close releases the DB pool. cmd/seed calls this since it never calls Run.
func (a *App) Close() { a.db.Close() }

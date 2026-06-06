// Package app is the central assembly of the control plane: it constructs the
// platform (config, DB, server), builds the modules, wires cross-module ports, and
// runs the HTTP server. cmd/controlplane is a thin shell over this.
package app

import (
	"context"
	"log/slog"

	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/log"
	"github.com/plorigo/plorigo/internal/platform/server"
	"github.com/plorigo/plorigo/internal/projects"
)

// App is the assembled control plane.
type App struct {
	cfg config.Config
	log *slog.Logger
	db  *database.DB
	srv *server.Server

	// modules
	auth     *auth.Module
	projects *projects.Module
}

// New validates config, opens the DB pool, builds modules, and prepares the server.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger := log.New(cfg.Dev)

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	a := &App{cfg: cfg, log: logger, db: db}
	a.buildModules()
	a.srv = server.New(":"+cfg.Port, a.router(), logger)
	return a, nil
}

// Run serves until ctx is cancelled, then shuts down and closes the DB pool.
func (a *App) Run(ctx context.Context) error {
	defer a.db.Close()
	return a.srv.Run(ctx)
}

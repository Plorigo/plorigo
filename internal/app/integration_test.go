//go:build integration

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/problem"
	"github.com/plorigo/plorigo/internal/projects"
)

// TestSlice_CreateProjectWritesAuditInOneTx exercises the full vertical slice against a
// real Postgres (CI provides DATABASE_URL and APP_MASTER_KEY): app assembly -> projects
// service -> WithinTx -> sqlc -> project row AND audit row committed together. This is
// what catches sqlc/schema drift that compiles but fails at runtime.
func TestSlice_CreateProjectWritesAuditInOneTx(t *testing.T) {
	ctx := context.Background()

	a, err := New(ctx, config.Load())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer a.db.Close()

	wsID := id.New().String()
	if _, err := a.db.Pool.Exec(ctx,
		`INSERT INTO workspaces (id, name, slug) VALUES ($1, $2, $3)`,
		wsID, "CI Workspace", "ci-"+wsID[:8]); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	p, err := a.projects.Service().Create(ctx, projects.CreateInput{WorkspaceID: wsID, Name: "CI App"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	var auditCount int
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_events WHERE target_id = $1 AND action = 'project.create'`,
		p.ID).Scan(&auditCount); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit_events rows = %d, want 1 (audit must commit in the same tx as the project)", auditCount)
	}

	// Same name in the same workspace slugs identically -> unique violation must surface
	// as AlreadyExists, not a generic internal error.
	_, err = a.projects.Service().Create(ctx, projects.CreateInput{WorkspaceID: wsID, Name: "CI App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindAlreadyExists {
		t.Fatalf("duplicate create: got %v, want AlreadyExists", err)
	}
}

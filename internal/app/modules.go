package app

import (
	"github.com/plorigo/plorigo/internal/audit"
	"github.com/plorigo/plorigo/internal/projects"
)

// buildModules is the SINGLE place that constructs modules and injects cross-module
// interfaces. Adding a module later = one block here, plus mounting its Route() in
// router.go. This is also the only file that imports more than one module — which is
// exactly why cross-module wiring lives here and not inside the modules.
func (a *App) buildModules() {
	auditSvc := audit.New(audit.Deps{Log: a.log})

	a.projects = projects.New(projects.Deps{
		DB: a.db,
		// *audit.Service structurally satisfies projects' consumer-defined Recorder
		// port. projects does not import audit; the boundary is wired here.
		Audit: auditSvc,
		Log:   a.log,
	})
}

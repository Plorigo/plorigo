// Package readiness is the Production Readiness Doctor: a read-only, decision-only module
// (it owns no tables and has no postgres.go) that deterministically aggregates current
// platform state — deployment status, configuration, domains/SSL, server health, and backups —
// into a per-service or per-environment checklist with a Critical/Warning/Info severity split
// and a Ready / Almost-ready / Not-ready verdict.
//
// It reads everything through consumer-defined ports (see store.go) wired in internal/app, so it
// imports no sibling module. Per docs/architecture/principles.md the output leads with a plain
// verdict and explains, for every non-passing check, exactly what to fix next. v1 is intentionally
// non-scanning: it reads configuration and deployment/server/domain STATE only, never source code
// or logs (which would be heuristic and non-deterministic). See docs/architecture/readiness.md.
package readiness

import "context"

// Severity ranks how much a failing check should block a production launch.
type Severity string

// Severity levels, most to least blocking: critical should block production by default, warning
// allows launch with acknowledgement, info is an improvement suggestion or context.
const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// State is the outcome of a single check.
type State string

// State outcomes for a single check.
const (
	StatePass    State = "pass"
	StateWarn    State = "warn"
	StateFail    State = "fail"
	StateUnknown State = "unknown"
)

// Level is the overall verdict for a service or environment.
type Level string

// Level verdicts, best to worst.
const (
	LevelReady       Level = "ready"
	LevelAlmostReady Level = "almost_ready"
	LevelNotReady    Level = "not_ready"
)

// Check categories.
const (
	CategoryDeployment = "deployment"
	CategoryConfig     = "config"
	CategoryDomain     = "domain"
	CategoryServer     = "server"
	CategoryBackup     = "backup"
	CategoryService    = "service" // environment-level: one row per service
)

// Check is one deterministic readiness check over current state.
type Check struct {
	Category    string
	Severity    Severity
	State       State
	Title       string
	Detail      string
	Remediation string // empty when State == StatePass
}

// Checklist aggregates checks into one verdict.
type Checklist struct {
	OverallLevel Level
	Checks       []Check
}

// Service is the readiness module's surface.
type Service interface {
	ServiceReadiness(ctx context.Context, serviceID string) (Checklist, error)
	EnvironmentReadiness(ctx context.Context, environmentID string) (Checklist, error)
}

// deriveLevel folds checks into the overall verdict: any critical failure makes the whole thing
// not-ready; otherwise any warning makes it almost-ready; otherwise ready.
func deriveLevel(checks []Check) Level {
	hasCriticalFail := false
	hasWarn := false
	for _, c := range checks {
		if c.Severity == SeverityCritical && c.State == StateFail {
			hasCriticalFail = true
		}
		if c.State == StateWarn {
			hasWarn = true
		}
	}
	switch {
	case hasCriticalFail:
		return LevelNotReady
	case hasWarn:
		return LevelAlmostReady
	default:
		return LevelReady
	}
}

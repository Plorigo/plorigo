package readiness

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// service computes readiness checklists from sibling-module state. It performs no writes, so it
// holds no transaction runner — only the read ports and the authorizer.
type service struct {
	services    ServiceReader
	config      ConfigReader
	domains     DomainReader
	deployments DeploymentReader
	servers     ServerReader
	backups     BackupReader // optional; nil until the backups module is wired
	authorizer  authz.Authorizer
	log         *slog.Logger
}

func newService(
	services ServiceReader,
	cfg ConfigReader,
	domains DomainReader,
	deployments DeploymentReader,
	servers ServerReader,
	backups BackupReader,
	authorizer authz.Authorizer,
	log *slog.Logger,
) *service {
	return &service{
		services:    services,
		config:      cfg,
		domains:     domains,
		deployments: deployments,
		servers:     servers,
		backups:     backups,
		authorizer:  authorizer,
		log:         log,
	}
}

// ServiceReadiness runs every check for one service and folds them into a verdict.
func (s *service) ServiceReadiness(ctx context.Context, serviceID string) (Checklist, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return Checklist{}, problem.InvalidInput("a valid service_id is required")
	}
	// The sibling read authorizes ActionServiceRead and 404s an unknown id.
	facts, err := s.services.Get(ctx, serviceID)
	if err != nil {
		return Checklist{}, err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionReadinessRead,
		authz.Resource{Type: "readiness", WorkspaceID: facts.WorkspaceID, ID: facts.ID}); err != nil {
		return Checklist{}, err
	}
	return s.checklistForService(ctx, facts)
}

// EnvironmentReadiness summarizes every service in an environment, one row per service, and
// reports the worst verdict overall.
func (s *service) EnvironmentReadiness(ctx context.Context, environmentID string) (Checklist, error) {
	if _, err := id.Parse(environmentID); err != nil {
		return Checklist{}, problem.InvalidInput("a valid environment_id is required")
	}
	// The sibling read authorizes ActionServiceRead through the environment's workspace.
	svcs, err := s.services.ListByEnvironment(ctx, environmentID)
	if err != nil {
		return Checklist{}, err
	}
	if len(svcs) == 0 {
		return Checklist{
			OverallLevel: LevelReady,
			Checks: []Check{{
				Category: CategoryService,
				Severity: SeverityInfo,
				State:    StatePass,
				Title:    "No services yet",
				Detail:   "This environment has no services to check.",
			}},
		}, nil
	}
	checks := make([]Check, 0, len(svcs))
	for _, svc := range svcs {
		// Per-service readiness re-authorizes ActionReadinessRead on the same workspace.
		list, err := s.ServiceReadiness(ctx, svc.ID)
		if err != nil {
			return Checklist{}, err
		}
		checks = append(checks, summarizeService(svc.Name, list))
	}
	return Checklist{OverallLevel: worstLevel(checks), Checks: checks}, nil
}

// checklistForService gathers each category's check for an already-authorized service.
func (s *service) checklistForService(ctx context.Context, facts ServiceFacts) (Checklist, error) {
	deployFact, deployed, err := s.deployments.LatestForService(ctx, facts.ID)
	if err != nil {
		return Checklist{}, err
	}
	entries, err := s.config.ListForService(ctx, facts.ID)
	if err != nil {
		return Checklist{}, err
	}
	domainFacts, err := s.domains.ListByService(ctx, facts.ID)
	if err != nil {
		return Checklist{}, err
	}
	serverReadiness, err := s.servers.WorkspaceReadiness(ctx, facts.WorkspaceID)
	if err != nil {
		return Checklist{}, err
	}

	checks := []Check{
		deploymentCheck(deployFact, deployed),
		configCheck(entries),
		domainCheck(facts, domainFacts),
		serverCheck(serverReadiness),
	}
	if facts.SourceKind == "template" {
		checks = append(checks, s.backupCheck(ctx, facts.ID))
	}
	return Checklist{OverallLevel: deriveLevel(checks), Checks: checks}, nil
}

// deploymentCheck reflects whether the service is actually running. A failed latest deploy is a
// critical blocker; never-deployed is a warning; in-flight is informational.
func deploymentCheck(d DeploymentFact, deployed bool) Check {
	c := Check{Category: CategoryDeployment, Title: "Latest deployment"}
	switch {
	case !deployed:
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = "This service hasn't been deployed yet."
		c.Remediation = "Deploy it to a connected server and watch the health check pass."
	case d.Status == "running":
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "The latest deployment is running and passed its health check."
	case d.Status == "failed":
		c.Severity, c.State = SeverityCritical, StateFail
		c.Detail = "The latest deployment failed."
		c.Remediation = "Open the deployment logs to see why, then redeploy. Any previous running release keeps serving."
	case d.Status == "superseded":
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "The latest deployment was replaced by a newer running release."
	default:
		c.Severity, c.State = SeverityInfo, StateUnknown
		c.Detail = "A deployment is in progress."
		c.Remediation = "Wait for it to finish, then re-check readiness."
	}
	return c
}

// placeholderHints are lowercase substrings that mark a variable value as an obvious placeholder
// the user forgot to fill in. Deterministic and conservative — no source scanning.
var placeholderHints = []string{"changeme", "change-me", "your-", "your_", "example.com", "todo", "replace-me", "xxxxx", "<your", "placeholder"}

// configCheck flags variables whose values still look like placeholders. It only inspects
// variables (secrets never expose a value), so it can never false-positive on a real secret.
func configCheck(entries []ConfigEntry) Check {
	c := Check{Category: CategoryConfig, Title: "Environment variables"}
	variables := 0
	var offenders []string
	for _, e := range entries {
		if e.Secret {
			continue
		}
		variables++
		if looksLikePlaceholder(e.Value) {
			offenders = append(offenders, e.Key)
		}
	}
	sort.Strings(offenders)
	switch {
	case len(offenders) > 0:
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = fmt.Sprintf("%d variable(s) still look like placeholders: %s.", len(offenders), strings.Join(offenders, ", "))
		c.Remediation = "Set real values before launching to production."
	case variables == 0:
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "No environment variables are configured for this service."
	default:
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = fmt.Sprintf("All %d configured variable(s) have real values.", variables)
	}
	return c
}

func looksLikePlaceholder(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return true
	}
	for _, hint := range placeholderHints {
		if strings.Contains(v, hint) {
			return true
		}
	}
	return false
}

// domainCheck reflects whether the service is reachable over HTTPS where it should be.
func domainCheck(facts ServiceFacts, domains []DomainFact) Check {
	c := Check{Category: CategoryDomain, Title: "Domain & SSL"}
	if facts.Visibility != "public" {
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "Private service — reachable only by sibling services, so no public domain is needed."
		return c
	}
	// Public service: weigh any custom domains first, then fall back to the generated route.
	var pending, failed []string
	active := false
	for _, d := range domains {
		switch d.Status {
		case "active", "verified":
			active = true
		case "pending_dns", "blocked":
			pending = append(pending, d.Hostname)
		case "failed":
			failed = append(failed, d.Hostname)
		}
	}
	switch {
	case len(failed) > 0:
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = fmt.Sprintf("A custom domain failed verification: %s.", strings.Join(failed, ", "))
		c.Remediation = "Re-check the DNS record on the service page, then verify again."
	case len(pending) > 0 && !active:
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = fmt.Sprintf("A custom domain isn't verified yet: %s.", strings.Join(pending, ", "))
		c.Remediation = "Add the DNS record shown on the service page, then verify it."
	case active:
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "A custom domain is active over HTTPS."
	case facts.RouteURL != "":
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "Served over HTTPS on its generated domain."
	default:
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = "This public service has no live URL yet."
		c.Remediation = "Deploy the service to get its generated HTTPS domain."
	}
	return c
}

// serverCheck reflects whether there's somewhere ready to deploy onto — the recovery/infra
// prerequisite. Mirrors the server readiness the control plane already derives.
func serverCheck(r ServerReadiness) Check {
	c := Check{Category: CategoryServer, Title: "Connected server"}
	switch {
	case !r.HasServer:
		c.Severity, c.State = SeverityCritical, StateFail
		c.Detail = "No server is connected to this workspace."
		c.Remediation = "Connect a server from the Servers page before deploying."
	case r.State == "blocked":
		c.Severity, c.State = SeverityCritical, StateFail
		c.Detail = reasoned("The connected server isn't ready to deploy.", r.Reason)
		c.Remediation = "Resolve the server issue on the Servers page."
	case r.State == "degraded":
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = reasoned("The connected server has a warning.", r.Reason)
		c.Remediation = "Review the server's health on the Servers page."
	case r.State == "ready":
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "A connected server is ready to deploy onto."
	default:
		c.Severity, c.State = SeverityWarning, StateWarn
		c.Detail = "No server is currently online."
		c.Remediation = "Bring a connected server online before deploying."
	}
	return c
}

// backupCheck reflects backup confidence for a managed database. Degrades gracefully when the
// backups module isn't wired yet (BackupReader is nil).
func (s *service) backupCheck(ctx context.Context, serviceID string) Check {
	c := Check{Category: CategoryBackup, Title: "Database backup"}
	if s.backups == nil {
		c.Severity, c.State = SeverityInfo, StateUnknown
		c.Detail = "Database backups aren't available yet."
		c.Remediation = "Backups for managed databases are coming in a later release."
		return c
	}
	has, err := s.backups.HasBackup(ctx, serviceID)
	if err != nil {
		c.Severity, c.State = SeverityInfo, StateUnknown
		c.Detail = "Couldn't determine backup status."
		return c
	}
	if has {
		c.Severity, c.State = SeverityInfo, StatePass
		c.Detail = "A backup exists for this database."
		return c
	}
	c.Severity, c.State = SeverityWarning, StateWarn
	c.Detail = "No backup has been taken for this database."
	c.Remediation = "Create a backup before trusting it with production data."
	return c
}

// summarizeService collapses a service's checklist into a single environment-level row.
func summarizeService(name string, list Checklist) Check {
	c := Check{Category: CategoryService, Title: name}
	pass, warn, fail := 0, 0, 0
	for _, ch := range list.Checks {
		switch ch.State {
		case StateFail:
			fail++
		case StateWarn:
			warn++
		case StatePass:
			pass++
		}
	}
	switch list.OverallLevel {
	case LevelNotReady:
		c.Severity, c.State = SeverityCritical, StateFail
	case LevelAlmostReady:
		c.Severity, c.State = SeverityWarning, StateWarn
	default:
		c.Severity, c.State = SeverityInfo, StatePass
	}
	c.Detail = fmt.Sprintf("%d passing, %d warning, %d critical.", pass, warn, fail)
	if list.OverallLevel != LevelReady {
		c.Remediation = "Open the service to see what to fix."
	}
	return c
}

// worstLevel returns the most severe verdict across environment-level service rows.
func worstLevel(checks []Check) Level {
	hasFail, hasWarn := false, false
	for _, c := range checks {
		switch c.State {
		case StateFail:
			hasFail = true
		case StateWarn:
			hasWarn = true
		}
	}
	switch {
	case hasFail:
		return LevelNotReady
	case hasWarn:
		return LevelAlmostReady
	default:
		return LevelReady
	}
}

func reasoned(base, reason string) string {
	if strings.TrimSpace(reason) == "" {
		return base
	}
	return base + " " + reason
}

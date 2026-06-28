package readiness

import (
	"context"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testServiceID = "11111111-1111-1111-1111-111111111111"
	testEnvID     = "22222222-2222-2222-2222-222222222222"
)

type fakeServices struct {
	facts ServiceFacts
	list  []ServiceFacts
	err   error
}

func (f fakeServices) Get(context.Context, string) (ServiceFacts, error) { return f.facts, f.err }
func (f fakeServices) ListByEnvironment(context.Context, string) ([]ServiceFacts, error) {
	return f.list, f.err
}

type fakeConfig struct{ entries []ConfigEntry }

func (f fakeConfig) ListForService(context.Context, string) ([]ConfigEntry, error) {
	return f.entries, nil
}

type fakeDomains struct{ list []DomainFact }

func (f fakeDomains) ListByService(context.Context, string) ([]DomainFact, error) { return f.list, nil }

type fakeDeploys struct {
	fact DeploymentFact
	ok   bool
}

func (f fakeDeploys) LatestForService(context.Context, string) (DeploymentFact, bool, error) {
	return f.fact, f.ok, nil
}

type fakeServers struct{ r ServerReadiness }

func (f fakeServers) WorkspaceReadiness(context.Context, string) (ServerReadiness, error) {
	return f.r, nil
}

type fakeBackups struct{ has bool }

func (f fakeBackups) HasBackup(context.Context, string) (bool, error) { return f.has, nil }

type allowAll struct{}

func (allowAll) Authorize(context.Context, principal.Principal, authz.Action, authz.Resource) error {
	return nil
}

type denyAll struct{}

func (denyAll) Authorize(context.Context, principal.Principal, authz.Action, authz.Resource) error {
	return problem.PermissionDenied("denied")
}

// readyService is a service that should pass every check: a public git service running on a
// healthy server with a generated HTTPS route and a real config value.
func readyService() (fakeServices, fakeConfig, fakeDomains, fakeDeploys, fakeServers) {
	return fakeServices{facts: ServiceFacts{ID: testServiceID, Name: "web", WorkspaceID: "ws", SourceKind: "git", Visibility: "public", RouteURL: "https://web.localhost"}},
		fakeConfig{entries: []ConfigEntry{{Key: "API_URL", Value: "https://api.example.org"}}},
		fakeDomains{},
		fakeDeploys{fact: DeploymentFact{Status: "running"}, ok: true},
		fakeServers{r: ServerReadiness{HasServer: true, State: "ready"}}
}

func newTestService(s fakeServices, c fakeConfig, d fakeDomains, dep fakeDeploys, srv fakeServers, b BackupReader, az authz.Authorizer) *service {
	return newService(s, c, d, dep, srv, b, az, nil)
}

func findCheck(list Checklist, category string) (Check, bool) {
	for _, c := range list.Checks {
		if c.Category == category {
			return c, true
		}
	}
	return Check{}, false
}

func TestServiceReadiness_AllGreen(t *testing.T) {
	s, c, d, dep, srv := readyService()
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, err := svc.ServiceReadiness(context.Background(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OverallLevel != LevelReady {
		t.Fatalf("overall = %q, want ready", got.OverallLevel)
	}
	// A non-template service emits no backup check.
	if _, ok := findCheck(got, CategoryBackup); ok {
		t.Fatalf("did not expect a backup check for a non-database service")
	}
	for _, cat := range []string{CategoryDeployment, CategoryConfig, CategoryDomain, CategoryServer} {
		ch, ok := findCheck(got, cat)
		if !ok {
			t.Fatalf("missing %q check", cat)
		}
		if ch.State != StatePass {
			t.Errorf("%q state = %q, want pass", cat, ch.State)
		}
	}
}

func TestServiceReadiness_NotReady(t *testing.T) {
	s, c, d, _, _ := readyService()
	dep := fakeDeploys{fact: DeploymentFact{Status: "failed"}, ok: true}
	srv := fakeServers{r: ServerReadiness{HasServer: false}}
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, err := svc.ServiceReadiness(context.Background(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OverallLevel != LevelNotReady {
		t.Fatalf("overall = %q, want not_ready", got.OverallLevel)
	}
	if ch, _ := findCheck(got, CategoryDeployment); ch.Severity != SeverityCritical || ch.State != StateFail {
		t.Errorf("deployment check = %+v, want critical/fail", ch)
	}
	if ch, _ := findCheck(got, CategoryServer); ch.Severity != SeverityCritical || ch.State != StateFail {
		t.Errorf("server check = %+v, want critical/fail", ch)
	}
}

func TestServiceReadiness_AlmostReady(t *testing.T) {
	s, _, d, dep, _ := readyService()
	c := fakeConfig{entries: []ConfigEntry{{Key: "SECRET_KEY", Value: "changeme"}}}
	srv := fakeServers{r: ServerReadiness{HasServer: true, State: "degraded", Reason: "Low disk."}}
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, err := svc.ServiceReadiness(context.Background(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OverallLevel != LevelAlmostReady {
		t.Fatalf("overall = %q, want almost_ready", got.OverallLevel)
	}
	if ch, _ := findCheck(got, CategoryConfig); ch.State != StateWarn {
		t.Errorf("config check state = %q, want warn", ch.State)
	}
}

func TestServiceReadiness_TemplateBackupDegradesGracefully(t *testing.T) {
	s, c, d, dep, srv := readyService()
	s.facts.SourceKind = "template"
	s.facts.Visibility = "private"
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{}) // nil backups
	got, err := svc.ServiceReadiness(context.Background(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ch, ok := findCheck(got, CategoryBackup)
	if !ok {
		t.Fatalf("expected a backup check for a template (database) service")
	}
	if ch.State != StateUnknown || ch.Severity != SeverityInfo {
		t.Errorf("backup check = %+v, want info/unknown when backups unwired", ch)
	}
	// An unknown backup must not drag the overall level below ready.
	if got.OverallLevel != LevelReady {
		t.Errorf("overall = %q, want ready (unknown backup is non-blocking)", got.OverallLevel)
	}
}

func TestServiceReadiness_TemplateWithBackup(t *testing.T) {
	s, c, d, dep, srv := readyService()
	s.facts.SourceKind = "template"
	svc := newTestService(s, c, d, dep, srv, fakeBackups{has: true}, allowAll{})
	got, _ := svc.ServiceReadiness(context.Background(), testServiceID)
	if ch, _ := findCheck(got, CategoryBackup); ch.State != StatePass {
		t.Errorf("backup check state = %q, want pass", ch.State)
	}
}

func TestServiceReadiness_NeverDeployed(t *testing.T) {
	s, c, d, _, srv := readyService()
	dep := fakeDeploys{ok: false}
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, _ := svc.ServiceReadiness(context.Background(), testServiceID)
	if ch, _ := findCheck(got, CategoryDeployment); ch.State != StateWarn {
		t.Errorf("deployment check state = %q, want warn when never deployed", ch.State)
	}
	if got.OverallLevel != LevelAlmostReady {
		t.Errorf("overall = %q, want almost_ready", got.OverallLevel)
	}
}

func TestServiceReadiness_PrivateServiceDomainPasses(t *testing.T) {
	s, c, d, dep, srv := readyService()
	s.facts.Visibility = "private"
	s.facts.RouteURL = ""
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, _ := svc.ServiceReadiness(context.Background(), testServiceID)
	if ch, _ := findCheck(got, CategoryDomain); ch.State != StatePass {
		t.Errorf("domain check for private service = %q, want pass", ch.State)
	}
}

func TestServiceReadiness_PendingCustomDomainWarns(t *testing.T) {
	s, c, _, dep, srv := readyService()
	d := fakeDomains{list: []DomainFact{{Hostname: "app.example.com", Status: "pending_dns"}}}
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, _ := svc.ServiceReadiness(context.Background(), testServiceID)
	if ch, _ := findCheck(got, CategoryDomain); ch.State != StateWarn {
		t.Errorf("domain check = %q, want warn for pending custom domain", ch.State)
	}
}

func TestServiceReadiness_InvalidID(t *testing.T) {
	s, c, d, dep, srv := readyService()
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	if _, err := svc.ServiceReadiness(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("expected an error for an invalid service id")
	}
}

func TestServiceReadiness_AuthorizationDenied(t *testing.T) {
	s, c, d, dep, srv := readyService()
	svc := newTestService(s, c, d, dep, srv, nil, denyAll{})
	if _, err := svc.ServiceReadiness(context.Background(), testServiceID); err == nil {
		t.Fatal("expected a permission error when authorization is denied")
	}
}

func TestEnvironmentReadiness_SummarizesServices(t *testing.T) {
	s, c, d, dep, srv := readyService()
	s.list = []ServiceFacts{
		{ID: testServiceID, Name: "web", WorkspaceID: "ws", SourceKind: "git", Visibility: "public", RouteURL: "https://web.localhost"},
		{ID: testServiceID, Name: "api", WorkspaceID: "ws", SourceKind: "git", Visibility: "public", RouteURL: "https://api.localhost"},
	}
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, err := svc.EnvironmentReadiness(context.Background(), testEnvID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Checks) != 2 {
		t.Fatalf("got %d service rows, want 2", len(got.Checks))
	}
	for _, ch := range got.Checks {
		if ch.Category != CategoryService {
			t.Errorf("env check category = %q, want service", ch.Category)
		}
	}
	if got.OverallLevel != LevelReady {
		t.Errorf("overall = %q, want ready", got.OverallLevel)
	}
}

func TestEnvironmentReadiness_Empty(t *testing.T) {
	s, c, d, dep, srv := readyService()
	s.list = nil
	svc := newTestService(s, c, d, dep, srv, nil, allowAll{})
	got, err := svc.EnvironmentReadiness(context.Background(), testEnvID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OverallLevel != LevelReady || len(got.Checks) != 1 {
		t.Fatalf("empty env = %+v, want ready with one info row", got)
	}
}

func TestLooksLikePlaceholder(t *testing.T) {
	for _, v := range []string{"", "  ", "changeme", "your-api-key", "TODO", "https://example.com/x", "<your-token>"} {
		if !looksLikePlaceholder(v) {
			t.Errorf("looksLikePlaceholder(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"production", "https://api.acme.io", "sk_live_realish", "3000"} {
		if looksLikePlaceholder(v) {
			t.Errorf("looksLikePlaceholder(%q) = true, want false", v)
		}
	}
}

func TestDeriveLevel(t *testing.T) {
	cases := []struct {
		name   string
		checks []Check
		want   Level
	}{
		{"empty", nil, LevelReady},
		{"all pass", []Check{{Severity: SeverityInfo, State: StatePass}}, LevelReady},
		{"warn", []Check{{Severity: SeverityWarning, State: StateWarn}}, LevelAlmostReady},
		{"critical fail beats warn", []Check{{Severity: SeverityWarning, State: StateWarn}, {Severity: SeverityCritical, State: StateFail}}, LevelNotReady},
		{"unknown is non-blocking", []Check{{Severity: SeverityInfo, State: StateUnknown}}, LevelReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveLevel(tc.checks); got != tc.want {
				t.Errorf("deriveLevel = %q, want %q", got, tc.want)
			}
		})
	}
}

package domains

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testDomainID  = "00000000-0000-0000-0000-0000000000dd"
	testServiceID = "00000000-0000-0000-0000-0000000000aa"
	testEnvID     = "11111111-1111-1111-1111-111111111111"
	testProjectID = "33333333-3333-3333-3333-333333333333"
	testWorkspace = "22222222-2222-2222-2222-222222222222"
)

type fakeStore struct {
	service ServiceRoute
	svcOK   bool
	domain  Domain
	rows    []Domain
	created []Domain
}

func (f *fakeStore) CreateDomain(_ context.Context, _ database.Tx, d Domain) (Domain, error) {
	d.ID = testDomainID
	f.created = append(f.created, d)
	return d, nil
}
func (f *fakeStore) GetDomain(_ context.Context, _ string) (Domain, bool, error) {
	return f.domain, f.domain.ID != "", nil
}
func (f *fakeStore) ListByService(_ context.Context, _ string) ([]Domain, error) { return f.rows, nil }
func (f *fakeStore) ListByProject(_ context.Context, _ string) ([]Domain, error) { return f.rows, nil }
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Domain, error) {
	return f.rows, nil
}
func (f *fakeStore) UpdateVerification(_ context.Context, _ database.Tx, id, status, message string) (Domain, error) {
	f.domain.ID = id
	f.domain.Status = status
	f.domain.StatusMessage = message
	return f.domain, nil
}
func (f *fakeStore) DeleteDomain(_ context.Context, _ database.Tx, id string) (string, bool, error) {
	return id, true, nil
}
func (f *fakeStore) ServiceRoute(_ context.Context, _ string) (ServiceRoute, bool, error) {
	return f.service, f.svcOK, nil
}
func (f *fakeStore) WorkspaceForProject(_ context.Context, _ string) (string, bool, error) {
	return f.service.WorkspaceID, f.svcOK, nil
}

type fakeResolver struct {
	cname map[string]string
	hosts map[string][]string
}

func (f fakeResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if v := f.cname[host]; v != "" {
		return v, nil
	}
	return "", errors.New("not found")
}
func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if v := f.hosts[host]; len(v) > 0 {
		return v, nil
	}
	return nil, errors.New("not found")
}

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(database.Tx) error) error { return fn(nil) }

type fakeRecorder struct{ action string }

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.action = action
	return nil
}

type fakeAuthz struct{}

func (fakeAuthz) Authorize(context.Context, principal.Principal, authz.Action, authz.Resource) error {
	return nil
}

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "u1", Method: principal.MethodSession})
}

func testSvc(store *fakeStore, resolver Resolver) *service {
	return newService(fakeTx{}, store, resolver, fakeAuthz{}, &fakeRecorder{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNormalizeHostnameRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{"https://app.example.com", "app.example.com/path", "app.example.com:443", "*.example.com", "localhost", "-bad.example.com"} {
		if _, err := normalizeHostname(raw); !isInvalid(err) {
			t.Fatalf("normalizeHostname(%q) = %v, want invalid input", raw, err)
		}
	}
	got, err := normalizeHostname(" App.Example.COM. ")
	if err != nil || got != "app.example.com" {
		t.Fatalf("normalizeHostname = %q, %v; want app.example.com", got, err)
	}
}

func TestCreateDomain_AllowsMultiplePerServiceAndShowsDNS(t *testing.T) {
	store := &fakeStore{svcOK: true, service: publicService()}
	svc := testSvc(store, fakeResolver{})
	for _, host := range []string{"app.example.com", "api.example.com"} {
		d, err := svc.CreateDomain(authedCtx(), CreateInput{ServiceID: testServiceID, Hostname: host})
		if err != nil {
			t.Fatalf("CreateDomain(%s): %v", host, err)
		}
		if d.Status != StatusPendingDNS || d.DNSRecordType != RecordCNAME || d.DNSRecordValue != "svc.localhost" {
			t.Fatalf("domain = %+v, want pending CNAME to generated host", d)
		}
	}
	if len(store.created) != 2 {
		t.Fatalf("created %d domains, want 2", len(store.created))
	}
}

func TestCreateDomain_BlockedUntilGeneratedRouteExists(t *testing.T) {
	store := &fakeStore{svcOK: true, service: publicService()}
	store.service.RouteURL = ""
	d, err := testSvc(store, fakeResolver{}).CreateDomain(authedCtx(), CreateInput{ServiceID: testServiceID, Hostname: "app.example.com"})
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if d.Status != StatusBlocked {
		t.Fatalf("status = %q, want blocked", d.Status)
	}
}

func TestListDomainsByProject_EnrichesDNSRecords(t *testing.T) {
	store := &fakeStore{
		svcOK:   true,
		service: publicService(),
		rows: []Domain{{
			ID:          testDomainID,
			ServiceID:   testServiceID,
			ProjectID:   testProjectID,
			WorkspaceID: testWorkspace,
			Hostname:    "app.example.com",
		}},
	}
	rows, err := testSvc(store, fakeResolver{}).ListByProject(authedCtx(), testProjectID)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(rows) != 1 || rows[0].DNSRecordType != RecordCNAME || rows[0].DNSRecordValue != "svc.localhost" {
		t.Fatalf("rows = %+v, want enriched CNAME record", rows)
	}
}

func TestListDomainsByWorkspace_EnrichesDNSRecords(t *testing.T) {
	store := &fakeStore{
		svcOK:   true,
		service: publicService(),
		rows: []Domain{{
			ID:          testDomainID,
			ServiceID:   testServiceID,
			ProjectID:   testProjectID,
			WorkspaceID: testWorkspace,
			Hostname:    "api.example.com",
		}},
	}
	rows, err := testSvc(store, fakeResolver{}).ListByWorkspace(authedCtx(), testWorkspace)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(rows) != 1 || rows[0].DNSRecordType != RecordCNAME || rows[0].DNSRecordValue != "svc.localhost" {
		t.Fatalf("rows = %+v, want enriched CNAME record", rows)
	}
}

func TestVerifyDomain_CNAMESuccess(t *testing.T) {
	store := &fakeStore{
		svcOK:   true,
		service: publicService(),
		domain:  Domain{ID: testDomainID, ServiceID: testServiceID, WorkspaceID: testWorkspace, Hostname: "app.example.com"},
	}
	resolver := fakeResolver{cname: map[string]string{"app.example.com": "svc.localhost."}}
	d, err := testSvc(store, resolver).VerifyDomain(authedCtx(), testDomainID)
	if err != nil {
		t.Fatalf("VerifyDomain: %v", err)
	}
	if d.Status != StatusVerified {
		t.Fatalf("status = %q, want verified", d.Status)
	}
}

func publicService() ServiceRoute {
	return ServiceRoute{
		ID:            testServiceID,
		EnvironmentID: testEnvID,
		ProjectID:     testProjectID,
		WorkspaceID:   testWorkspace,
		Visibility:    "public",
		RouteURL:      "http://svc.localhost:8083",
	}
}

func isInvalid(err error) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == problem.KindInvalidInput
}

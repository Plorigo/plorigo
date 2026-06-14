package domains

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const maxHostnameLen = 253

type service struct {
	tx         TxRunner
	store      Store
	resolver   Resolver
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, resolver Resolver, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, resolver: resolver, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

func (s *service) CreateDomain(ctx context.Context, in CreateInput) (Domain, error) {
	if _, err := id.Parse(in.ServiceID); err != nil {
		return Domain{}, problem.InvalidInput("a valid service_id is required")
	}
	hostname, err := normalizeHostname(in.Hostname)
	if err != nil {
		return Domain{}, err
	}
	svc, ok, err := s.store.ServiceRoute(ctx, in.ServiceID)
	if err != nil {
		return Domain{}, problem.Internalf(err, "create domain")
	}
	if !ok {
		return Domain{}, problem.NotFound("service %s not found", in.ServiceID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDomainCreate, authz.Resource{Type: "domain", WorkspaceID: svc.WorkspaceID}); err != nil {
		return Domain{}, err
	}

	status, message := initialStatus(svc)
	candidate := Domain{
		ServiceID:     svc.ID,
		EnvironmentID: svc.EnvironmentID,
		ProjectID:     svc.ProjectID,
		WorkspaceID:   svc.WorkspaceID,
		Hostname:      hostname,
		Status:        status,
		StatusMessage: message,
	}
	var saved Domain
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		saved, txErr = s.store.CreateDomain(ctx, tx, candidate)
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "domain.create", "domain", saved.ID, svc.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return Domain{}, mapErr(err, "create domain")
	}
	s.log.Info("domain created", "id", saved.ID, "service_id", saved.ServiceID, "hostname", saved.Hostname, "status", saved.Status, "workspace_id", saved.WorkspaceID, "actor", caller.UserID)
	return s.enrich(ctx, saved), nil
}

func (s *service) ListByService(ctx context.Context, serviceID string) ([]Domain, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return nil, problem.InvalidInput("a valid service_id is required")
	}
	svc, ok, err := s.store.ServiceRoute(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list domains")
	}
	if !ok {
		return nil, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDomainRead, authz.Resource{Type: "domain", WorkspaceID: svc.WorkspaceID}); err != nil {
		return nil, err
	}
	rows, err := s.store.ListByService(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list domains")
	}
	for i := range rows {
		rows[i] = enrichWithService(rows[i], svc, s.resolver)
	}
	return rows, nil
}

func (s *service) ListByProject(ctx context.Context, projectID string) ([]Domain, error) {
	if _, err := id.Parse(projectID); err != nil {
		return nil, problem.InvalidInput("a valid project_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceForProject(ctx, projectID)
	if err != nil {
		return nil, problem.Internalf(err, "list domains")
	}
	if !ok {
		return nil, problem.NotFound("project %s not found", projectID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDomainRead, authz.Resource{Type: "domain", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	rows, err := s.store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, problem.Internalf(err, "list domains")
	}
	return s.enrichRows(ctx, rows), nil
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Domain, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return nil, problem.InvalidInput("a valid workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDomainRead, authz.Resource{Type: "domain", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	rows, err := s.store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, problem.Internalf(err, "list domains")
	}
	return s.enrichRows(ctx, rows), nil
}

func (s *service) VerifyDomain(ctx context.Context, domainID string) (Domain, error) {
	if _, err := id.Parse(domainID); err != nil {
		return Domain{}, problem.InvalidInput("a valid domain id is required")
	}
	d, svc, err := s.domainAndService(ctx, domainID)
	if err != nil {
		return Domain{}, err
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDomainVerify, authz.Resource{Type: "domain", WorkspaceID: d.WorkspaceID, ID: d.ID}); err != nil {
		return Domain{}, err
	}

	status, message := s.verify(ctx, d, svc)
	var saved Domain
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		saved, txErr = s.store.UpdateVerification(ctx, tx, d.ID, status, message)
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "domain.verify", "domain", d.ID, d.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return Domain{}, mapErr(err, "verify domain")
	}
	return enrichWithService(saved, svc, s.resolver), nil
}

func (s *service) DeleteDomain(ctx context.Context, domainID string) error {
	if _, err := id.Parse(domainID); err != nil {
		return problem.InvalidInput("a valid domain id is required")
	}
	d, _, err := s.domainAndService(ctx, domainID)
	if err != nil {
		return err
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDomainDelete, authz.Resource{Type: "domain", WorkspaceID: d.WorkspaceID, ID: d.ID}); err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteDomain(ctx, tx, d.ID)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			return problem.NotFound("domain %s not found", d.ID)
		}
		return s.audit.Record(ctx, tx, "domain.delete", "domain", deletedID, d.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "delete domain")
	}
	s.log.Info("domain deleted", "id", d.ID, "hostname", d.Hostname, "workspace_id", d.WorkspaceID, "actor", caller.UserID)
	return nil
}

func (s *service) domainAndService(ctx context.Context, domainID string) (Domain, ServiceRoute, error) {
	d, ok, err := s.store.GetDomain(ctx, domainID)
	if err != nil {
		return Domain{}, ServiceRoute{}, problem.Internalf(err, "get domain")
	}
	if !ok {
		return Domain{}, ServiceRoute{}, problem.NotFound("domain %s not found", domainID)
	}
	svc, ok, err := s.store.ServiceRoute(ctx, d.ServiceID)
	if err != nil {
		return Domain{}, ServiceRoute{}, problem.Internalf(err, "get domain")
	}
	if !ok {
		return Domain{}, ServiceRoute{}, problem.NotFound("service %s not found", d.ServiceID)
	}
	return d, svc, nil
}

func (s *service) verify(ctx context.Context, d Domain, svc ServiceRoute) (string, string) {
	if svc.Visibility == "private" {
		return StatusBlocked, "Make the service public before attaching a custom domain."
	}
	target, ok := generatedHost(svc.RouteURL)
	if !ok {
		return StatusBlocked, "Deploy this public service first so Plorigo can create its generated domain."
	}
	if isApexDomain(d.Hostname) {
		got, err := s.resolver.LookupHost(ctx, d.Hostname)
		if err != nil {
			return StatusPendingDNS, "DNS is not pointing here yet. Add the shown A/AAAA records, wait for propagation, then verify again."
		}
		want, err := s.resolver.LookupHost(ctx, target)
		if err != nil || len(want) == 0 {
			return StatusPendingDNS, "Plorigo could not resolve the generated domain yet. Wait for the generated route to resolve, then verify again."
		}
		if intersects(hosts(got), hosts(want)) {
			return StatusVerified, "DNS is verified. The agent will activate this domain on the next route sync."
		}
		return StatusPendingDNS, "DNS is not pointing to this service yet. Update the shown A/AAAA records, then verify again."
	}
	cname, err := s.resolver.LookupCNAME(ctx, d.Hostname)
	if err == nil && sameDNSName(cname, target) {
		return StatusVerified, "DNS is verified. The agent will activate this domain on the next route sync."
	}
	return StatusPendingDNS, "DNS is not pointing here yet. Add the shown CNAME record, wait for propagation, then verify again."
}

func (s *service) enrich(ctx context.Context, d Domain) Domain {
	svc, ok, err := s.store.ServiceRoute(ctx, d.ServiceID)
	if err != nil || !ok {
		return d
	}
	return enrichWithService(d, svc, s.resolver)
}

func (s *service) enrichRows(ctx context.Context, rows []Domain) []Domain {
	for i := range rows {
		rows[i] = s.enrich(ctx, rows[i])
	}
	return rows
}

func enrichWithService(d Domain, svc ServiceRoute, resolver Resolver) Domain {
	d.DNSRecordName = d.Hostname
	if svc.Visibility == "private" || svc.RouteURL == "" {
		return d
	}
	target, ok := generatedHost(svc.RouteURL)
	if !ok {
		return d
	}
	if isApexDomain(d.Hostname) {
		d.DNSRecordType = RecordA
		d.DNSRecordValue = strings.Join(resolveIPs(context.Background(), resolver, target), ", ")
		if d.DNSRecordValue == "" {
			d.DNSRecordValue = target
		}
		return d
	}
	d.DNSRecordType = RecordCNAME
	d.DNSRecordValue = target
	return d
}

func initialStatus(svc ServiceRoute) (string, string) {
	if svc.Visibility == "private" {
		return StatusBlocked, "Make the service public before attaching a custom domain."
	}
	if svc.RouteURL == "" {
		return StatusBlocked, "Deploy this public service first so Plorigo can create its generated domain."
	}
	return StatusPendingDNS, "Add the shown DNS record at your DNS provider, then verify this domain."
}

func normalizeHostname(raw string) (string, error) {
	host := strings.TrimSpace(strings.ToLower(raw))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", problem.InvalidInput("domain is required")
	}
	if strings.Contains(host, "://") {
		return "", problem.InvalidInput("domain must not include http:// or https://")
	}
	if strings.ContainsAny(host, "/?#") {
		return "", problem.InvalidInput("domain must not include a path or query string")
	}
	if strings.Contains(host, ":") {
		return "", problem.InvalidInput("domain must not include a port")
	}
	if strings.HasPrefix(host, "*.") {
		return "", problem.InvalidInput("wildcard domains are not supported yet")
	}
	if strings.Contains(host, "*") {
		return "", problem.InvalidInput("wildcard domains are not supported yet")
	}
	if len(host) > maxHostnameLen {
		return "", problem.InvalidInput("domain must be at most %d characters", maxHostnameLen)
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", problem.InvalidInput("domain must include a public suffix, e.g. app.example.com")
	}
	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return "", err
		}
	}
	return host, nil
}

func validateLabel(label string) error {
	if label == "" {
		return problem.InvalidInput("domain labels must not be empty")
	}
	if len(label) > 63 {
		return problem.InvalidInput("domain label %q is too long", label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return problem.InvalidInput("domain label %q must not start or end with '-'", label)
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return problem.InvalidInput("domain label %q contains invalid character %q", label, r)
	}
	return nil
}

func generatedHost(routeURL string) (string, bool) {
	u, err := url.Parse(routeURL)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")), true
}

func isApexDomain(host string) bool {
	etld1, err := publicsuffix.EffectiveTLDPlusOne(host)
	return err == nil && host == etld1
}

func resolveIPs(ctx context.Context, resolver Resolver, host string) []string {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	got, err := resolver.LookupHost(ctx, host)
	if err != nil {
		return nil
	}
	return hosts(got)
}

func hosts(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range in {
		if ip := net.ParseIP(h); ip != nil {
			h = ip.String()
		}
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func intersects(a, b []string) bool {
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if set[v] {
			return true
		}
	}
	return false
}

func sameDNSName(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

func mapErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pe *problem.Error
	if errors.As(err, &pe) {
		return err
	}
	return problem.Internalf(fmt.Errorf("%s: %w", op, err), "%s", op)
}

// Package domains owns custom hostnames attached to services. A service keeps its generated
// route_url and can add multiple custom domains; each hostname is unique within a workspace.
package domains

import (
	"context"
	"time"
)

const (
	// StatusBlocked means the service is not ready for custom-domain setup yet.
	StatusBlocked = "blocked"
	// StatusPendingDNS means the user still needs to point DNS at the generated route.
	StatusPendingDNS = "pending_dns"
	// StatusVerified means DNS points at Plorigo and the agent can activate the route.
	StatusVerified = "verified"
	// StatusActive means the agent has successfully routed the hostname.
	StatusActive = "active"
	// StatusFailed means the latest route sync or verification failed.
	StatusFailed = "failed"
)

const (
	// RecordCNAME is the DNS record type used for non-apex custom domains.
	RecordCNAME = "CNAME"
	// RecordA is the IPv4 DNS record type used for apex custom domains.
	RecordA = "A"
	// RecordAAAA is the IPv6 DNS record type used for apex custom domains.
	RecordAAAA = "AAAA"
)

// Domain is the domain model for one custom hostname attached to a service.
type Domain struct {
	ID             string
	ServiceID      string
	EnvironmentID  string
	ProjectID      string
	WorkspaceID    string
	Hostname       string
	Status         string
	StatusMessage  string
	DNSRecordType  string
	DNSRecordName  string
	DNSRecordValue string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastCheckedAt  *time.Time
}

// CreateInput is what the dashboard supplies to attach a hostname to a service.
type CreateInput struct {
	ServiceID string
	Hostname  string
}

// Service is the dashboard-facing custom-domain service surface.
type Service interface {
	CreateDomain(ctx context.Context, in CreateInput) (Domain, error)
	ListByService(ctx context.Context, serviceID string) ([]Domain, error)
	ListByProject(ctx context.Context, projectID string) ([]Domain, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Domain, error)
	VerifyDomain(ctx context.Context, domainID string) (Domain, error)
	DeleteDomain(ctx context.Context, domainID string) error
}

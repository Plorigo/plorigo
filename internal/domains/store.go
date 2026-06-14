package domains

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// ServiceRoute is the service row subset needed to create, authorize, and verify domains.
type ServiceRoute struct {
	ID            string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	Visibility    string
	RouteURL      string
}

// Store is the repository port for the domains module. Implemented by postgres.go.
type Store interface {
	CreateDomain(ctx context.Context, tx database.Tx, d Domain) (Domain, error)
	GetDomain(ctx context.Context, id string) (Domain, bool, error)
	ListByService(ctx context.Context, serviceID string) ([]Domain, error)
	ListByProject(ctx context.Context, projectID string) ([]Domain, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Domain, error)
	UpdateVerification(ctx context.Context, tx database.Tx, id, status, message string) (Domain, error)
	DeleteDomain(ctx context.Context, tx database.Tx, id string) (deletedID string, ok bool, err error)
	ServiceRoute(ctx context.Context, serviceID string) (ServiceRoute, bool, error)
	WorkspaceForProject(ctx context.Context, projectID string) (workspaceID string, ok bool, err error)
}

// TxRunner runs fn inside one transaction.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the consumer-defined audit port domains needs.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// Resolver is the DNS lookup surface used by verification. net.Resolver satisfies it.
type Resolver interface {
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

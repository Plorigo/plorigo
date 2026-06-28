// Package config owns unified configuration: variables (plaintext, readable) and secrets
// (encrypted at rest via the Sealer port, WRITE-ONLY), each at SERVICE or ENVIRONMENT
// scope. It replaces the separate envvars (service-scoped, plaintext) and secrets
// (environment-scoped, encrypted) modules — type and scope are now independent axes.
//
// It is a PRIVILEGED module: every mutation is authorized via the neutral authz.Authorizer
// port (satisfied by the policy module) before it runs, and audited in the same
// transaction (modules.md, Rule 4). The owning workspace is resolved through the service
// (service scope) or the environment's project (environment scope). Secret plaintext is
// sealed before storage and is NEVER logged or returned — only the key + metadata appear.
// At deploy time a service receives its environment-shared entries merged with its own
// service-level entries, the latter overriding on a key collision; that merge lives in the
// deployments module, which reads this table. See docs/architecture/security.md and modules.md.
package config

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Type is whether an entry's value is readable plaintext or an encrypted secret.
type Type string

// The configuration entry types.
const (
	TypeVariable Type = "variable"
	TypeSecret   Type = "secret"
)

// Scope is whether an entry belongs to one service or is shared across an environment.
type Scope string

// The configuration entry scopes.
const (
	ScopeService     Scope = "service"
	ScopeEnvironment Scope = "environment"
)

// Entry is the domain model (independent of DB and transport types). Value is the plaintext
// for variables and is ALWAYS empty for secrets (the plaintext is sealed on write and never
// read back). Exactly one of ServiceID / EnvironmentID is set, matching Scope. WorkspaceID
// is the owning workspace, used to authorize and audit; it is not part of the wire contract.
type Entry struct {
	ID            string
	Type          Type
	Scope         Scope
	ServiceID     string
	EnvironmentID string
	WorkspaceID   string
	Key           string
	Value         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SetInput is the data to create or update an entry. Value is the plaintext; for secrets it
// is sealed before storage and never stored or logged in the clear. Set service_id when
// Scope is ScopeService, environment_id when ScopeEnvironment.
type SetInput struct {
	Type          Type
	Scope         Scope
	ServiceID     string
	EnvironmentID string
	Key           string
	Value         string
}

// DeleteInput identifies the entry to delete, by scope target and key.
type DeleteInput struct {
	Scope         Scope
	ServiceID     string
	EnvironmentID string
	Key           string
}

// Service is the surface other code (handlers, internal/app, tests) depends on. Set and
// ListForService return the plaintext for variables and metadata only (Value empty) for
// secrets.
type Service interface {
	Set(ctx context.Context, in SetInput) (Entry, error)
	// ListForService returns the service's service-level entries plus the entries shared
	// across the service's environment.
	ListForService(ctx context.Context, serviceID string) ([]Entry, error)
	Delete(ctx context.Context, in DeleteInput) error
	// SetWithinTx writes service-scoped configuration VARIABLES inside the CALLER's
	// transaction, for the services module provisioning a managed service's generated
	// config (e.g. a database's credentials) so the config and the service row commit
	// together. It performs NO authorization (the caller has already authorized the service
	// create) and logs nothing (values may be credentials). Keys are validated and values
	// bounded just as Set does.
	SetWithinTx(ctx context.Context, tx database.Tx, serviceID string, vars map[string]string) error
}

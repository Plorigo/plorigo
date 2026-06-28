package serversetup

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/sshkeys"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the serversetup module needs. Audit, Policy, and Crypto are
// CONSUMER-DEFINED ports (authz.Authorizer is satisfied by *policy.Service, Recorder by
// *audit.Service, Sealer by *crypto.Box), wired in internal/app — serversetup imports none
// of those modules.
type Deps struct {
	DB     *database.DB
	Audit  Recorder
	Policy authz.Authorizer
	Crypto Sealer
	Log    *slog.Logger
	// Dialer opens the bootstrap SSH session; Agents mints registration tokens and reports
	// agent liveness; PublicURL is the control-plane URL passed to the installer. All are
	// for the dashboard-managed setup run.
	Dialer    SSHDialer
	Agents    AgentProvisioner
	PublicURL string
}

// Module is the serversetup module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the service over its ports, using the real ed25519 key generator.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, defaultKeyGen{}, d.Crypto, d.Policy, d.Audit, d.Log, d.Dialer, d.Agents, d.PublicURL),
	}
}

// Service exposes the module's service interface (for internal/app, the bootstrap runner,
// and tests).
func (m *Module) Service() Service { return m.service }

// Route returns the ServerSetupService mount path and handler. opts carries the app-wide
// interceptors (e.g. the auth interceptor).
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewServerSetupServiceHandler(&handler{svc: m.service}, opts...)
}

// defaultKeyGen is the production KeyGenerator: a fresh ed25519 SSH keypair per call.
type defaultKeyGen struct{}

func (defaultKeyGen) Generate() (sshkeys.KeyPair, error) { return sshkeys.Generate() }

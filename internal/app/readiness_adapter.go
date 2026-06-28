package app

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/config"
	"github.com/plorigo/plorigo/internal/deployments"
	"github.com/plorigo/plorigo/internal/domains"
	"github.com/plorigo/plorigo/internal/readiness"
	"github.com/plorigo/plorigo/internal/services"
)

// These adapters fiber sibling modules' read-only Service() methods into the readiness module's
// consumer-defined ports, mapping each module's domain types to readiness's neutral structs. They
// live in internal/app (the single wiring surface) so the readiness module imports no sibling
// module. Every underlying call carries the request principal, so each sibling read authorizes
// itself (defense in depth on top of readiness's own ActionReadinessRead check).

type readinessServiceReader struct{ services services.Servicer }

var _ readiness.ServiceReader = readinessServiceReader{}

func (r readinessServiceReader) Get(ctx context.Context, serviceID string) (readiness.ServiceFacts, error) {
	s, err := r.services.GetService(ctx, serviceID)
	if err != nil {
		return readiness.ServiceFacts{}, err
	}
	return serviceFacts(s), nil
}

func (r readinessServiceReader) ListByEnvironment(ctx context.Context, environmentID string) ([]readiness.ServiceFacts, error) {
	list, err := r.services.ListByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	out := make([]readiness.ServiceFacts, 0, len(list))
	for _, s := range list {
		out = append(out, serviceFacts(s))
	}
	return out, nil
}

func serviceFacts(s services.Service) readiness.ServiceFacts {
	return readiness.ServiceFacts{
		ID:            s.ID,
		Name:          s.Name,
		WorkspaceID:   s.WorkspaceID,
		EnvironmentID: s.EnvironmentID,
		SourceKind:    s.SourceKind,
		Visibility:    s.Visibility,
		RouteURL:      s.RouteURL,
		ContainerPort: s.ContainerPort,
	}
}

type readinessConfigReader struct{ config config.Service }

var _ readiness.ConfigReader = readinessConfigReader{}

func (r readinessConfigReader) ListForService(ctx context.Context, serviceID string) ([]readiness.ConfigEntry, error) {
	entries, err := r.config.ListForService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]readiness.ConfigEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, readiness.ConfigEntry{
			Key:    e.Key,
			Secret: e.Type == config.TypeSecret,
			Value:  e.Value, // blank for secrets, so placeholder detection runs on variables only
		})
	}
	return out, nil
}

type readinessDomainReader struct{ domains domains.Service }

var _ readiness.DomainReader = readinessDomainReader{}

func (r readinessDomainReader) ListByService(ctx context.Context, serviceID string) ([]readiness.DomainFact, error) {
	list, err := r.domains.ListByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]readiness.DomainFact, 0, len(list))
	for _, d := range list {
		out = append(out, readiness.DomainFact{Hostname: d.Hostname, Status: d.Status})
	}
	return out, nil
}

type readinessDeploymentReader struct{ deployments deployments.Service }

var _ readiness.DeploymentReader = readinessDeploymentReader{}

// LatestForService returns the newest deployment (the list is newest-first), or ok=false when the
// service has never been deployed.
func (r readinessDeploymentReader) LatestForService(ctx context.Context, serviceID string) (readiness.DeploymentFact, bool, error) {
	list, err := r.deployments.ListByService(ctx, serviceID)
	if err != nil {
		return readiness.DeploymentFact{}, false, err
	}
	if len(list) == 0 {
		return readiness.DeploymentFact{}, false, nil
	}
	d := list[0]
	return readiness.DeploymentFact{Status: d.Status, ServerID: d.ServerID}, true, nil
}

type readinessServerReader struct {
	agents agents.Service
	now    func() time.Time
	dev    bool
}

var _ readiness.ServerReader = readinessServerReader{}

// readinessRank ranks server readiness states so WorkspaceReadiness can report the best server.
func readinessRank(state string) int {
	switch state {
	case agents.ReadinessReady:
		return 3
	case agents.ReadinessDegraded:
		return 2
	case agents.ReadinessBlocked:
		return 1
	default: // unknown
		return 0
	}
}

// WorkspaceReadiness returns the most-ready connected agent's readiness — the answer to "is there
// somewhere ready to deploy?" — computed the same way the dashboard derives it (now + dev relaxes
// the Linux-only host requirement).
func (r readinessServerReader) WorkspaceReadiness(ctx context.Context, workspaceID string) (readiness.ServerReadiness, error) {
	list, err := r.agents.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return readiness.ServerReadiness{}, err
	}
	if len(list) == 0 {
		return readiness.ServerReadiness{HasServer: false}, nil
	}
	now := r.now()
	best := readiness.ServerReadiness{HasServer: true, State: agents.ReadinessUnknown}
	bestRank := -1
	for _, a := range list {
		state, reason := a.Readiness(now, r.dev)
		if rank := readinessRank(state); rank > bestRank {
			bestRank = rank
			best.State, best.Reason = state, reason
		}
	}
	return best, nil
}

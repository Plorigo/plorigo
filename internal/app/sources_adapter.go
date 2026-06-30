package app

import (
	"context"

	"github.com/plorigo/plorigo/internal/services"
	"github.com/plorigo/plorigo/internal/sources"
)

// sourcesForServices bridges the sources module to the services module at the wiring layer:
// services defines its own ConnectionMeta/ResolvedRepo port types (so it never imports sources), and
// this adapter translates the sources domain types into them. Keeping the translation in internal/app
// preserves the module boundary that depguard enforces.
type sourcesForServices struct {
	svc sources.Service
}

var _ services.Sources = sourcesForServices{}

func (a sourcesForServices) GetConnectionMeta(ctx context.Context, connectionID string) (services.ConnectionMeta, bool, error) {
	c, ok, err := a.svc.GetConnectionMeta(ctx, connectionID)
	if err != nil || !ok {
		return services.ConnectionMeta{}, ok, err
	}
	return services.ConnectionMeta{
		WorkspaceID:  c.WorkspaceID,
		Provider:     c.Provider,
		Kind:         c.Kind,
		AccountLogin: c.AccountLogin,
	}, true, nil
}

func (a sourcesForServices) ValidateRepo(ctx context.Context, connectionID, owner, repo, branch string) (services.ResolvedRepo, error) {
	r, err := a.svc.ValidateRepo(ctx, connectionID, owner, repo, branch)
	if err != nil {
		return services.ResolvedRepo{}, err
	}
	return services.ResolvedRepo{
		Owner:         r.Owner,
		Name:          r.Name,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		HTMLURL:       r.HTMLURL,
		IsPrivate:     r.IsPrivate,
		Branch:        r.Branch,
		Provider:      r.Provider,
		Kind:          r.Kind,
		AccountLogin:  r.AccountLogin,
		Buildable:     r.Buildable,
	}, nil
}

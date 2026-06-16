package services

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/plorigo/plorigo/internal/builder"
	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	maxNameLen     = 100
	maxImageRefLen = 512
	maxPort        = 65535
)

// service is the business logic. It orchestrates ports only — no SQL, no transport, and no
// cryptography of its own. Every mutation resolves the owning workspace, authorizes the
// caller BEFORE the WithinTx block, and audits inside it (modules.md, Rule 4). Git source
// validation hits GitHub BEFORE the transaction (network I/O must not run inside a DB tx).
// An OAuth token is opened only to validate a connected repo and is never returned or logged.
type service struct {
	tx         TxRunner
	store      Store
	box        SecretBox
	gh         GitHubClient
	enqueuer   Enqueuer
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, box SecretBox, gh GitHubClient, enqueuer Enqueuer, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, box: box, gh: gh, enqueuer: enqueuer, authorizer: authorizer, audit: audit, log: log}
}

var _ Servicer = (*service)(nil)

// resolvedSource is a validated source ready to persist, plus whether it can be deployed now
// (image/template, or a PUBLIC git repo — an OAuth/private repo can't be built yet).
type resolvedSource struct {
	kind          string
	imageRef      string
	templateID    string
	access        string
	connectionID  string
	owner         string
	repo          string
	fullName      string
	branch        string
	defaultBranch string
	htmlURL       string
	isPrivate     bool
	githubLogin   string
	buildable     bool
}

func (s *service) CreateService(ctx context.Context, in CreateInput) (Result, error) {
	if _, err := id.Parse(in.EnvironmentID); err != nil {
		return Result{}, problem.InvalidInput("a valid environment_id is required")
	}
	name, slug, err := validateName(in.Name)
	if err != nil {
		return Result{}, err
	}
	visibility, err := validateVisibility(in.Visibility)
	if err != nil {
		return Result{}, err
	}

	workspaceID, projectID, ok, err := s.store.WorkspaceAndProjectForEnvironment(ctx, in.EnvironmentID)
	if err != nil {
		return Result{}, problem.Internalf(err, "create service")
	}
	if !ok {
		return Result{}, problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionServiceCreate, authz.Resource{Type: "service", WorkspaceID: workspaceID}); err != nil {
		return Result{}, err
	}

	rs, err := s.resolveSource(ctx, workspaceID, in.SourceKind, in.ImageRef, in.TemplateID, in.RepoURL, in.Owner, in.Repo, in.Branch)
	if err != nil {
		return Result{}, err
	}
	port, err := validatePort(rs.kind, in.ContainerPort)
	if err != nil {
		return Result{}, err
	}

	// deploy_now needs a valid server in the same workspace — but only when the source can
	// actually be built (an OAuth/private git service is created without a first deployment).
	deploy := in.DeployNow && rs.buildable
	var serverID string
	if deploy {
		if _, err := id.Parse(in.ServerID); err != nil {
			return Result{}, problem.InvalidInput("a valid server_id is required to deploy")
		}
		serverWorkspace, ok, err := s.store.WorkspaceForServer(ctx, in.ServerID)
		if err != nil {
			return Result{}, problem.Internalf(err, "create service")
		}
		if !ok || serverWorkspace != workspaceID {
			return Result{}, problem.NotFound("server %s not found in this workspace", in.ServerID)
		}
		serverID = in.ServerID
	}

	var saved Service
	var deploymentID string
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if rs.kind == SourceGit {
			saved, txErr = s.store.InsertGitService(ctx, tx, GitServiceWrite{
				EnvironmentID: in.EnvironmentID, ProjectID: projectID, WorkspaceID: workspaceID,
				Name: name, Slug: slug, SourceAccess: rs.access, ConnectionID: rs.connectionID,
				Provider: provider, Owner: rs.owner, Repo: rs.repo, FullName: rs.fullName,
				Branch: rs.branch, DefaultBranch: rs.defaultBranch, IsPrivate: rs.isPrivate,
				HTMLURL: rs.htmlURL, ContainerPort: port, Visibility: visibility,
			})
		} else {
			saved, txErr = s.store.InsertService(ctx, tx, ServiceWrite{
				EnvironmentID: in.EnvironmentID, ProjectID: projectID, WorkspaceID: workspaceID,
				Name: name, Slug: slug, SourceKind: rs.kind, ImageRef: rs.imageRef,
				TemplateID: rs.templateID, ContainerPort: port, Visibility: visibility,
			})
		}
		if txErr != nil {
			return txErr
		}
		if txErr := s.audit.Record(ctx, tx, "service.create", "service", saved.ID, workspaceID, caller.UserID); txErr != nil {
			return txErr
		}
		if deploy {
			deploymentID, txErr = s.enqueuer.EnqueueFirstDeployment(ctx, tx, saved.ID, serverID)
			if txErr != nil {
				return txErr
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, mapErr(err, "create service")
	}
	saved.GitHubLogin = rs.githubLogin
	s.log.Info("service created", "id", saved.ID, "environment_id", saved.EnvironmentID, "source_kind", saved.SourceKind, "visibility", visibility, "deployed", deploy, "workspace_id", workspaceID, "actor", caller.UserID)
	return Result{Service: saved, DeploymentID: deploymentID}, nil
}

func (s *service) GetService(ctx context.Context, serviceID string) (Service, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return Service{}, problem.InvalidInput("a valid service id is required")
	}
	svc, ok, err := s.store.GetService(ctx, serviceID)
	if err != nil {
		return Service{}, problem.Internalf(err, "get service")
	}
	if !ok {
		return Service{}, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServiceRead, authz.Resource{Type: "service", WorkspaceID: svc.WorkspaceID, ID: svc.ID}); err != nil {
		return Service{}, err
	}
	return svc, nil
}

func (s *service) ListByEnvironment(ctx context.Context, environmentID string) ([]Service, error) {
	if _, err := id.Parse(environmentID); err != nil {
		return nil, problem.InvalidInput("a valid environment_id is required")
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForEnvironment(ctx, environmentID)
	if err != nil {
		return nil, problem.Internalf(err, "list services")
	}
	if !ok {
		return nil, problem.NotFound("environment %s not found", environmentID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServiceRead, authz.Resource{Type: "service", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByEnvironment(ctx, environmentID)
}

func (s *service) ListByProject(ctx context.Context, projectID string) ([]Service, error) {
	if _, err := id.Parse(projectID); err != nil {
		return nil, problem.InvalidInput("a valid project_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceForProject(ctx, projectID)
	if err != nil {
		return nil, problem.Internalf(err, "list services")
	}
	if !ok {
		return nil, problem.NotFound("project %s not found", projectID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServiceRead, authz.Resource{Type: "service", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByProject(ctx, projectID)
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Service, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return nil, problem.InvalidInput("a valid workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionServiceRead, authz.Resource{Type: "service", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

func (s *service) UpdateSource(ctx context.Context, in UpdateSourceInput) (Service, error) {
	if _, err := id.Parse(in.ID); err != nil {
		return Service{}, problem.InvalidInput("a valid service id is required")
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, in.ID)
	if err != nil {
		return Service{}, problem.Internalf(err, "update service source")
	}
	if !ok {
		return Service{}, problem.NotFound("service %s not found", in.ID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionServiceUpdate, authz.Resource{Type: "service", WorkspaceID: workspaceID, ID: in.ID}); err != nil {
		return Service{}, err
	}
	rs, err := s.resolveSource(ctx, workspaceID, in.SourceKind, in.ImageRef, in.TemplateID, in.RepoURL, in.Owner, in.Repo, in.Branch)
	if err != nil {
		return Service{}, err
	}
	port, err := validatePort(rs.kind, in.ContainerPort)
	if err != nil {
		return Service{}, err
	}

	var saved Service
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		saved, txErr = s.store.UpdateServiceSource(ctx, tx, SourceWrite{
			ID: in.ID, SourceKind: rs.kind, ImageRef: rs.imageRef, TemplateID: rs.templateID,
			ConnectionID: rs.connectionID, Provider: providerFor(rs.kind), Owner: rs.owner,
			Repo: rs.repo, FullName: rs.fullName, Branch: rs.branch, DefaultBranch: rs.defaultBranch,
			IsPrivate: rs.isPrivate, HTMLURL: rs.htmlURL, SourceAccess: rs.access, ContainerPort: port,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "service.update", "service", in.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Service{}, mapErr(err, "update service source")
	}
	saved.GitHubLogin = rs.githubLogin
	s.log.Info("service source updated", "id", in.ID, "source_kind", saved.SourceKind, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

func (s *service) UpdateVisibility(ctx context.Context, serviceID, visibility string) (Service, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return Service{}, problem.InvalidInput("a valid service id is required")
	}
	vis, err := validateVisibility(visibility)
	if err != nil {
		return Service{}, err
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, serviceID)
	if err != nil {
		return Service{}, problem.Internalf(err, "update service visibility")
	}
	if !ok {
		return Service{}, problem.NotFound("service %s not found", serviceID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionServiceUpdate, authz.Resource{Type: "service", WorkspaceID: workspaceID, ID: serviceID}); err != nil {
		return Service{}, err
	}
	var saved Service
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		saved, txErr = s.store.UpdateVisibility(ctx, tx, serviceID, vis)
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "service.update", "service", serviceID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Service{}, mapErr(err, "update service visibility")
	}
	s.log.Info("service visibility updated", "id", serviceID, "visibility", vis, "workspace_id", workspaceID, "actor", caller.UserID)
	return saved, nil
}

func (s *service) DeleteService(ctx context.Context, serviceID string) error {
	if _, err := id.Parse(serviceID); err != nil {
		return problem.InvalidInput("a valid service id is required")
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, serviceID)
	if err != nil {
		return problem.Internalf(err, "delete service")
	}
	if !ok {
		return problem.NotFound("service %s not found", serviceID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionServiceDelete, authz.Resource{Type: "service", WorkspaceID: workspaceID, ID: serviceID}); err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteService(ctx, tx, serviceID)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			return problem.NotFound("service %s not found", serviceID)
		}
		return s.audit.Record(ctx, tx, "service.delete", "service", deletedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "delete service")
	}
	s.log.Info("service deleted", "id", serviceID, "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

// DetectFramework previews how a PUBLIC repo would build: it reads the repo's files (no
// credential) and runs the shared internal/builder detection — the same rules the agent runs
// at build time, so the dashboard preview is exactly what will be built. It creates nothing and
// touches no tenant data, so it relies on the session auth interceptor rather than per-resource
// authorization.
func (s *service) DetectFramework(ctx context.Context, in DetectInput) (Detection, error) {
	owner, repo, err := parsePublicRepo(in.RepoURL)
	if err != nil {
		return Detection{}, err
	}
	// Read with no token: a private/missing repo is simply invisible, and only public repos
	// are buildable in this slice.
	info, err := s.gh.GetRepository(ctx, "", owner, repo)
	if err != nil {
		return Detection{}, mapGitHubErr(err)
	}
	if info.Private {
		return Detection{}, problem.InvalidInput("%s is private; connect GitHub to deploy a private repository", info.FullName)
	}
	ref := strings.TrimSpace(in.Branch)
	if ref == "" {
		ref = info.DefaultBranch
	}
	plan, err := builder.Detect(githubFiles{ctx: ctx, gh: s.gh, owner: info.Owner, repo: info.Name, ref: ref})
	if err != nil {
		return Detection{}, problem.Internalf(err, "detect framework")
	}
	return Detection{
		Status:         string(plan.Status),
		Runtime:        plan.Runtime,
		RuntimeLabel:   plan.RuntimeLabel(),
		PackageManager: plan.PackageManager,
		NodeVersion:    plan.NodeVersion,
		BuildCommand:   plan.BuildCommand,
		StartCommand:   plan.StartCommand,
		ContainerPort:  plan.Port,
		Dockerfile:     plan.Dockerfile,
		NextSteps:      plan.NextSteps,
	}, nil
}

// githubFiles adapts the GitHubClient port to builder.Files for a public repo at ref. The
// request context is captured because builder.Files is context-free (so the agent's local-file
// implementation needs none); this adapter is short-lived and scoped to one DetectFramework
// call.
type githubFiles struct {
	ctx         context.Context
	gh          GitHubClient
	owner, repo string
	ref         string
}

func (g githubFiles) ReadFile(path string) ([]byte, bool, error) {
	return g.gh.GetFileContent(g.ctx, "", g.owner, g.repo, g.ref, path)
}

// resolveSource validates the chosen source and (for git) confirms the repo + branch with
// GitHub before any database write. It returns the authoritative fields to persist.
func (s *service) resolveSource(ctx context.Context, workspaceID, kind, imageRef, templateID, repoURL, owner, repo, branch string) (resolvedSource, error) {
	switch kind {
	case SourceImage:
		ref, err := validateImageRef(imageRef)
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{kind: SourceImage, imageRef: ref, buildable: true}, nil

	case SourceTemplate:
		ref, err := validateImageRef(imageRef)
		if err != nil {
			return resolvedSource{}, err
		}
		if strings.TrimSpace(templateID) == "" {
			return resolvedSource{}, problem.InvalidInput("a template_id is required for a template service")
		}
		return resolvedSource{kind: SourceTemplate, imageRef: ref, templateID: strings.TrimSpace(templateID), buildable: true}, nil

	case SourceGit:
		return s.resolveGitSource(ctx, workspaceID, repoURL, owner, repo, branch)

	default:
		return resolvedSource{}, problem.InvalidInput("source_kind must be one of %q, %q, or %q", SourceImage, SourceGit, SourceTemplate)
	}
}

// resolveGitSource handles both the PUBLIC path (a repo URL, read anonymously) and the OAuth
// path (a connected owner/repo, read with the workspace's token). Only public repos are
// buildable in this slice.
func (s *service) resolveGitSource(ctx context.Context, workspaceID, repoURL, owner, repo, branch string) (resolvedSource, error) {
	branch = strings.TrimSpace(branch)
	if strings.TrimSpace(repoURL) != "" {
		// Public: read the repo with no credential, so a private/missing repo is invisible.
		o, r, err := parsePublicRepo(repoURL)
		if err != nil {
			return resolvedSource{}, err
		}
		info, err := s.gh.GetRepository(ctx, "", o, r)
		if err != nil {
			return resolvedSource{}, mapGitHubErr(err)
		}
		if info.Private {
			return resolvedSource{}, problem.InvalidInput("%s is private; connect GitHub to deploy a private repository", info.FullName)
		}
		b, err := s.resolveBranch(ctx, "", info, branch)
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{
			kind: SourceGit, access: accessPublic, owner: info.Owner, repo: info.Name,
			fullName: info.FullName, branch: b, defaultBranch: info.DefaultBranch,
			htmlURL: info.HTMLURL, isPrivate: false, buildable: true,
		}, nil
	}

	// OAuth: validate a connected repo with the workspace's token. Not buildable yet (no
	// credential leaves the control plane), but the service is still recorded.
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return resolvedSource{}, problem.InvalidInput("a public repository URL, or a connected owner and repo, is required")
	}
	connID, login, ok, err := s.store.GetConnection(ctx, workspaceID)
	if err != nil {
		return resolvedSource{}, problem.Internalf(err, "resolve git source")
	}
	if !ok {
		return resolvedSource{}, problem.NotFound("connect GitHub for this workspace first")
	}
	token, err := s.openConnectionToken(ctx, workspaceID)
	if err != nil {
		return resolvedSource{}, err
	}
	info, err := s.gh.GetRepository(ctx, token, strings.TrimSpace(owner), strings.TrimSpace(repo))
	if err != nil {
		return resolvedSource{}, mapGitHubErr(err)
	}
	b, err := s.resolveBranch(ctx, token, info, branch)
	if err != nil {
		return resolvedSource{}, err
	}
	return resolvedSource{
		kind: SourceGit, access: accessOAuth, connectionID: connID, owner: info.Owner,
		repo: info.Name, fullName: info.FullName, branch: b, defaultBranch: info.DefaultBranch,
		htmlURL: info.HTMLURL, isPrivate: info.Private, githubLogin: login, buildable: false,
	}, nil
}

// resolveBranch defaults an empty branch to the repo's default; otherwise it verifies the
// chosen branch directly (avoiding the capped branch-list page).
func (s *service) resolveBranch(ctx context.Context, token string, info github.RepoInfo, branch string) (string, error) {
	if branch == "" {
		return info.DefaultBranch, nil
	}
	if err := s.gh.GetBranch(ctx, token, info.Owner, info.Name, branch); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return "", problem.InvalidInput("branch %q was not found in %s", branch, info.FullName)
		}
		return "", mapGitHubErr(err)
	}
	return branch, nil
}

// openConnectionToken loads and opens the workspace's sealed token for a server-side provider
// call. The plaintext stays in memory for the call only and is never returned to a caller.
func (s *service) openConnectionToken(ctx context.Context, workspaceID string) (string, error) {
	cipher, ok, err := s.store.GetConnectionToken(ctx, workspaceID)
	if err != nil {
		return "", problem.Internalf(err, "load github token")
	}
	if !ok {
		return "", problem.NotFound("connect GitHub for this workspace first")
	}
	plain, err := s.box.Open(cipher)
	if err != nil {
		return "", problem.Internalf(err, "open github token")
	}
	return string(plain), nil
}

// providerFor reports the provider stored for a source kind: GitHub for a git source, empty
// for an image/template service.
func providerFor(kind string) string {
	if kind == SourceGit {
		return provider
	}
	return ""
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// validateName trims a service name and derives its DNS-safe slug (which doubles as the
// service's network alias). It rejects a name that slugifies to empty.
func validateName(raw string) (name, slug string, err error) {
	name = strings.TrimSpace(raw)
	if name == "" {
		return "", "", problem.InvalidInput("a service name is required")
	}
	if len(name) > maxNameLen {
		return "", "", problem.InvalidInput("name must be at most %d characters", maxNameLen)
	}
	slug = strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		return "", "", problem.InvalidInput("name must contain a letter or number")
	}
	return name, slug, nil
}

func validateVisibility(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return VisibilityPublic, nil
	}
	if v != VisibilityPublic && v != VisibilityPrivate {
		return "", problem.InvalidInput("visibility must be %q or %q", VisibilityPublic, VisibilityPrivate)
	}
	return v, nil
}

// validatePort bounds the container port. An image/template service must declare a port; a
// git service may pass 0 to auto-detect from the Dockerfile EXPOSE on the agent.
func validatePort(kind string, port int32) (int32, error) {
	if port < 0 || port > maxPort {
		return 0, problem.InvalidInput("container_port must be between 1 and %d", maxPort)
	}
	if kind != SourceGit && port < 1 {
		return 0, problem.InvalidInput("a container_port is required for an image service")
	}
	return port, nil
}

// validateImageRef trims and sanity-checks a container image reference, defaulting the tag
// to :latest when none is given. This slice pulls PUBLIC images only.
func validateImageRef(raw string) (string, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return "", problem.InvalidInput("an image reference is required")
	}
	if len(ref) > maxImageRefLen {
		return "", problem.InvalidInput("image reference must be at most %d characters", maxImageRefLen)
	}
	if strings.ContainsAny(ref, " \t\n\r") {
		return "", problem.InvalidInput("image reference must not contain whitespace")
	}
	if !strings.Contains(ref, "@") {
		seg := ref[strings.LastIndex(ref, "/")+1:]
		if !strings.Contains(seg, ":") {
			ref += ":latest"
		}
	}
	return ref, nil
}

// parsePublicRepo extracts owner and repo from a public GitHub reference: a full URL, the SSH
// form, or a bare "owner/repo". It rejects anything that does not resolve to owner/repo on
// github.com.
func parsePublicRepo(raw string) (owner, repo string, err error) {
	str := strings.TrimSpace(raw)
	bad := func() (string, string, error) {
		return "", "", problem.InvalidInput("enter a public GitHub repository URL like https://github.com/owner/repo")
	}
	if str == "" {
		return bad()
	}
	if rest, ok := strings.CutPrefix(str, "git@github.com:"); ok {
		str = rest
	} else {
		str = strings.TrimPrefix(str, "https://")
		str = strings.TrimPrefix(str, "http://")
		str = strings.TrimPrefix(str, "www.")
		if rest, ok := strings.CutPrefix(str, "github.com/"); ok {
			str = rest
		} else if first, _, found := strings.Cut(str, "/"); found && strings.Contains(first, ".") {
			return bad()
		}
	}
	parts := strings.Split(strings.TrimPrefix(str, "/"), "/")
	if len(parts) < 2 {
		return bad()
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return bad()
	}
	return owner, repo, nil
}

// mapGitHubErr translates the github client's typed errors into plain-English domain errors.
// A missing and an inaccessible repo both surface as NotFound.
func mapGitHubErr(err error) error {
	switch {
	case errors.Is(err, github.ErrNotFound):
		return problem.NotFound("repository not found, or your GitHub access can't see it")
	case errors.Is(err, github.ErrUnauthorized):
		return problem.InvalidInput("GitHub rejected the request; the connection may have been revoked — reconnect GitHub")
	case errors.Is(err, github.ErrForbidden):
		return problem.PermissionDenied("GitHub denied access to this repository")
	case errors.Is(err, github.ErrRateLimited):
		return problem.Internalf(err, "GitHub rate limit reached; please try again shortly")
	default:
		return problem.Internalf(err, "could not reach GitHub")
	}
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an internal
// error, so a unique violation (AlreadyExists) surfaces unchanged rather than masked.
func mapErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pe *problem.Error
	if errors.As(err, &pe) {
		return err
	}
	return problem.Internalf(err, "%s", op)
}

package deployments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	maxImageRefLen = 512
	maxPort        = 65535
)

// service is the business logic. It orchestrates ports only — no SQL, no transport.
// Dashboard-facing methods resolve the owning workspace (through the environment's
// project, the project, or the deployment row) and authorize the caller BEFORE the
// WithinTx block, auditing inside it (modules.md, Rule 4). The agent-facing
// Poll/Report RPCs authenticate by the durable agent credential carried in the request
// (like Heartbeat) and scope work to the agent's own server, so they do not go through
// the authorizer.
type service struct {
	tx         TxRunner
	store      Store
	authorizer authz.Authorizer
	audit      Recorder
	opener     Opener
	gh         GitHubClient
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, authorizer authz.Authorizer, audit Recorder, opener Opener, gh GitHubClient, log *slog.Logger) *service {
	return &service{tx: tx, store: store, authorizer: authorizer, audit: audit, opener: opener, gh: gh, log: log}
}

var _ Service = (*service)(nil)

// Config scope/type values, matching the config_entries table (deployments must not import
// the config module — these are the stored enum strings).
const (
	configScopeService     = "service"
	configScopeEnvironment = "environment"
	configTypeSecret       = "secret"
)

// configEnvForService builds the container env for a service: its environment-shared entries
// merged with its own service-level entries, the latter overriding on a key collision
// (environment = defaults, service = override). Secret values are decrypted here via the
// Opener; variable values are plaintext. The merged map of plaintext KEY=VALUE travels to
// the agent in the signed deploy job, exactly as env vars always have.
//
// includeSecrets is false for a PREVIEW deployment: a preview builds untrusted branch/PR code,
// so it must not receive the environment's decrypted secrets (it gets non-secret variables
// only). This is the first line of preview isolation; the preview also runs on its own network
// (see PollDeployment). See docs/architecture/deployment-engine.md and security.md.
func (s *service) configEnvForService(ctx context.Context, serviceID string, includeSecrets bool) (map[string]string, error) {
	entries, err := s.store.ConfigForService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string, len(entries))
	// Apply environment-shared first, then service-level so service overrides on collision.
	for _, scope := range []string{configScopeEnvironment, configScopeService} {
		for _, e := range entries {
			if e.Scope != scope {
				continue
			}
			if e.Type == configTypeSecret && !includeSecrets {
				continue
			}
			val, verr := s.configValue(e)
			if verr != nil {
				return nil, verr
			}
			env[e.Key] = val
		}
	}
	return env, nil
}

// configValue returns an entry's plaintext: a variable's stored value, or a secret's
// decrypted ciphertext (opened in-process, never logged).
func (s *service) configValue(e ConfigForDeploy) (string, error) {
	if e.Type == configTypeSecret {
		plain, err := s.opener.Open(e.Ciphertext)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}
	if e.Value != nil {
		return *e.Value, nil
	}
	return "", nil
}

// CreateForService records a queued deployment for an existing service on a server, so the
// agent for that server claims it on its next poll. The service resolves the source
// server-side from the service row (a pre-built image, or a PUBLIC git repo it clones and
// builds) — a caller can't smuggle a private URL through. The server must belong to the
// service's workspace (cross-tenant guard). container_port and git_ref are optional
// overrides (0 / empty = the service's configured port and branch/default).
func (s *service) CreateForService(ctx context.Context, in CreateForServiceInput) (Deployment, error) {
	if _, err := id.Parse(in.ServiceID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid service_id is required")
	}
	if _, err := id.Parse(in.ServerID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid server_id is required")
	}
	// 0 means "use the service's configured port" (which for git may itself be 0 =
	// auto-detect from the Dockerfile EXPOSE on the agent after the build).
	if in.ContainerPort < 0 || in.ContainerPort > maxPort {
		return Deployment{}, problem.InvalidInput("container_port must be between 1 and %d, or 0 to use the service's port", maxPort)
	}

	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, in.ServiceID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment")
	}
	if !ok {
		return Deployment{}, problem.NotFound("service %s not found", in.ServiceID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentCreate, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return Deployment{}, err
	}

	// The target server must live in the same workspace as the service. Resolving after
	// authorization (and treating another workspace's server as not-found) avoids revealing
	// servers the caller has no access to.
	serverWorkspace, ok, err := s.store.WorkspaceForServer(ctx, in.ServerID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment")
	}
	if !ok || serverWorkspace != workspaceID {
		return Deployment{}, problem.NotFound("server %s not found in this workspace", in.ServerID)
	}

	var created Deployment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		svc, ok, txErr := s.store.ServiceForDeployTx(ctx, tx, in.ServiceID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return problem.NotFound("service %s not found", in.ServiceID)
		}
		created, txErr = s.buildAndInsert(ctx, tx, in.ServiceID, svc, in.ServerID, in.ContainerPort, in.GitRef)
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.create", "deployment", created.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Deployment{}, mapErr(err, "create deployment")
	}
	s.log.Info("deployment created", "id", created.ID, "service_id", in.ServiceID, "server_id", created.ServerID, "source_kind", created.SourceKind, "workspace_id", workspaceID, "actor", caller.UserID)
	return created, nil
}

// CreatePreview enqueues a preview deployment of an existing git service: a build of a branch,
// or of a pull request's head ref (resolved through GitHub and linked back via pr_url). The
// preview runs alongside production with its own route_key — its own URL, container-replacement
// group, supersede scope, and isolated network — so it never disturbs the service's production
// deployment. The service's source is resolved server-side (a caller can't smuggle a private
// URL through); only PUBLIC git services are buildable in this slice.
func (s *service) CreatePreview(ctx context.Context, in CreatePreviewInput) (Deployment, error) {
	if _, err := id.Parse(in.ServiceID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid service_id is required")
	}
	if _, err := id.Parse(in.ServerID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid server_id is required")
	}
	if in.ContainerPort < 0 || in.ContainerPort > maxPort {
		return Deployment{}, problem.InvalidInput("container_port must be between 1 and %d, or 0 to use the service's port", maxPort)
	}
	branch := strings.TrimSpace(in.Branch)
	// Exactly one of branch / pr_number identifies what to build.
	if (branch == "") == (in.PRNumber <= 0) {
		return Deployment{}, problem.InvalidInput("provide exactly one of a branch or a pull request number")
	}

	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, in.ServiceID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create preview")
	}
	if !ok {
		return Deployment{}, problem.NotFound("service %s not found", in.ServiceID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentCreate, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return Deployment{}, err
	}

	serverWorkspace, ok, err := s.store.WorkspaceForServer(ctx, in.ServerID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create preview")
	}
	if !ok || serverWorkspace != workspaceID {
		return Deployment{}, problem.NotFound("server %s not found in this workspace", in.ServerID)
	}

	return s.enqueuePreview(ctx, in.ServiceID, in.ServerID, branch, in.PRNumber, in.ContainerPort, in.Password, in.PasswordUser, caller.UserID)
}

// enqueuePreview resolves a git service's source and (for a PR) its head ref, derives the preview
// route_key, and inserts a queued preview deployment + audit row in one tx. It is the shared core of
// the dashboard-authorized CreatePreview and the webhook-driven CreatePreviewForPR — so both build a
// preview identically (public git only); only the audit actor differs. An optional password is
// bcrypt-hashed here (the plaintext is never stored or sent to the agent). The caller has already
// validated the inputs (and, for CreatePreview, authorized + checked the server's workspace). The
// PR lookup is network I/O, so it runs BEFORE the transaction.
func (s *service) enqueuePreview(ctx context.Context, serviceID, serverID, branch string, prNumber, portOverride int32, password, passwordUser, actor string) (Deployment, error) {
	svc, ok, err := s.store.ServiceForDeploy(ctx, serviceID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create preview")
	}
	if !ok {
		return Deployment{}, problem.NotFound("service %s not found", serviceID)
	}
	if svc.SourceKind != SourceGit {
		return Deployment{}, problem.InvalidInput("previews are only supported for git services")
	}
	if svc.SourceAccess != "public" {
		return Deployment{}, problem.InvalidInput("building private repositories isn't supported yet — connect a public repo (GitHub App support is coming)")
	}

	// Resolve the ref to build and the PR linkage. A PR number is resolved to its head ref; the
	// route_key is keyed by PR number (stable across pushes) or by a slug of the branch.
	gitRef := branch
	var prURL string
	if prNumber > 0 {
		pr, perr := s.gh.GetPullRequest(ctx, "", svc.Owner, svc.Repo, int(prNumber))
		if perr != nil {
			return Deployment{}, mapGitHubErr(perr)
		}
		if pr.HeadRef == "" {
			return Deployment{}, problem.NotFound("pull request #%d was not found in %s/%s", prNumber, svc.Owner, svc.Repo)
		}
		gitRef = pr.HeadRef
		prURL = pr.HTMLURL
	}

	port := svc.ContainerPort
	if portOverride > 0 {
		port = portOverride
	}
	routeKey := previewRouteKey(serviceID, prNumber, gitRef)

	// Optionally protect the preview URL with basic auth. Only the bcrypt hash is stored/sent; the
	// plaintext password never leaves this function.
	authUser, authHash, err := hashPreviewPassword(password, passwordUser)
	if err != nil {
		return Deployment{}, err
	}

	var created Deployment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		created, txErr = s.store.InsertPreviewDeployment(ctx, tx, NewPreviewDeployment{
			ServiceID:     serviceID,
			RouteKey:      routeKey,
			EnvironmentID: svc.EnvironmentID,
			ProjectID:     svc.ProjectID,
			WorkspaceID:   svc.WorkspaceID,
			ServerID:      serverID,
			ContainerPort: port,
			SourceAccess:  svc.SourceAccess,
			// Provider is GitHub-only today; construct the standard clone URL from owner/repo.
			CloneURL: "https://github.com/" + svc.Owner + "/" + svc.Repo + ".git",
			GitRef:   gitRef,
			PRNumber: prNumber,
			PRURL:    prURL,
			AuthUser: authUser,
			AuthHash: authHash,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.preview", "deployment", created.ID, svc.WorkspaceID, actor)
	})
	if err != nil {
		return Deployment{}, mapErr(err, "create preview")
	}
	s.log.Info("preview deployment created", "id", created.ID, "service_id", serviceID, "route_key", routeKey, "pr_number", prNumber, "git_ref", gitRef, "server_id", serverID, "actor", actor)
	return created, nil
}

// RollbackToDeployment redeploys a previous healthy version: it enqueues a NEW deployment
// that reproduces the target deployment's artifact — the same pre-built image, or the same
// repo pinned to the exact built commit — on the same service and server, linked back via
// rolled_back_from so the action shows in deployment history. The new deployment runs the
// normal flow (health check, then traffic switch), so a failed rollback keeps the current
// running release and leaves its own logs to diagnose. The target must be a previously
// healthy deployment (running or superseded).
func (s *service) RollbackToDeployment(ctx context.Context, targetDeploymentID string) (Deployment, error) {
	if _, err := id.Parse(targetDeploymentID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid deployment id is required")
	}
	target, ok, err := s.store.GetDeployment(ctx, targetDeploymentID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "rollback deployment")
	}
	if !ok {
		return Deployment{}, problem.NotFound("deployment %s not found", targetDeploymentID)
	}

	caller := principal.FromContext(ctx)
	// A rollback creates a deployment, so it takes the same privilege as a deploy.
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentCreate, authz.Resource{Type: "deployment", WorkspaceID: target.WorkspaceID}); err != nil {
		return Deployment{}, err
	}

	// Only a previously healthy version is a valid target: it ran successfully at least once.
	// A queued/in-flight/failed deployment is not a known-good state to restore.
	if target.Status != StatusRunning && target.Status != StatusSuperseded {
		return Deployment{}, problem.InvalidInput("can only roll back to a previously healthy deployment; %s is %q", target.ID, target.Status)
	}

	var created Deployment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if created, txErr = s.insertRollback(ctx, tx, target); txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.rollback", "deployment", created.ID, target.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return Deployment{}, mapErr(err, "rollback deployment")
	}
	s.log.Info("deployment rolled back", "id", created.ID, "rolled_back_from", target.ID, "service_id", target.ServiceID, "server_id", target.ServerID, "workspace_id", target.WorkspaceID, "actor", caller.UserID)
	return created, nil
}

// insertRollback enqueues a deployment reproducing target's artifact inside tx, linked back
// via rolled_back_from. A git target rebuilds the exact commit it ran (so the rollback does
// not depend on a built image still being on the server); an image target reruns its image.
func (s *service) insertRollback(ctx context.Context, tx database.Tx, target Deployment) (Deployment, error) {
	if target.SourceKind == SourceGit {
		// Pin to the exact commit the target built; fall back to its ref if unknown.
		ref := target.CommitSha
		if ref == "" {
			ref = target.GitRef
		}
		return s.store.InsertDeploymentFromGit(ctx, tx, NewDeploymentFromGit{
			ServiceID:      target.ServiceID,
			EnvironmentID:  target.EnvironmentID,
			ProjectID:      target.ProjectID,
			WorkspaceID:    target.WorkspaceID,
			ServerID:       target.ServerID,
			ContainerPort:  target.ContainerPort,
			SourceAccess:   target.SourceAccess,
			CloneURL:       target.CloneURL,
			GitRef:         ref,
			RolledBackFrom: target.ID,
		})
	}
	imageRef, err := validateImageRef(target.ImageRef)
	if err != nil {
		return Deployment{}, err
	}
	return s.store.InsertDeployment(ctx, tx, NewDeployment{
		ServiceID:      target.ServiceID,
		EnvironmentID:  target.EnvironmentID,
		ProjectID:      target.ProjectID,
		WorkspaceID:    target.WorkspaceID,
		ServerID:       target.ServerID,
		ImageRef:       imageRef,
		ContainerPort:  target.ContainerPort,
		RolledBackFrom: target.ID,
	})
}

// EnqueueFirstDeployment queues a brand new service's first deployment inside the CALLER's
// transaction (the services module, which has already authorized the create and validated
// the server). It resolves the service through the tx — so it sees the service inserted
// earlier in the same transaction — and returns the new deployment id. No authorization or
// audit here: both belong to the caller's service.create action.
func (s *service) EnqueueFirstDeployment(ctx context.Context, tx database.Tx, serviceID, serverID string) (string, error) {
	svc, ok, err := s.store.ServiceForDeployTx(ctx, tx, serviceID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", problem.NotFound("service %s not found", serviceID)
	}
	dep, err := s.buildAndInsert(ctx, tx, serviceID, svc, serverID, 0, "")
	if err != nil {
		return "", err
	}
	return dep.ID, nil
}

// buildAndInsert inserts a queued deployment (image or git) for a resolved service inside
// tx. portOverride > 0 replaces the service's configured port; gitRef overrides the branch.
// It is the shared core of CreateForService and EnqueueFirstDeployment.
func (s *service) buildAndInsert(ctx context.Context, tx database.Tx, serviceID string, svc ServiceForDeploy, serverID string, portOverride int32, gitRef string) (Deployment, error) {
	port := svc.ContainerPort
	if portOverride > 0 {
		port = portOverride
	}

	if svc.SourceKind == SourceGit {
		// Public-first: the agent only ever receives a clone URL, never a credential.
		// Building a private repo needs a short-lived credential, which lands with the
		// GitHub App. The services module gates deploy_now on this too; this is defense.
		if svc.SourceAccess != "public" {
			return Deployment{}, problem.InvalidInput("building private repositories isn't supported yet — connect a public repo (GitHub App support is coming)")
		}
		ref := strings.TrimSpace(gitRef)
		if ref == "" {
			if ref = svc.Branch; ref == "" {
				ref = svc.DefaultBranch
			}
		}
		return s.store.InsertDeploymentFromGit(ctx, tx, NewDeploymentFromGit{
			ServiceID:     serviceID,
			EnvironmentID: svc.EnvironmentID,
			ProjectID:     svc.ProjectID,
			WorkspaceID:   svc.WorkspaceID,
			ServerID:      serverID,
			ContainerPort: port,
			SourceAccess:  svc.SourceAccess,
			// Provider is GitHub-only today; construct the standard clone URL from owner/repo.
			CloneURL: "https://github.com/" + svc.Owner + "/" + svc.Repo + ".git",
			GitRef:   ref,
		})
	}

	// image or template (a template's image_ref is resolved at service-create time).
	imageRef, err := validateImageRef(svc.ImageRef)
	if err != nil {
		return Deployment{}, err
	}
	if port < 1 || port > maxPort {
		return Deployment{}, problem.InvalidInput("container_port must be between 1 and %d", maxPort)
	}
	return s.store.InsertDeployment(ctx, tx, NewDeployment{
		ServiceID:     serviceID,
		EnvironmentID: svc.EnvironmentID,
		ProjectID:     svc.ProjectID,
		WorkspaceID:   svc.WorkspaceID,
		ServerID:      serverID,
		ImageRef:      imageRef,
		ContainerPort: port,
	})
}

func (s *service) Get(ctx context.Context, deploymentID string) (Deployment, error) {
	if _, err := id.Parse(deploymentID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid deployment id is required")
	}
	dep, ok, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "get deployment")
	}
	if !ok {
		return Deployment{}, problem.NotFound("deployment %s not found", deploymentID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: dep.WorkspaceID, ID: dep.ID}); err != nil {
		return Deployment{}, err
	}
	return dep, nil
}

func (s *service) ListByService(ctx context.Context, serviceID string) ([]Deployment, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return nil, problem.InvalidInput("a valid service_id is required")
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForService(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list deployments")
	}
	if !ok {
		return nil, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByService(ctx, serviceID)
}

func (s *service) ListByEnvironment(ctx context.Context, environmentID string) ([]Deployment, error) {
	if _, err := id.Parse(environmentID); err != nil {
		return nil, problem.InvalidInput("a valid environment_id is required")
	}
	workspaceID, _, ok, err := s.store.WorkspaceAndProjectForEnvironment(ctx, environmentID)
	if err != nil {
		return nil, problem.Internalf(err, "list deployments")
	}
	if !ok {
		return nil, problem.NotFound("environment %s not found", environmentID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByEnvironment(ctx, environmentID)
}

func (s *service) ListByProject(ctx context.Context, projectID string) ([]Deployment, error) {
	if _, err := id.Parse(projectID); err != nil {
		return nil, problem.InvalidInput("a valid project_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceForProject(ctx, projectID)
	if err != nil {
		return nil, problem.Internalf(err, "list deployments")
	}
	if !ok {
		return nil, problem.NotFound("project %s not found", projectID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByProject(ctx, projectID)
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Deployment, error) {
	if workspaceID == "" {
		return nil, problem.InvalidInput("workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

func (s *service) ListEvents(ctx context.Context, deploymentID string, afterSeq int64) ([]Event, error) {
	if _, err := id.Parse(deploymentID); err != nil {
		return nil, problem.InvalidInput("a valid deployment id is required")
	}
	dep, ok, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return nil, problem.Internalf(err, "list deployment events")
	}
	if !ok {
		return nil, problem.NotFound("deployment %s not found", deploymentID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionDeploymentRead, authz.Resource{Type: "deployment", WorkspaceID: dep.WorkspaceID, ID: dep.ID}); err != nil {
		return nil, err
	}
	return s.store.ListEvents(ctx, deploymentID, afterSeq)
}

// PollDeployment claims the next queued deployment for the agent's server, if any. It
// authenticates the agent by its credential and scopes the claim to that agent's server
// (so an agent can only ever run its own server's work).
func (s *service) PollDeployment(ctx context.Context, in PollInput) (Claimed, error) {
	if in.Credential == "" {
		return Claimed{}, problem.InvalidInput("a credential is required")
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return Claimed{}, err
	}

	var claimed Deployment
	var has bool
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		dep, ok, txErr := s.store.ClaimNextForServer(ctx, tx, serverID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return nil
		}
		claimed, has = dep, true
		return s.store.AppendEvent(ctx, tx, NewEvent{DeploymentID: dep.ID, Kind: KindStatus, Status: StatusAssigned, Message: "claimed by agent"})
	})
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll deployment")
	}
	if !has {
		return Claimed{HasWork: false}, nil
	}

	isPreview := claimed.Kind == KindPreview
	// A preview runs untrusted branch/PR code, so it gets the service's non-secret variables
	// but NOT the environment's decrypted secrets.
	env, err := s.configEnvForService(ctx, claimed.ServiceID, !isPreview)
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll deployment")
	}
	// Resolve the service's slug + visibility for the route label and the network the agent
	// attaches the container to.
	svc, ok, err := s.store.ServiceForDeploy(ctx, claimed.ServiceID)
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll deployment")
	}
	if !ok {
		return Claimed{}, problem.Internalf(errors.New("service for claimed deployment not found"), "poll deployment")
	}
	// AppLabel drives the Caddy route host, the container-replacement group, and the reported
	// route_url. It is the route_key: the service id for production, a distinct key per preview,
	// so a preview gets its own URL and never replaces production's container. (Older rows
	// predating route_key fall back to the service id.)
	appLabel := claimed.RouteKey
	if appLabel == "" {
		appLabel = claimed.ServiceID
	}
	// Production joins its per-environment network (siblings reach it at its slug). A preview
	// joins its OWN isolated network so it can't reach production's siblings (e.g. the prod
	// database) — separation in depth alongside withholding secrets.
	networkName := environmentNetworkName(claimed.EnvironmentID)
	routeHost := ""
	if isPreview {
		networkName = previewNetworkName(appLabel)
		// A preview gets a human-readable public host ({slug}-pr-{n}-{hash}) instead of the
		// UUID-based route_key, while the route_key still keys replacement + teardown.
		routeHost = previewRouteHost(svc.Slug, claimed.PRNumber, claimed.GitRef, claimed.ServiceID)
	}
	s.log.Info("deployment claimed", "id", claimed.ID, "service_id", claimed.ServiceID, "kind", claimed.Kind, "route_key", appLabel, "server_id", serverID, "source_kind", claimed.SourceKind, "image", claimed.ImageRef)
	out := Claimed{
		HasWork:       true,
		DeploymentID:  claimed.ID,
		ImageRef:      claimed.ImageRef,
		ContainerPort: claimed.ContainerPort,
		Env:           env,
		AppLabel:      appLabel,
		SourceKind:    claimed.SourceKind,
		Visibility:    svc.Visibility,
		NetworkName:   networkName,
		NetworkAlias:  svc.Slug,
		// Basic-auth for a protected preview (empty for production/unprotected); the agent renders
		// it onto the preview's Caddy route. The hash is bcrypt — never the plaintext password.
		BasicAuthUser: claimed.AuthUser,
		BasicAuthHash: claimed.AuthHash,
		// The pretty public host for a preview (empty for production).
		RouteHost: routeHost,
	}
	// For a git deployment the agent clones + builds; hand it the clone URL, ref, and a
	// deterministic local tag to build to and then run. No credential (public repos only).
	if claimed.SourceKind == SourceGit {
		out.CloneURL = claimed.CloneURL
		out.GitRef = claimed.GitRef
		out.BuiltImageTag = builtImageTag(claimed.ID)
	}
	return out, nil
}

// builtImageTag is the deterministic local image tag the agent builds a git deployment to
// and then runs. It is unique per deployment and a valid Docker reference.
func builtImageTag(deploymentID string) string {
	return "plorigo-build:" + deploymentID
}

// environmentNetworkName is the per-environment Docker network that all of an environment's
// service containers join, so siblings reach each other by their service slug (network
// alias) while different environments stay isolated. The agent ensures it exists.
func environmentNetworkName(environmentID string) string {
	return "plorigo-" + environmentID
}

// previewNetworkName is the isolated Docker network a single preview deployment joins (keyed by
// its route_key), so the preview cannot reach the production environment's siblings (e.g. its
// database). The agent creates it lazily. See docs/architecture/deployment-engine.md.
func previewNetworkName(routeKey string) string {
	return "plorigo-preview-" + routeKey
}

// maxRouteKeyLen bounds a route_key to a single DNS label, since it becomes the Caddy route
// host {route_key}.{base_domain}.
const maxRouteKeyLen = 63

var routeKeyNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// previewRouteKey derives a stable, DNS-label-safe routing key for a preview. It is keyed by PR
// number when previewing a pull request (so the key is stable as the PR is pushed to), otherwise
// by a slug of the branch. The service id prefix keeps previews of different services from
// colliding; an over-long key is truncated with a short hash tail so distinct refs stay distinct.
func previewRouteKey(serviceID string, prNumber int32, ref string) string {
	suffix := slugifyRef(ref)
	if prNumber > 0 {
		suffix = "pr-" + strconv.Itoa(int(prNumber))
	}
	key := serviceID + "-" + suffix
	if len(key) <= maxRouteKeyLen {
		return key
	}
	h := sha256.Sum256([]byte(suffix))
	tail := hex.EncodeToString(h[:])[:8]
	budget := maxRouteKeyLen - len(serviceID) - len("-") - len("-") - len(tail)
	if budget < 0 {
		budget = 0
	}
	trunc := suffix
	if len(trunc) > budget {
		trunc = trunc[:budget]
	}
	trunc = strings.Trim(trunc, "-")
	return serviceID + "-" + trunc + "-" + tail
}

// hashPreviewPassword bcrypt-hashes an optional preview password for basic-auth protection. It
// returns ("", "", nil) when no password is set (an unprotected preview). The username defaults to
// "preview". Only the returned hash is ever stored or sent to the agent — never the plaintext.
// previewAuthUserRe bounds a preview basic-auth username to a safe charset, so it can never inject
// Caddyfile directives when the agent renders the basic_auth block (the bcrypt hash is inherently
// safe).
var previewAuthUserRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

func hashPreviewPassword(password, username string) (authUser, authHash string, err error) {
	if password == "" {
		return "", "", nil
	}
	user := strings.TrimSpace(username)
	if user == "" {
		user = "preview"
	}
	if !previewAuthUserRe.MatchString(user) {
		return "", "", problem.InvalidInput("the preview username must be 1-32 characters of letters, digits, '.', '_' or '-'")
	}
	h, herr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if herr != nil {
		return "", "", problem.Internalf(herr, "hash preview password")
	}
	return user, string(h), nil
}

// previewRouteHost derives a human-readable, collision-safe DNS label for a preview's PUBLIC route
// host: {slug}-pr-{n} (or {slug}-{branch-slug}) plus a short hash of the service id, so two services
// that share a slug never collide on the same host. It is bounded to one DNS label (63 chars). This
// is distinct from the route_key (which keys the container-replacement group + teardown by service
// id); only the displayed/routed host changes.
func previewRouteHost(slug string, prNumber int32, ref, serviceID string) string {
	base := slugifyRef(slug)
	if strings.TrimSpace(slug) == "" {
		base = "preview"
	}
	suffix := slugifyRef(ref)
	if prNumber > 0 {
		suffix = "pr-" + strconv.Itoa(int(prNumber))
	}
	h := sha256.Sum256([]byte(serviceID))
	tail := hex.EncodeToString(h[:])[:6]
	head := base + "-" + suffix
	// Budget the {base}-{suffix} head so the whole label (head + "-" + tail) fits a DNS label.
	if maxHead := maxRouteKeyLen - len("-") - len(tail); len(head) > maxHead {
		head = strings.Trim(head[:maxHead], "-")
	}
	return head + "-" + tail
}

// slugifyRef lowercases a git ref and reduces it to a DNS-label-safe slug ([a-z0-9-], no
// leading/trailing hyphen), defaulting to "branch" if nothing is left.
func slugifyRef(ref string) string {
	s := strings.Trim(routeKeyNonAlnum.ReplaceAllString(strings.ToLower(ref), "-"), "-")
	if s == "" {
		return "branch"
	}
	return s
}

// mapGitHubErr translates the github client's typed errors into plain-English domain errors for
// the preview PR lookup (a missing/inaccessible PR both surface as NotFound).
func mapGitHubErr(err error) error {
	switch {
	case errors.Is(err, github.ErrNotFound):
		return problem.NotFound("pull request not found, or your access can't see it")
	case errors.Is(err, github.ErrRateLimited):
		return problem.Internalf(err, "GitHub rate limit reached; please try again shortly")
	default:
		return problem.Internalf(err, "could not reach GitHub")
	}
}

// ReportDeployment records a status transition and any new runtime log lines for a
// deployment the agent is executing. It verifies the deployment belongs to the agent's
// own server before writing anything.
func (s *service) ReportDeployment(ctx context.Context, in ReportInput) error {
	if in.Credential == "" {
		return problem.InvalidInput("a credential is required")
	}
	if _, err := id.Parse(in.DeploymentID); err != nil {
		return problem.InvalidInput("a valid deployment_id is required")
	}
	if !isAgentReportableStatus(in.Status) {
		return problem.InvalidInput("status %q is not a valid agent-reported status", in.Status)
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return err
	}
	dep, ok, err := s.store.GetDeployment(ctx, in.DeploymentID)
	if err != nil {
		return problem.Internalf(err, "report deployment")
	}
	if !ok {
		return problem.NotFound("deployment %s not found", in.DeploymentID)
	}
	if dep.ServerID != serverID {
		return problem.PermissionDenied("this agent does not own deployment %s", in.DeploymentID)
	}

	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if txErr := s.store.UpdateStatus(ctx, tx, StatusUpdate{
			DeploymentID:  in.DeploymentID,
			Status:        in.Status,
			Message:       in.Message,
			HostPort:      in.HostPort,
			ContainerID:   in.ContainerID,
			CommitSha:     in.CommitSha,
			BuiltImageRef: in.BuiltImageRef,
			RouteURL:      in.RouteURL,
		}); txErr != nil {
			return txErr
		}
		if txErr := s.store.AppendEvent(ctx, tx, NewEvent{DeploymentID: in.DeploymentID, Kind: KindStatus, Status: in.Status, Message: in.Message}); txErr != nil {
			return txErr
		}
		for _, line := range in.LogLines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if txErr := s.store.AppendEvent(ctx, tx, NewEvent{DeploymentID: in.DeploymentID, Kind: KindLog, Message: line, Stream: in.LogStream}); txErr != nil {
				return txErr
			}
		}
		// Cache the public URL on the service whenever a PRODUCTION deployment reports one
		// (public services only; private services report an empty route), so the dashboard can
		// show the service's current URL without walking its history. A preview reports its own
		// route_url onto its deployment row (above) but must NOT overwrite the service's live
		// URL, which always tracks production.
		if in.RouteURL != "" && dep.Kind != KindPreview {
			if txErr := s.store.UpdateServiceRouteURL(ctx, tx, dep.ServiceID, in.RouteURL); txErr != nil {
				return txErr
			}
		}
		// Supersede the prior release with the SAME route_key only on the agent's real "now
		// running" report, which carries the bound host port. The runtime-log tail loop
		// re-reports status='running' (host port 0) just to attach new log lines — that must
		// not re-run the supersede on every tick. Keyed by route_key so a preview supersedes
		// only its own prior preview, never production (and vice versa).
		if in.Status == StatusRunning && in.HostPort > 0 {
			routeKey := dep.RouteKey
			if routeKey == "" {
				routeKey = dep.ServiceID
			}
			return s.store.SupersedePreviousRunning(ctx, tx, routeKey, serverID, in.DeploymentID)
		}
		return nil
	})
	if err != nil {
		return problem.Internalf(err, "report deployment")
	}
	s.log.Debug("deployment reported", "id", in.DeploymentID, "status", in.Status, "server_id", serverID)
	return nil
}

func (s *service) SyncRoutes(ctx context.Context, in SyncRoutesInput) ([]RouteOverride, error) {
	if in.Credential == "" {
		return nil, problem.InvalidInput("a credential is required")
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return nil, err
	}
	serviceSet := map[string]bool{}
	for _, r := range in.Routes {
		if r.HostPort <= 0 {
			continue
		}
		if _, err := id.Parse(r.ServiceID); err != nil {
			continue
		}
		if _, err := id.Parse(r.DeploymentID); err != nil {
			continue
		}
		dep, ok, err := s.store.GetDeployment(ctx, r.DeploymentID)
		if err != nil {
			return nil, problem.Internalf(err, "sync routes")
		}
		if !ok || dep.ServerID != serverID || dep.ServiceID != r.ServiceID {
			continue
		}
		serviceSet[r.ServiceID] = true
	}
	serviceIDs := make([]string, 0, len(serviceSet))
	for serviceID := range serviceSet {
		serviceIDs = append(serviceIDs, serviceID)
	}
	domainsByService, err := s.store.VerifiedDomainsForServices(ctx, serviceIDs)
	if err != nil {
		return nil, problem.Internalf(err, "sync routes")
	}
	out := make([]RouteOverride, 0, len(domainsByService))
	for _, serviceID := range serviceIDs {
		hostnames := domainsByService[serviceID]
		if len(hostnames) == 0 {
			continue
		}
		out = append(out, RouteOverride{ServiceID: serviceID, Hostnames: hostnames})
	}
	return out, nil
}

func (s *service) ReportRouteSync(ctx context.Context, in ReportRouteSyncInput) error {
	if in.Credential == "" {
		return problem.InvalidInput("a credential is required")
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		for _, r := range in.Results {
			if len(r.Hostnames) == 0 {
				continue
			}
			if _, err := id.Parse(r.ServiceID); err != nil {
				continue
			}
			if _, err := id.Parse(r.DeploymentID); err != nil {
				continue
			}
			dep, ok, txErr := s.store.GetDeployment(ctx, r.DeploymentID)
			if txErr != nil {
				return txErr
			}
			if !ok || dep.ServerID != serverID || dep.ServiceID != r.ServiceID {
				continue
			}
			status := "failed"
			message := strings.TrimSpace(r.Message)
			if r.OK {
				status = "active"
				if message == "" {
					message = "Domain is routed to this service."
				}
			} else if message == "" {
				message = "Caddy could not activate this domain. Check the deployment route logs and try again."
			}
			if txErr := s.store.MarkDomainsRouteSync(ctx, tx, r.ServiceID, r.Hostnames, status, message); txErr != nil {
				return txErr
			}
		}
		return nil
	})
	if err != nil {
		return problem.Internalf(err, "report route sync")
	}
	return nil
}

// resolveAgent validates a durable agent credential and returns the agent and its
// server. If agentID is provided it must match the credential's agent.
func (s *service) resolveAgent(ctx context.Context, agentID, credential string) (string, string, error) {
	gotAgentID, serverID, ok, err := s.store.AgentServerByCredential(ctx, hashToken(credential))
	if err != nil {
		return "", "", problem.Internalf(err, "authenticate agent")
	}
	if !ok {
		return "", "", problem.PermissionDenied("unknown agent credential")
	}
	if agentID != "" && agentID != gotAgentID {
		return "", "", problem.PermissionDenied("credential does not belong to agent %s", agentID)
	}
	return gotAgentID, serverID, nil
}

// validateImageRef trims and sanity-checks a container image reference, defaulting the
// tag to :latest when none is given. This slice pulls PUBLIC images only.
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
	// Default to :latest when the final path segment carries no tag and the ref is not
	// pinned by digest. A colon in an earlier segment (a registry host:port) is ignored.
	if !strings.Contains(ref, "@") {
		seg := ref[strings.LastIndex(ref, "/")+1:]
		if !strings.Contains(seg, ":") {
			ref += ":latest"
		}
	}
	return ref, nil
}

func isAgentReportableStatus(status string) bool {
	switch status {
	case StatusCloning, StatusBuilding, StatusPulling, StatusStarting, StatusHealthcheck, StatusRouting, StatusRunning, StatusFailed:
		return true
	}
	return false
}

// hashToken is the one-way function applied to an agent credential before lookup,
// matching how internal/agents stores and validates it.
func hashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as internal.
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

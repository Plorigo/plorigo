package deployments

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
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
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, authorizer authz.Authorizer, audit Recorder, opener Opener, log *slog.Logger) *service {
	return &service{tx: tx, store: store, authorizer: authorizer, audit: audit, opener: opener, log: log}
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
func (s *service) configEnvForService(ctx context.Context, serviceID string) (map[string]string, error) {
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

	env, err := s.configEnvForService(ctx, claimed.ServiceID)
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll deployment")
	}
	// Resolve the service's slug + visibility for the route label and the per-environment
	// network the agent attaches the container to.
	svc, ok, err := s.store.ServiceForDeploy(ctx, claimed.ServiceID)
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll deployment")
	}
	if !ok {
		return Claimed{}, problem.Internalf(errors.New("service for claimed deployment not found"), "poll deployment")
	}
	s.log.Info("deployment claimed", "id", claimed.ID, "service_id", claimed.ServiceID, "server_id", serverID, "source_kind", claimed.SourceKind, "image", claimed.ImageRef)
	out := Claimed{
		HasWork:       true,
		DeploymentID:  claimed.ID,
		ImageRef:      claimed.ImageRef,
		ContainerPort: claimed.ContainerPort,
		Env:           env,
		AppLabel:      claimed.ServiceID,
		SourceKind:    claimed.SourceKind,
		Visibility:    svc.Visibility,
		NetworkName:   environmentNetworkName(claimed.EnvironmentID),
		NetworkAlias:  svc.Slug,
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
		// Cache the public URL on the service whenever the agent reports one (public
		// services only; private services report an empty route), so the dashboard can show
		// a service's current URL without walking its deployment history.
		if in.RouteURL != "" {
			if txErr := s.store.UpdateServiceRouteURL(ctx, tx, dep.ServiceID, in.RouteURL); txErr != nil {
				return txErr
			}
		}
		// Supersede THIS service's previous release only on the agent's real "now running"
		// report, which carries the bound host port. The runtime-log tail loop re-reports
		// status='running' (host port 0) just to attach new log lines — that must not
		// re-run the supersede on every tick. Keyed by service so a sibling service in the
		// same environment is never superseded.
		if in.Status == StatusRunning && in.HostPort > 0 {
			return s.store.SupersedePreviousRunning(ctx, tx, dep.ServiceID, serverID, in.DeploymentID)
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

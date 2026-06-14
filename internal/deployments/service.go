package deployments

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

// Create records a queued deployment for an environment on a server. The server must
// belong to the environment's workspace (cross-tenant guard). The agent for that server
// claims the queued row on its next poll.
func (s *service) Create(ctx context.Context, in CreateInput) (Deployment, error) {
	if _, err := id.Parse(in.EnvironmentID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid environment_id is required")
	}
	if _, err := id.Parse(in.ServerID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid server_id is required")
	}
	imageRef, err := validateImageRef(in.ImageRef)
	if err != nil {
		return Deployment{}, err
	}
	if in.ContainerPort < 1 || in.ContainerPort > maxPort {
		return Deployment{}, problem.InvalidInput("container_port must be between 1 and %d", maxPort)
	}

	workspaceID, projectID, ok, err := s.store.WorkspaceAndProjectForEnvironment(ctx, in.EnvironmentID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment")
	}
	if !ok {
		return Deployment{}, problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentCreate, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return Deployment{}, err
	}

	// The target server must live in the same workspace as the environment. Resolving
	// after authorization (and treating another workspace's server as not-found) avoids
	// revealing servers the caller has no access to.
	serverWorkspace, ok, err := s.store.WorkspaceForServer(ctx, in.ServerID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment")
	}
	if !ok || serverWorkspace != workspaceID {
		return Deployment{}, problem.NotFound("server %s not found in this workspace", in.ServerID)
	}

	var created Deployment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		created, txErr = s.store.InsertDeployment(ctx, tx, NewDeployment{
			EnvironmentID: in.EnvironmentID,
			ProjectID:     projectID,
			WorkspaceID:   workspaceID,
			ServerID:      in.ServerID,
			ImageRef:      imageRef,
			ContainerPort: in.ContainerPort,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.create", "deployment", created.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Deployment{}, mapErr(err, "create deployment")
	}
	s.log.Info("deployment created", "id", created.ID, "environment_id", created.EnvironmentID, "server_id", created.ServerID, "image", imageRef, "workspace_id", workspaceID, "actor", caller.UserID)
	return created, nil
}

// CreateFromSource records a queued git deployment that builds the project's connected
// repository on the server. The request carries no repo URL: the service resolves the
// project's source and derives the clone URL, so a caller can't smuggle one through. This
// slice builds PUBLIC repositories only — a private/OAuth source is rejected with a plain
// message (the agent receives no credential). git_ref is optional (empty = default branch).
func (s *service) CreateFromSource(ctx context.Context, in CreateFromSourceInput) (Deployment, error) {
	if _, err := id.Parse(in.EnvironmentID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid environment_id is required")
	}
	if _, err := id.Parse(in.ServerID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid server_id is required")
	}
	// 0 means "auto-detect from the image's EXPOSE on the agent after the build".
	if in.ContainerPort < 0 || in.ContainerPort > maxPort {
		return Deployment{}, problem.InvalidInput("container_port must be between 1 and %d, or 0 to auto-detect from the Dockerfile", maxPort)
	}

	workspaceID, projectID, ok, err := s.store.WorkspaceAndProjectForEnvironment(ctx, in.EnvironmentID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment from source")
	}
	if !ok {
		return Deployment{}, problem.NotFound("environment %s not found", in.EnvironmentID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentCreate, authz.Resource{Type: "deployment", WorkspaceID: workspaceID}); err != nil {
		return Deployment{}, err
	}

	src, ok, err := s.store.SourceForProject(ctx, projectID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment from source")
	}
	if !ok {
		return Deployment{}, problem.NotFound("connect a repository to this project before deploying from source")
	}
	// Public-first: the agent only ever receives a clone URL, never a credential. Building
	// a private repo needs a short-lived credential, which lands with the GitHub App.
	if src.Access != "public" {
		return Deployment{}, problem.InvalidInput("building private repositories isn't supported yet — connect a public repo (GitHub App support is coming)")
	}

	// The target server must live in the same workspace as the environment (cross-tenant
	// guard). Resolve after authorization; another workspace's server is treated as not-found.
	serverWorkspace, ok, err := s.store.WorkspaceForServer(ctx, in.ServerID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "create deployment from source")
	}
	if !ok || serverWorkspace != workspaceID {
		return Deployment{}, problem.NotFound("server %s not found in this workspace", in.ServerID)
	}

	ref := strings.TrimSpace(in.GitRef)
	if ref == "" {
		if ref = src.Branch; ref == "" {
			ref = src.DefaultBranch
		}
	}

	var created Deployment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		created, txErr = s.store.InsertDeploymentFromGit(ctx, tx, NewDeploymentFromGit{
			EnvironmentID: in.EnvironmentID,
			ProjectID:     projectID,
			WorkspaceID:   workspaceID,
			ServerID:      in.ServerID,
			ContainerPort: in.ContainerPort,
			SourceAccess:  src.Access,
			CloneURL:      src.CloneURL,
			GitRef:        ref,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.create", "deployment", created.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Deployment{}, mapErr(err, "create deployment from source")
	}
	s.log.Info("git deployment created", "id", created.ID, "environment_id", created.EnvironmentID, "server_id", created.ServerID, "clone_url", src.CloneURL, "git_ref", ref, "workspace_id", workspaceID, "actor", caller.UserID)
	return created, nil
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

func (s *service) SetCustomDomain(ctx context.Context, deploymentID, customDomain string) (Deployment, error) {
	if _, err := id.Parse(deploymentID); err != nil {
		return Deployment{}, problem.InvalidInput("a valid deployment id is required")
	}
	dep, ok, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return Deployment{}, problem.Internalf(err, "set custom domain")
	}
	if !ok {
		return Deployment{}, problem.NotFound("deployment %s not found", deploymentID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionDeploymentUpdate, authz.Resource{Type: "deployment", WorkspaceID: dep.WorkspaceID, ID: dep.ID}); err != nil {
		return Deployment{}, err
	}
	domain := strings.TrimSpace(customDomain)
	if domain != "" {
		if err := validateCustomDomain(domain); err != nil {
			return Deployment{}, problem.InvalidInput("custom domain: %v", err)
		}
	}
	var updated Deployment
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		updated, txErr = s.store.SetCustomDomain(ctx, tx, deploymentID, domain)
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "deployment.update_domain", "deployment", deploymentID, dep.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return Deployment{}, mapErr(err, "set custom domain")
	}
	s.log.Info("custom domain set", "id", deploymentID, "domain", domain, "workspace_id", dep.WorkspaceID, "actor", caller.UserID)
	return updated, nil
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

	env, err := s.store.EnvVarsForEnvironment(ctx, claimed.EnvironmentID)
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll deployment")
	}
	s.log.Info("deployment claimed", "id", claimed.ID, "server_id", serverID, "source_kind", claimed.SourceKind, "image", claimed.ImageRef)
	out := Claimed{
		HasWork:       true,
		DeploymentID:  claimed.ID,
		ImageRef:      claimed.ImageRef,
		ContainerPort: claimed.ContainerPort,
		Env:           env,
		AppLabel:      claimed.EnvironmentID,
		SourceKind:    claimed.SourceKind,
		CustomDomain:  claimed.CustomDomain,
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
		// Supersede the previous release only on the agent's real "now running" report,
		// which carries the bound host port. The runtime-log tail loop re-reports
		// status='running' (host port 0) just to attach new log lines — that must not
		// re-run the supersede on every tick.
		if in.Status == StatusRunning && in.HostPort > 0 {
			return s.store.SupersedePreviousRunning(ctx, tx, dep.EnvironmentID, serverID, in.DeploymentID)
		}
		return nil
	})
	if err != nil {
		return problem.Internalf(err, "report deployment")
	}
	s.log.Debug("deployment reported", "id", in.DeploymentID, "status", in.Status, "server_id", serverID)
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

// validateCustomDomain validates a custom domain string. Empty is allowed (clears the
// domain). Non-empty must be a valid DNS name with no scheme, port, or path.
func validateCustomDomain(domain string) error {
	if domain == "" {
		return nil
	}
	if len(domain) > 253 {
		return fmt.Errorf("too long (max 253 characters)")
	}
	if strings.Contains(domain, "://") {
		return fmt.Errorf("must not include a scheme")
	}
	if strings.Contains(domain, "/") {
		return fmt.Errorf("must not include a path")
	}
	if strings.Contains(domain, ":") {
		return fmt.Errorf("must not include a port")
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("must not contain empty labels (consecutive dots)")
		}
		if len(label) > 63 {
			return fmt.Errorf("label %q is too long (max 63)", label)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("label %q must not start or end with '-'", label)
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || (r >= 'A' && r <= 'Z') {
				continue
			}
			return fmt.Errorf("label %q contains invalid character %q", label, r)
		}
	}
	return nil
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
	case StatusCloning, StatusBuilding, StatusPulling, StatusStarting, StatusRouting, StatusRunning, StatusFailed:
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

package projects

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// service is the business logic. It orchestrates ports only — no SQL, no transport.
// Every mutation authorizes the caller (via the authz.Authorizer port) before the
// WithinTx block, and audits inside it (see docs/architecture/modules.md, Rule 4).
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

func (s *service) Create(ctx context.Context, in CreateInput) (Project, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Project{}, problem.InvalidInput("project name is required")
	}
	if in.WorkspaceID == "" {
		return Project{}, problem.InvalidInput("workspace_id is required")
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionProjectCreate, authz.Resource{Type: "project", WorkspaceID: in.WorkspaceID}); err != nil {
		return Project{}, err
	}

	slug := slugify(name)
	if slug == "" {
		return Project{}, problem.InvalidInput("project name must contain at least one letter or number")
	}
	candidate := Project{WorkspaceID: in.WorkspaceID, Name: name, Slug: slug}

	var created Project
	err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		if created, txErr = s.store.InsertProject(ctx, tx, candidate); txErr != nil {
			return txErr
		}
		// The audit record commits in the SAME transaction as the project row, with
		// the real authenticated actor.
		return s.audit.Record(ctx, tx, "project.create", "project", created.ID, created.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return Project{}, mapErr(err, "create project")
	}
	s.log.Info("project created", "id", created.ID, "workspace_id", created.WorkspaceID, "actor", caller.UserID)
	return created, nil
}

func (s *service) Get(ctx context.Context, projectID string) (Project, error) {
	if _, err := id.Parse(projectID); err != nil {
		return Project{}, problem.InvalidInput("invalid project id")
	}
	proj, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionProjectRead, authz.Resource{Type: "project", WorkspaceID: proj.WorkspaceID, ID: proj.ID}); err != nil {
		return Project{}, err
	}
	return proj, nil
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Project, error) {
	if workspaceID == "" {
		return nil, problem.InvalidInput("workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionProjectRead, authz.Resource{Type: "project", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

func (s *service) CreateWorkspace(ctx context.Context, in CreateWorkspaceInput) (Workspace, error) {
	caller := principal.FromContext(ctx)
	if !caller.IsAuthenticated() {
		return Workspace{}, problem.PermissionDenied("authentication required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return Workspace{}, problem.InvalidInput("workspace name is required")
	}
	var ws Workspace
	err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		ws, txErr = s.createWorkspaceTx(ctx, tx, caller.UserID, in.Name, caller.UserID)
		return txErr
	})
	if err != nil {
		return Workspace{}, mapErr(err, "create workspace")
	}
	return ws, nil
}

func (s *service) CreateInitialWorkspace(ctx context.Context, tx database.Tx, userID, name, actor string) (string, error) {
	ws, err := s.createWorkspaceTx(ctx, tx, userID, name, actor)
	if err != nil {
		return "", err
	}
	return ws.ID, nil
}

// createWorkspaceTx creates a workspace, makes ownerID its owner, and audits it —
// all inside tx. Authorization is the caller's responsibility: CreateWorkspace
// requires an authenticated caller, and registration's bootstrap is self-authorizing
// (a new user becoming the owner of their own brand-new workspace).
func (s *service) createWorkspaceTx(ctx context.Context, tx database.Tx, ownerID, name, actor string) (Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Workspace{}, problem.InvalidInput("workspace name is required")
	}
	ws, err := s.store.InsertWorkspace(ctx, tx, name, workspaceSlug(name))
	if err != nil {
		return Workspace{}, err
	}
	if err := s.store.AddMember(ctx, tx, ws.ID, ownerID, authz.RoleOwner); err != nil {
		return Workspace{}, err
	}
	if err := s.audit.Record(ctx, tx, "workspace.create", "workspace", ws.ID, ws.ID, actor); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

func (s *service) ListMyWorkspaces(ctx context.Context, userID string) ([]Workspace, error) {
	return s.store.ListWorkspacesForUser(ctx, userID)
}

func (s *service) InviteMember(ctx context.Context, in InviteMemberInput) error {
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionMemberInvite, authz.Resource{Type: "workspace", WorkspaceID: in.WorkspaceID}); err != nil {
		return err
	}
	role := strings.TrimSpace(in.Role)
	// Owners are granted via ChangeMemberRole (owner-only), never via invite, so an
	// admin cannot mint an owner.
	if role != authz.RoleAdmin && role != authz.RoleMember && role != authz.RoleViewer {
		return problem.InvalidInput("role must be one of admin, member, viewer")
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	userID, ok, err := s.store.UserIDByEmail(ctx, email)
	if err != nil {
		return problem.Internalf(err, "invite member")
	}
	if !ok {
		return problem.NotFound("no Plorigo account exists for %s; ask them to sign up first", email)
	}
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.AddMember(ctx, tx, in.WorkspaceID, userID, role); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "workspace.member.invite", "membership", userID, in.WorkspaceID, caller.UserID)
	}), "invite member")
}

func (s *service) ListMembers(ctx context.Context, in ListMembersInput) ([]Member, error) {
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionMemberList, authz.Resource{Type: "workspace", WorkspaceID: in.WorkspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, in.WorkspaceID)
}

func (s *service) ChangeMemberRole(ctx context.Context, in ChangeRoleInput) error {
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionMemberRoleChange, authz.Resource{Type: "workspace", WorkspaceID: in.WorkspaceID}); err != nil {
		return err
	}
	if !authz.ValidRole(in.Role) {
		return problem.InvalidInput("invalid role")
	}
	// Verify the target is actually a member: UpdateMemberRole is an UPDATE that matches
	// zero rows for a non-member, which Postgres reports as success — so without this check
	// promoting a non-member would silently no-op yet still audit a change.
	role, ok, err := s.store.MemberRole(ctx, in.WorkspaceID, in.UserID)
	if err != nil {
		return problem.Internalf(err, "change member role")
	}
	if !ok {
		return problem.NotFound("that user is not a member of this workspace")
	}
	// Don't demote the current owner — that risks locking everyone out. Ownership is
	// added, not transferred, in this slice.
	if role == authz.RoleOwner && in.Role != authz.RoleOwner {
		return problem.PermissionDenied("an owner cannot be demoted; promote another owner first")
	}
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.UpdateMemberRole(ctx, tx, in.WorkspaceID, in.UserID, in.Role); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "workspace.member.role.change", "membership", in.UserID, in.WorkspaceID, caller.UserID)
	}), "change member role")
}

func (s *service) RemoveMember(ctx context.Context, in RemoveMemberInput) error {
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionMemberRemove, authz.Resource{Type: "workspace", WorkspaceID: in.WorkspaceID}); err != nil {
		return err
	}
	role, ok, err := s.store.MemberRole(ctx, in.WorkspaceID, in.UserID)
	if err != nil {
		return problem.Internalf(err, "remove member")
	}
	if !ok {
		return problem.NotFound("that user is not a member of this workspace")
	}
	if role == authz.RoleOwner {
		return problem.PermissionDenied("an owner cannot be removed; promote another owner first")
	}
	return mapErr(s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if e := s.store.RemoveMember(ctx, tx, in.WorkspaceID, in.UserID); e != nil {
			return e
		}
		return s.audit.Record(ctx, tx, "workspace.member.remove", "membership", in.UserID, in.WorkspaceID, caller.UserID)
	}), "remove member")
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// workspaceSlug derives a globally-unique slug from name by appending a short
// random suffix — workspace slugs are unique across the whole platform.
func workspaceSlug(name string) string {
	base := slugify(name)
	if base == "" {
		base = "workspace"
	}
	return base + "-" + id.New().String()[:8]
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an
// internal error, so a unique violation surfaces as AlreadyExists rather than being
// masked as Internal.
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

package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

type fakeMembers struct {
	role string
	ok   bool
	err  error
}

func (f fakeMembers) RoleForUser(context.Context, string, string) (string, bool, error) {
	return f.role, f.ok, f.err
}

func authed() principal.Principal {
	return principal.Principal{UserID: "u1", Method: principal.MethodSession}
}

func TestAuthorizeMatrix(t *testing.T) {
	cases := []struct {
		role    string
		action  authz.Action
		allowed bool
	}{
		{authz.RoleOwner, authz.ActionMemberRoleChange, true},
		{authz.RoleAdmin, authz.ActionMemberRoleChange, false}, // only owners change roles
		{authz.RoleAdmin, authz.ActionProjectCreate, true},
		{authz.RoleMember, authz.ActionProjectCreate, true},
		{authz.RoleMember, authz.ActionMemberInvite, false},
		{authz.RoleViewer, authz.ActionProjectRead, true},
		{authz.RoleViewer, authz.ActionProjectCreate, false},
	}
	for _, c := range cases {
		svc := New(Deps{Membership: fakeMembers{role: c.role, ok: true}})
		err := svc.Authorize(context.Background(), authed(), c.action,
			authz.Resource{Type: "workspace", WorkspaceID: "w1"})
		switch {
		case c.allowed && err != nil:
			t.Errorf("role %s action %s: got %v, want allowed", c.role, c.action, err)
		case !c.allowed && !isDenied(err):
			t.Errorf("role %s action %s: got %v, want PermissionDenied", c.role, c.action, err)
		}
	}
}

func TestAuthorizeDeniesNonMember(t *testing.T) {
	svc := New(Deps{Membership: fakeMembers{ok: false}})
	err := svc.Authorize(context.Background(), authed(), authz.ActionProjectRead,
		authz.Resource{WorkspaceID: "w1"})
	if !isDenied(err) {
		t.Fatalf("non-member: got %v, want PermissionDenied", err)
	}
}

func TestAuthorizeDeniesAnonymous(t *testing.T) {
	svc := New(Deps{Membership: fakeMembers{role: authz.RoleOwner, ok: true}})
	err := svc.Authorize(context.Background(), principal.Principal{}, authz.ActionProjectRead,
		authz.Resource{WorkspaceID: "w1"})
	if !isDenied(err) {
		t.Fatalf("anonymous: got %v, want PermissionDenied", err)
	}
}

func isDenied(err error) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == problem.KindPermissionDenied
}

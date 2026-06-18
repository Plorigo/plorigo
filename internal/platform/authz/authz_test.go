package authz

import "testing"

// Action strings may appear in audit records and future persisted policy, so
// their values are part of the contract — guard against accidental renames.
func TestActionValues(t *testing.T) {
	cases := map[Action]string{
		ActionWorkspaceCreate:  "workspace.create",
		ActionMemberInvite:     "workspace.member.invite",
		ActionMemberRemove:     "workspace.member.remove",
		ActionMemberRoleChange: "workspace.member.role.change",
		ActionMemberList:       "workspace.member.list",
		ActionProjectCreate:    "project.create",
		ActionProjectRead:      "project.read",
		ActionProjectDelete:    "project.delete",
		ActionConfigSet:        "config.set",
		ActionConfigRead:       "config.read",
		ActionConfigDelete:     "config.delete",
	}
	for a, want := range cases {
		if string(a) != want {
			t.Errorf("action = %q, want %q", string(a), want)
		}
	}
}

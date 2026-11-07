package iam

import "testing"

func principalWith(role Role, scope Scope) *Principal {
	return &Principal{User: User{ID: "u", Username: "u", Status: StatusActive}, Assignments: []Assignment{{Role: role, Scope: scope}}}
}

func TestDefaultDeny(t *testing.T) {
	none := &Principal{User: User{Status: StatusActive}}
	if Authorize(none, ActionUsersRead, Global) || Authorize(nil, ActionUsersRead, Global) {
		t.Fatal("a principal without assignments must be denied everything")
	}
	if Visible(none, Scope{Kind: ScopeProject, ID: "a"}) {
		t.Fatal("no assignments means nothing is visible")
	}
}

func TestOwnerAllowsEverything(t *testing.T) {
	o := principalWith(RoleOwner, Global)
	for _, a := range []Action{ActionUsersWrite, ActionRolesAssign, ActionSecretsExport, ActionOwnershipTransfer, Action("future:thing")} {
		if !Authorize(o, a, Scope{Kind: ScopeProject, ID: "x"}) {
			t.Fatalf("owner must be allowed %s", a)
		}
	}
}

func TestRoleTableMatchesSpec51(t *testing.T) {
	cases := []struct {
		role  Role
		allow []Action
		deny  []Action
	}{
		{RolePlatformAdmin, []Action{ActionNodesWrite, ActionPlatformWrite, ActionAppsWrite}, []Action{ActionSecretsExport, ActionOwnershipTransfer, ActionUsersWrite, ActionRolesAssign}},
		{RoleHostOperator, []Action{ActionHostWrite}, []Action{ActionProjectsDeploy, ActionUsersRead}},
		{RoleProjectAdmin, []Action{ActionProjectsDeploy, ActionProjectsWrite}, []Action{ActionHostWrite, ActionTerminalUse}},
		{RoleDBOperator, []Action{ActionDatabasesWrite}, []Action{ActionHostWrite, ActionUsersWrite}},
		{RoleAuditor, []Action{ActionAuditRead, ActionProjectsRead, ActionHostRead}, []Action{ActionProjectsDeploy, ActionTerminalUse, ActionSecretsRead, ActionHostWrite, ActionUsersWrite}},
	}
	for _, tc := range cases {
		p := principalWith(tc.role, Global)
		for _, a := range tc.allow {
			if !Authorize(p, a, Global) {
				t.Errorf("%s must allow %s", tc.role, a)
			}
		}
		for _, a := range tc.deny {
			if Authorize(p, a, Global) {
				t.Errorf("%s must deny %s", tc.role, a)
			}
		}
	}
}

func TestScopeIsolation(t *testing.T) {
	a := Scope{Kind: ScopeProject, ID: "A"}
	b := Scope{Kind: ScopeProject, ID: "B"}
	p := principalWith(RoleProjectAdmin, a)
	if !Authorize(p, ActionProjectsDeploy, a) {
		t.Fatal("project admin of A must deploy A")
	}
	if Authorize(p, ActionProjectsRead, b) || Visible(p, b) {
		t.Fatal("project admin of A must not even see B")
	}
	if Visible(p, Scope{Kind: ScopeNode, ID: "A"}) {
		t.Fatal("same id in another kind is a different scope")
	}
	global, scopes := ScopesFor(p, ActionProjectsRead)
	if global || len(scopes) != 1 || scopes[0] != a {
		t.Fatalf("ScopesFor = %v %v", global, scopes)
	}
}

func TestDisabledUserIsDenied(t *testing.T) {
	p := principalWith(RoleOwner, Global)
	p.User.Status = StatusDisabled
	if Authorize(p, ActionUsersRead, Global) {
		t.Fatal("disabled users keep no permissions")
	}
}

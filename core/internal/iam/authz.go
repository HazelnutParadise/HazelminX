package iam

import "strings"

// grants is the 5.1 role table. Owner is handled as a wildcard. Every action
// absent from a role's list is denied; there is no "allow everything else".
var grants = map[Role][]Action{
	RolePlatformAdmin: {
		ActionUsersRead, ActionAuditRead,
		ActionNodesRead, ActionNodesWrite,
		ActionPlatformRead, ActionPlatformWrite,
		ActionAppsRead, ActionAppsWrite,
		ActionHostRead, ActionHostWrite,
		ActionProjectsRead, ActionProjectsWrite, ActionProjectsDeploy,
		ActionDatabasesRead, ActionDatabasesWrite,
		ActionTerminalUse,
	},
	RoleHostOperator: {
		ActionNodesRead,
		ActionHostRead, ActionHostWrite,
		ActionTerminalUse,
	},
	RoleProjectAdmin: {
		ActionProjectsRead, ActionProjectsWrite, ActionProjectsDeploy,
	},
	RoleDBOperator: {
		ActionDatabasesRead, ActionDatabasesWrite,
	},
	RoleAuditor: {
		ActionUsersRead, ActionAuditRead,
		ActionNodesRead, ActionPlatformRead, ActionAppsRead,
		ActionHostRead, ActionProjectsRead, ActionDatabasesRead,
	},
}

// Grants returns the actions a role allows (nil for owner, which allows all).
func Grants(r Role) []Action { return grants[r] }

// ValidRole reports whether r is one of the assignable presets.
func ValidRole(r Role) bool {
	for _, known := range Roles {
		if known == r {
			return true
		}
	}
	return false
}

// roleAllows reports whether role r grants action a (ignoring scope).
func roleAllows(r Role, a Action) bool {
	if r == RoleOwner {
		return true
	}
	for _, g := range grants[r] {
		if g == a {
			return true
		}
	}
	return false
}

// Authorize is the single permission decision: true only when at least one
// assignment both grants the action and covers the resource scope.
func Authorize(p *Principal, a Action, on Scope) bool {
	if p == nil || p.User.Status != StatusActive {
		return false
	}
	for _, as := range p.Assignments {
		if as.Scope.Covers(on) && roleAllows(as.Role, a) {
			return true
		}
	}
	return false
}

// Visible reports whether the principal may learn that a resource in scope
// exists: any read action on that scope kind. Handlers answer 404 when this
// is false, whatever the actual existence of the resource (T-014).
func Visible(p *Principal, on Scope) bool {
	for _, a := range readActions(on.Kind) {
		if Authorize(p, a, on) {
			return true
		}
	}
	return false
}

func readActions(k ScopeKind) []Action {
	switch k {
	case ScopeNode:
		return []Action{ActionNodesRead, ActionHostRead}
	case ScopeProject:
		return []Action{ActionProjectsRead}
	case ScopeDatabase:
		return []Action{ActionDatabasesRead}
	default:
		return []Action{ActionPlatformRead, ActionUsersRead, ActionAuditRead}
	}
}

// ScopesFor returns the scopes where the principal holds action a, with
// Global meaning "everywhere". It drives list filters so searches never
// include rows the caller could not open one by one.
func ScopesFor(p *Principal, a Action) (global bool, scopes []Scope) {
	if p == nil || p.User.Status != StatusActive {
		return false, nil
	}
	for _, as := range p.Assignments {
		if !roleAllows(as.Role, a) {
			continue
		}
		if as.Scope.Kind == ScopeGlobal {
			return true, nil
		}
		scopes = append(scopes, as.Scope)
	}
	return false, scopes
}

// ParseAction validates the "<area>:<verb>" shape without enumerating.
func ParseAction(s string) (Action, bool) {
	area, verb, ok := strings.Cut(s, ":")
	if !ok || area == "" || verb == "" {
		return "", false
	}
	return Action(s), true
}

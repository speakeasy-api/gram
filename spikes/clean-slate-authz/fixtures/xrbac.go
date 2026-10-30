package fixtures

import (
	"fmt"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

// XWorld is the same organization as Relationships(), expressed in Option B's
// shapes: principal_grants rows with selectors, role assignments, directory
// groups mapped to roles, and domain rows for agents, keys and chats.
func XWorld() xrbac.World {
	ends := TemporaryAccessEnds
	sel := func(kv ...string) map[string]string {
		m := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	grants := []xrbac.StoredGrant{
		{Principal: "role:role_member", Scope: xrbac.ScopeAgentCreate, Selectors: sel("resource_kind", "org", "resource_id", Org)},
		{Principal: "role:role_member", Scope: xrbac.ScopeMCPRead, Selectors: sel("resource_kind", "mcp", "resource_id", "*", "project_id", Project)},
		{Principal: "role:role_eng", Scope: xrbac.ScopeMCPConnect, Selectors: sel("resource_kind", "mcp", "resource_id", Server)},
		{Principal: "role:role_eng", Scope: xrbac.ScopeMCPBlockedConn, Selectors: sel("resource_kind", "mcp", "resource_id", Server, "disposition", "destructive")},
		{Principal: "user:dave", Scope: xrbac.ScopeMCPWrite, Selectors: sel("resource_kind", "mcp", "resource_id", "*", "project_id", Project)},
		// Team review: the grant's principal is the reviewer; subject_principal is whose activity.
		{Principal: "user:alice", Scope: xrbac.ScopeChatRead, Selectors: sel("resource_kind", "chat", "resource_id", "*", "project_id", Project, "subject_principal", "role:role_eng")},
		// Temporary access: the new validity column. "*" matches Option A's
		// organization-wide protected review; B could also name one person.
		{Principal: "user:erin", Scope: xrbac.ScopeChatRead, Selectors: sel("resource_kind", "chat", "resource_id", "*", "subject_principal", "*"), ValidUntil: &ends},
		// Org-wide review, with protection expressed as a reviewer-side block.
		{Principal: "user:frank", Scope: xrbac.ScopeChatRead, Selectors: sel("resource_kind", "chat", "resource_id", "*", "subject_principal", "*")},
		{Principal: "user:frank", Scope: xrbac.ScopeChatBlockedRead, Selectors: sel("resource_kind", "chat", "resource_id", "*", "subject_principal", "user:carol")},
		// Alice's agent: three tools, plus the owner's denial carried by containment.
		{Principal: "agent:" + AgentID, Scope: xrbac.ScopeMCPConnect, Selectors: sel("resource_kind", "mcp", "resource_id", Server, "tool", "list_issues")},
		{Principal: "agent:" + AgentID, Scope: xrbac.ScopeMCPConnect, Selectors: sel("resource_kind", "mcp", "resource_id", Server, "tool", "get_issue")},
		{Principal: "agent:" + AgentID, Scope: xrbac.ScopeMCPConnect, Selectors: sel("resource_kind", "mcp", "resource_id", Server, "tool", "update_issue")},
		{Principal: "agent:" + AgentID, Scope: xrbac.ScopeMCPBlockedConn, Selectors: sel("resource_kind", "mcp", "resource_id", Server, "disposition", "destructive")},
	}
	for i := range grants {
		grants[i].ID = fmt.Sprintf("g%02d", i+1)
	}
	everyone := map[string][]string{}
	for _, u := range []string{"alice", "bob", "carol", "dave", "erin", "frank"} {
		everyone[u] = []string{"role:role_member"}
	}
	chats := map[string]xrbac.ChatFacts{}
	for _, c := range Chats {
		chats[c.ID] = xrbac.ChatFacts{ID: c.ID, ProjectID: Project, UserID: c.AuthorUserID, AgentID: c.AgentID}
	}
	return xrbac.World{
		Grants:              grants,
		RoleAssignments:     everyone,
		DirectoryMembership: map[string][]string{"alice": {"grp_eng"}, "bob": {"grp_eng"}},
		DirectoryRoleMaps:   map[string][]string{"grp_eng": {"role:role_eng"}},
		Agents:              map[string]xrbac.AgentFacts{AgentID: {ID: AgentID, ProjectID: Project, OwnerUserID: "alice", Status: "active"}},
		Credentials: map[string]xrbac.CredentialFacts{KeyID: {ID: KeyID, AgentID: AgentID, Policy: []xrbac.Rule{
			{Scope: xrbac.ScopeMCPConnect, Selector: sel("resource_kind", "mcp", "resource_id", Server), Source: "credential:" + KeyID},
		}}},
		Chats: chats,
	}
}

// X targets: the loaded resources with their actual ancestors.
func XRef(kind, id string) xrbac.Ref { return xrbac.Ref{Org: Org, Kind: kind, ID: id} }

var (
	XOrgTarget    = xrbac.Target{Resource: XRef("org", Org)}
	XServerTarget = xrbac.Target{Resource: XRef("mcp", Server), Ancestors: []xrbac.Ref{XRef("project", Project), XRef("org", Org)}}
	XAgentTarget  = xrbac.Target{Resource: XRef("agent", AgentID), Ancestors: []xrbac.Ref{XRef("project", Project), XRef("org", Org)}}
)

func XChatTarget(id string) xrbac.Target {
	return xrbac.Target{Resource: XRef("chat", id), Ancestors: []xrbac.Ref{XRef("project", Project), XRef("org", Org)}}
}

// DWorld is Option D's data: Option B's tables and rows, with two new row
// conventions. Protection is one marker row on the person (replacing Frank's
// reviewer-side block), and protected review is its own stored scope.
func DWorld() xrbac.World {
	w := XWorld()
	var grants []xrbac.StoredGrant
	for _, g := range w.Grants {
		switch {
		case g.Principal == "user:frank" && g.Scope == xrbac.ScopeChatBlockedRead:
			continue
		case g.Principal == "user:erin":
			g.Scope = "chat:read_protected"
		}
		grants = append(grants, g)
	}
	w.Grants = append(grants, xrbac.StoredGrant{
		ID: "g99", Principal: "organization:" + Org, Scope: "user:protected",
		Selectors: map[string]string{"resource_kind": "user", "resource_id": "carol"},
	})
	return w
}

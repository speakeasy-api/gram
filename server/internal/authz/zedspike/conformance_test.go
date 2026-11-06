package zedspike

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/authzed/spicedb/pkg/development"
	core "github.com/authzed/spicedb/pkg/proto/core/v1"
	devinterface "github.com/authzed/spicedb/pkg/proto/developer/v1"
	v1dispatch "github.com/authzed/spicedb/pkg/proto/dispatch/v1"
	"github.com/authzed/spicedb/pkg/tuple"
	"github.com/stretchr/testify/require"
)

// spicedbEvaluator answers checks with SpiceDB's own evaluator: it lands the
// policy's rows exactly as production would, adds the check's edges and facts
// as relationships, and runs a development-mode check against rbac.zed. It is
// the reference the in-process RegistryEvaluator must agree with.
type spicedbEvaluator struct{ registry *Registry }

func (e spicedbEvaluator) Check(ctx context.Context, organizationID string, policy Policy, check Check) (Permissionship, error) {
	rels, err := e.relationships(organizationID, policy, check)
	if err != nil {
		return NoPermission, err
	}
	devCtx, devErrs, err := development.NewDevContext(ctx, &devinterface.RequestContext{Schema: SchemaSource(), Relationships: rels})
	if err != nil {
		return NoPermission, err
	}
	if devErrs != nil {
		return NoPermission, fmt.Errorf("spicedb rejected relationships: %v", devErrs.GetInputErrors())
	}
	defer devCtx.Dispose()

	result, err := development.RunCheck(devCtx,
		tuple.ONR(check.resourceType, check.resourceID, check.permission),
		tuple.ONR(policy.Subject.Type, policy.Subject.ID, tuple.Ellipsis),
		check.context,
	)
	if err != nil {
		return NoPermission, err
	}
	switch result.Permissionship {
	case v1dispatch.ResourceCheckResult_MEMBER:
		return HasPermission, nil
	case v1dispatch.ResourceCheckResult_CAVEATED_MEMBER:
		return ConditionalPermission, nil
	default:
		return NoPermission, nil
	}
}

func (e spicedbEvaluator) LookupResources(context.Context, string, Policy, Lookup) (Predicate, error) {
	return Predicate{}, ErrNotImplemented
}

func (e spicedbEvaluator) relationships(organizationID string, policy Policy, check Check) ([]*core.RelationTuple, error) {
	var rels []Relationship
	for _, row := range policy.Grants {
		landed, err := e.registry.Land(organizationID, row)
		if err != nil {
			return nil, err
		}
		rels = append(rels, landed...)
	}
	if policy.Subject.Type == "user" {
		for _, role := range policy.Roles {
			membership, err := Membership(role, policy.Subject.ID)
			if err != nil {
				return nil, err
			}
			rels = append(rels, membership)
		}
	}
	rels = append(rels, edgeRelationships(e.registry, organizationID, check)...)
	rels = append(rels, check.facts...)

	seen := map[string]bool{}
	out := make([]*core.RelationTuple, 0, len(rels))
	for _, rel := range rels {
		key := fmt.Sprintf("%s%v", rel, rel.CaveatContext)
		if seen[key] {
			continue
		}
		seen[key] = true
		t := tuple.Relationship{RelationshipReference: tuple.RelationshipReference{
			Resource: tuple.ONR(rel.ResourceType, rel.ResourceID, rel.Relation),
			Subject:  tuple.ONR(rel.SubjectType, rel.SubjectID, subjectRelation(rel)),
		}}
		if rel.Caveat != "" {
			var err error
			t, err = tuple.WithCaveat(t, rel.Caveat, rel.CaveatContext)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, t.ToCoreTuple())
	}
	return out, nil
}

func subjectRelation(rel Relationship) string {
	if rel.SubjectRelation != "" {
		return rel.SubjectRelation
	}
	return tuple.Ellipsis
}

// edgeRelationships are the edges a check carries, written as the
// relationships SpiceDB would store. In process they are never written
// anywhere: the evaluator reads them off the reference.
func edgeRelationships(reg *Registry, organizationID string, check Check) []Relationship {
	var out []Relationship
	edge := func(resourceType, resourceID, relation, targetType, targetID string) {
		out = append(out, Relationship{
			ResourceType: resourceType, ResourceID: resourceID, Relation: relation,
			SubjectType: targetType, SubjectID: targetID,
		})
	}
	if reg.hasRelation(check.resourceType, "organization") {
		edge(check.resourceType, check.resourceID, "organization", "organization", organizationID)
	}
	if check.projectID != "" {
		edge(check.resourceType, check.resourceID, "project", "project", check.projectID)
		edge("project", check.projectID, "organization", "organization", organizationID)
	}
	return out
}

const (
	org        = "org1"
	projectP   = "projP"
	projectQ   = "projQ"
	memberRole = "role:organization:members"
	adminRole  = "role:organization:admins"
)

func row(principal string, scope Scope, selector map[string]string) GrantRow {
	return GrantRow{PrincipalURN: principal, Scope: scope, Selector: selector}
}

func mcpRow(resourceID, projectID string, extra ...string) map[string]string {
	selector := map[string]string{"resource_kind": "mcp", "resource_id": resourceID}
	if projectID != "" {
		selector["project_id"] = projectID
	}
	for i := 0; i+1 < len(extra); i += 2 {
		selector[extra[i]] = extra[i+1]
	}
	return selector
}

func user(id string, roles []string, grants ...GrantRow) Policy {
	return Policy{Subject: Subject{Type: "user", ID: id}, Roles: roles, Grants: grants}
}

type conformanceCase struct {
	name     string
	policies []Policy // every one must allow
	check    Check
	want     bool
}

// conformanceCases are today's behaviours, one per hard case. Each is
// answered by SpiceDB's evaluator over rbac.zed; the in-process evaluator
// must give the same answers once it exists.
func conformanceCases() []conformanceCase {
	members := []string{memberRole}
	memberConnectsAll := row(memberRole, ScopeMCPConnect, mcpRow("*", projectP))
	memberBlockedS := row(memberRole, ScopeMCPBlockedConnect, mcpRow("S", projectP))
	danaGrantS := row("user:dana", ScopeMCPConnect, mcpRow("S", projectP))

	inferred := danaGrantS
	inferred.Inferred = true

	return []conformanceCase{
		{
			name:     "org admin administers",
			policies: []Policy{user("ann", []string{adminRole}, row(adminRole, ScopeOrgAdmin, map[string]string{"resource_kind": "org", "resource_id": org}))},
			check:    Organization(org).Administer(), want: true,
		},
		{
			name:     "org admin reads through admin",
			policies: []Policy{user("ann", []string{adminRole}, row(adminRole, ScopeOrgAdmin, map[string]string{"resource_kind": "org", "resource_id": org}))},
			check:    Organization(org).Read(), want: true,
		},
		{
			name:     "member does not administer",
			policies: []Policy{user("bob", members, row(memberRole, ScopeOrgRead, map[string]string{"resource_kind": "org", "resource_id": org}))},
			check:    Organization(org).Administer(), want: false,
		},
		{
			name:     "org read block withdraws administer",
			policies: []Policy{user("ann", []string{adminRole}, row(adminRole, ScopeOrgAdmin, map[string]string{"resource_kind": "org", "resource_id": org}), row(adminRole, ScopeOrgBlockedRead, map[string]string{"resource_kind": "org", "resource_id": org}))},
			check:    Organization(org).Administer(), want: false,
		},

		// tool caveats
		{
			name:     "tool-limited grant opens the server",
			policies: []Policy{user("bob", members, row(memberRole, ScopeMCPConnect, mcpRow("*", projectP, "tool", "search")))},
			check:    MCPServer(projectP, "S").Connect(), want: true,
		},
		{
			name:     "tool-limited grant calls its tool",
			policies: []Policy{user("bob", members, row(memberRole, ScopeMCPConnect, mcpRow("*", projectP, "tool", "search")))},
			check:    MCPServer(projectP, "S").CallTool("search", ""), want: true,
		},
		{
			name:     "tool-limited grant does not call another tool",
			policies: []Policy{user("bob", members, row(memberRole, ScopeMCPConnect, mcpRow("*", projectP, "tool", "search")))},
			check:    MCPServer(projectP, "S").CallTool("delete_all", ""), want: false,
		},
		{
			name:     "tool block does not stop opening the server",
			policies: []Policy{user("bob", members, memberConnectsAll, row(memberRole, ScopeMCPBlockedConnect, mcpRow("S", projectP, "tool", "delete_all")))},
			check:    MCPServer(projectP, "S").Connect(), want: true,
		},
		{
			name:     "tool block stops its tool",
			policies: []Policy{user("bob", members, memberConnectsAll, row(memberRole, ScopeMCPBlockedConnect, mcpRow("S", projectP, "tool", "delete_all")))},
			check:    MCPServer(projectP, "S").CallTool("delete_all", ""), want: false,
		},
		{
			name:     "project grant does not reach a server in another project",
			policies: []Policy{user("bob", members, memberConnectsAll)},
			check:    MCPServer(projectQ, "T").Connect(), want: false,
		},
		{
			name:     "read block does not stop connect",
			policies: []Policy{user("bob", members, memberConnectsAll, row(memberRole, ScopeMCPBlockedRead, mcpRow("S", projectP)))},
			check:    MCPServer(projectP, "S").Connect(), want: true,
		},

		// a user's own grant over a role's block
		{
			name:     "direct grant overrides a role block",
			policies: []Policy{user("dana", members, memberConnectsAll, memberBlockedS, danaGrantS)},
			check:    MCPServer(projectP, "S").Connect(), want: true,
		},
		{
			name:     "role member without a direct grant stays blocked",
			policies: []Policy{user("bob", members, memberConnectsAll, memberBlockedS)},
			check:    MCPServer(projectP, "S").Connect(), want: false,
		},
		{
			name:     "a grant on every server does not override",
			policies: []Policy{user("dana", members, memberConnectsAll, memberBlockedS, row("user:dana", ScopeMCPConnect, mcpRow("*", projectP)))},
			check:    MCPServer(projectP, "S").Connect(), want: false,
		},
		{
			name:     "an inferred grant does not override",
			policies: []Policy{user("dana", members, memberConnectsAll, memberBlockedS, inferred)},
			check:    MCPServer(projectP, "S").Connect(), want: false,
		},
		{
			name:     "the user's own block beats their own grant",
			policies: []Policy{user("dana", members, memberConnectsAll, danaGrantS, row("user:dana", ScopeMCPBlockedConnect, mcpRow("S", projectP)))},
			check:    MCPServer(projectP, "S").Connect(), want: false,
		},
		{
			name:     "a direct grant on one tool does not unblock another",
			policies: []Policy{user("dana", members, memberConnectsAll, memberBlockedS, row("user:dana", ScopeMCPConnect, mcpRow("S", projectP, "tool", "search")))},
			check:    MCPServer(projectP, "S").CallTool("delete_all", ""), want: false,
		},
		{
			name:     "an everyone block is inherited too",
			policies: []Policy{user("dana", nil, row("user:all", ScopeMCPConnect, mcpRow("*", projectP)), row("user:all", ScopeMCPBlockedConnect, mcpRow("S", projectP)), danaGrantS)},
			check:    MCPServer(projectP, "S").Connect(), want: true,
		},

		// agents: every policy must allow
		{
			name: "agent cannot exceed its owner",
			policies: []Policy{
				{Subject: Subject{Type: "agent", ID: "a1"}, Grants: []GrantRow{row("agent:a1", ScopeMCPConnect, mcpRow("*", projectP))}},
				user("carol", members, row(memberRole, ScopeMCPConnect, mcpRow("S1", projectP))),
			},
			check: MCPServer(projectP, "S2").Connect(), want: false,
		},
		{
			name: "agent and owner both allow",
			policies: []Policy{
				{Subject: Subject{Type: "agent", ID: "a1"}, Grants: []GrantRow{row("agent:a1", ScopeMCPConnect, mcpRow("*", projectP))}},
				user("carol", members, row(memberRole, ScopeMCPConnect, mcpRow("S1", projectP))),
			},
			check: MCPServer(projectP, "S1").Connect(), want: true,
		},
		{
			name: "an agent's own grant does not override a block",
			policies: []Policy{
				{Subject: Subject{Type: "agent", ID: "a1"}, Grants: []GrantRow{row("agent:a1", ScopeMCPConnect, mcpRow("S", projectP)), row("agent:a1", ScopeMCPBlockedConnect, mcpRow("*", projectP))}},
			},
			check: MCPServer(projectP, "S").Connect(), want: false,
		},
		{
			name: "api key holds only its own grants",
			policies: []Policy{
				{Subject: Subject{Type: "api_key", ID: "k1"}, Grants: []GrantRow{row("api_key:k1", ScopeMCPConnect, mcpRow("*", projectP))}},
			},
			check: MCPServer(projectP, "S").Connect(), want: true,
		},

		// chats
		{
			name:     "author reads their chat without a grant",
			policies: []Policy{user("bob", members)},
			check:    Chat(projectP, "c1").WithOwner(ChatOwner{UserID: "bob"}).Read(), want: true,
		},
		{
			name:     "another member does not",
			policies: []Policy{user("eve", members)},
			check:    Chat(projectP, "c1").WithOwner(ChatOwner{UserID: "bob"}).Read(), want: false,
		},
		{
			name:     "owner of a live agent reads its agent-only chat",
			policies: []Policy{user("carol", members)},
			check:    Chat(projectP, "c2").WithOwner(ChatOwner{Agent: &AgentOwner{AgentID: "a1", OwnerUserID: "carol"}}).Read(), want: true,
		},
		{
			name:     "not once the agent is deleted",
			policies: []Policy{user("carol", members)},
			check:    Chat(projectP, "c2").WithOwner(ChatOwner{Agent: &AgentOwner{AgentID: "a1", OwnerUserID: "carol", Deleted: true}}).Read(), want: false,
		},
		{
			name:     "not when a person started the chat",
			policies: []Policy{user("carol", members)},
			check:    Chat(projectP, "c3").WithOwner(ChatOwner{UserID: "bob", Agent: &AgentOwner{AgentID: "a1", OwnerUserID: "carol"}}).Read(), want: false,
		},
		{
			name:     "a project chat grant reads every chat",
			policies: []Policy{user("rita", []string{"role:organization:reviewers"}, row("role:organization:reviewers", ScopeChatRead, map[string]string{"resource_kind": "chat", "resource_id": "*", "project_id": projectP}))},
			check:    Chat(projectP, "c1").WithOwner(ChatOwner{UserID: "bob"}).Read(), want: true,
		},

		// collections, composites, auth clients, root
		{
			name:     "a project-wide grant lists the project's servers",
			policies: []Policy{user("bob", members, row(memberRole, ScopeMCPRead, mcpRow("*", projectP)))},
			check:    Project(org, projectP).ReadMCPServers(), want: true,
		},
		{
			name:     "a tool-limited project-wide grant still lists the servers",
			policies: []Policy{user("bob", members, row(memberRole, ScopeMCPRead, mcpRow("*", projectP, "tool", "search")))},
			check:    Project(org, projectP).ReadMCPServers(), want: true,
		},
		{
			name: "a bypass bounded to one server does not skip the policy elsewhere",
			policies: []Policy{user("bob", members,
				row(memberRole, ScopeRiskPolicyEvaluate, map[string]string{"resource_kind": "risk_policy", "resource_id": "R"}),
				row(memberRole, ScopeRiskPolicyBypass, map[string]string{"resource_kind": "risk_policy", "resource_id": "R", "server_url": "https://a.example"}))},
			check: RiskPolicy("R", "https://b.example", "").Evaluate(), want: true,
		},
		{
			name: "but does skip it on that server",
			policies: []Policy{user("bob", members,
				row(memberRole, ScopeRiskPolicyEvaluate, map[string]string{"resource_kind": "risk_policy", "resource_id": "R"}),
				row(memberRole, ScopeRiskPolicyBypass, map[string]string{"resource_kind": "risk_policy", "resource_id": "R", "server_url": "https://a.example"}))},
			check: RiskPolicy("R", "https://a.example", "").Evaluate(), want: false,
		},
		{
			name:     "a grant on one server does not list them all",
			policies: []Policy{user("bob", members, row(memberRole, ScopeMCPRead, mcpRow("S", projectP)))},
			check:    Project(org, projectP).ReadMCPServers(), want: false,
		},
		{
			name:     "write or administer passes for an org admin",
			policies: []Policy{user("ann", []string{adminRole}, row(adminRole, ScopeOrgAdmin, map[string]string{"resource_kind": "org", "resource_id": org}))},
			check:    Project(org, projectP).WriteOrAdminister(), want: true,
		},
		{
			name:     "a plugin block vetoes the administer branch",
			policies: []Policy{user("ann", []string{adminRole}, row(adminRole, ScopeOrgAdmin, map[string]string{"resource_kind": "org", "resource_id": org}), row(adminRole, ScopePluginBlockedWrite, map[string]string{"resource_kind": "project", "resource_id": projectP}))},
			check:    Project(org, projectP).PublishPlugin(), want: false,
		},
		{
			name:     "everyone uses their own personal connection",
			policies: []Policy{user("bob", nil, row("user:all", ScopeMCPAuth, DefaultForEveryone[0].Selector))},
			check:    MCPAuthClient("ac1", AuthClientAttrs{ClientType: "remote_session", ConnectionOwner: "bob"}).Use(), want: true,
		},
		{
			name:     "but not someone else's",
			policies: []Policy{user("bob", nil, row("user:all", ScopeMCPAuth, DefaultForEveryone[0].Selector))},
			check:    MCPAuthClient("ac1", AuthClientAttrs{ClientType: "remote_session", ConnectionOwner: "eve"}).Use(), want: false,
		},
		{
			name:     "root reaches every grant",
			policies: []Policy{user("sam", nil, row("user:sam", ScopeRoot, map[string]string{"resource_kind": "*", "resource_id": "*"}))},
			check:    MCPServer(projectP, "S").CallTool("delete_all", "destructive"), want: true,
		},
		{
			name:     "root does not reach upstream auth clients",
			policies: []Policy{user("sam", nil, row("user:sam", ScopeRoot, map[string]string{"resource_kind": "*", "resource_id": "*"}))},
			check:    MCPAuthClient("ac1", AuthClientAttrs{ClientType: "service_account"}).Use(), want: false,
		},
	}
}

func TestConformance(t *testing.T) {
	t.Parallel()
	reg, err := LoadRegistry()
	require.NoError(t, err)
	reference := NewClient(reg, spicedbEvaluator{registry: reg})
	ours := NewClient(reg, RegistryEvaluator{Registry: reg})

	for _, tc := range conformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := withCaller(context.Background(), Caller{OrganizationID: org, Policies: tc.policies, RepresentedUser: tc.policies[0].Subject.ID})

			got, err := reference.Allowed(ctx, tc.check)
			require.NoError(t, err)
			require.Equal(t, tc.want, got, "SpiceDB's evaluator over rbac.zed")

			mine, err := ours.Allowed(ctx, tc.check)
			if errors.Is(err, ErrNotImplemented) {
				return // the in-process evaluator is a stub in the spike
			}
			require.NoError(t, err)
			require.Equal(t, got, mine, "in-process evaluator disagrees with SpiceDB")
		})
	}
}

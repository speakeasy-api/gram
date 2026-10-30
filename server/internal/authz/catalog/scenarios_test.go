package catalog

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

// These tests back the five role scenarios in GUIDE.md, step 2. Connection
// checks go through the catalog client; tool calls are not in the catalog, so
// they are checked against the engine directly, the way the tool interceptors
// do.

const (
	scenarioRoleA = "role:organization:role-a"
	scenarioRoleB = "role:organization:role-b"
	scenarioRoleC = "role:organization:role-c"
	scenarioRoleD = "role:organization:role-d"
	scenarioSrvA  = "server-a"
	scenarioSrvC  = "server-c"
)

func scenarioGrant(principal string, scope authz.Scope, selector map[string]string) authz.Grant {
	s := authz.NewSelector(scope, selector[authz.SelectorKeyResourceID])
	for k, v := range selector {
		s[k] = v
	}
	return heldBy(principal, authz.NewGrantWithSelector(scope, s))
}

func connectDecision(t *testing.T, grants []authz.Grant, serverID, projectID string) Decision {
	t.Helper()
	client := newClientTestClient(t)
	ctx := authz.GrantsToContext(sessionCtx(t), grants)
	requests, err := MCPServerUse(MCPServerTarget(clientOrg, projectID, serverID), MCPServerPrivate)
	require.NoError(t, err)
	decision, err := client.Authorize(ctx, requests...)
	require.NoError(t, err)
	return decision
}

func toolCallAllowed(t *testing.T, grants []authz.Grant, serverID, tool, disposition string) bool {
	t.Helper()
	allowed, err := authz.GrantsAuthorize(grants, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: tool, Disposition: disposition, ProjectID: clientProject}))
	require.NoError(t, err)
	return allowed
}

// Scenario 1: role A may connect to every server in project A.
func TestScenario1_connectEveryServerInProject(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{scenarioGrant(scenarioRoleA, authz.ScopeMCPConnect, map[string]string{
		authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyProjectID: clientProject,
	})}

	for _, server := range []string{scenarioSrvA, scenarioSrvC} {
		decision := connectDecision(t, grants, server, clientProject)
		require.True(t, decision.Allowed(), server)
		require.Equal(t, BasisLegacyBroadGrant, decision.Basis, "mcp:connect is defined per server, so a project-wide grant is flagged")
	}
	require.False(t, connectDecision(t, grants, scenarioSrvA, "project-2").Allowed(), "another project")
}

// Scenario 2: role A may manage one server, which also lets it view that
// server, but not use it.
func TestScenario2_manageOneServerImpliesView(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := authz.GrantsToContext(sessionCtx(t), []authz.Grant{scenarioGrant(scenarioRoleA, authz.ScopeMCPWrite, map[string]string{
		authz.SelectorKeyResourceID: scenarioSrvA, authz.SelectorKeyProjectID: clientProject,
	})})
	target := MCPServerTarget(clientOrg, clientProject, scenarioSrvA)

	for name, tc := range map[string]struct {
		request Request
		allowed bool
	}{
		"manage":         {Request{Permission: authz.ScopeMCPWrite, Target: target}, true},
		"view":           {MCPServerRead(target), true},
		"connect":        {Request{Permission: authz.ScopeMCPConnect, Target: target}, false},
		"view another":   {MCPServerRead(MCPServerTarget(clientOrg, clientProject, scenarioSrvC)), false},
		"manage another": {Request{Permission: authz.ScopeMCPWrite, Target: MCPServerTarget(clientOrg, clientProject, scenarioSrvC)}, false},
	} {
		decision, err := client.Authorize(ctx, tc.request)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, decision.Allowed(), name)
	}
}

// Scenario 3: role B blocks server C; a user in roles A and B connects to every
// server in the project except C, and cannot call C's tools either.
func TestScenario3_blockOneServerForUserInBothRoles(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{
		scenarioGrant(scenarioRoleA, authz.ScopeMCPConnect, map[string]string{
			authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyProjectID: clientProject,
		}),
		scenarioGrant(scenarioRoleB, authz.ScopeMCPBlockedConnect, map[string]string{authz.SelectorKeyResourceID: scenarioSrvC}),
	}

	require.True(t, connectDecision(t, grants, scenarioSrvA, clientProject).Allowed())
	blocked := connectDecision(t, grants, scenarioSrvC, clientProject)
	require.False(t, blocked.Allowed())
	require.Equal(t, ReasonBlocked, blocked.Reason)

	require.True(t, toolCallAllowed(t, grants, scenarioSrvA, "search", authz.DispositionReadOnly))
	require.False(t, toolCallAllowed(t, grants, scenarioSrvC, "search", authz.DispositionReadOnly))

	// A grant made directly to the user does not get past the block either.
	withDirect := []authz.Grant{grants[0], grants[1], scenarioGrant(clientUser, authz.ScopeMCPConnect, map[string]string{authz.SelectorKeyResourceID: scenarioSrvC})}
	require.False(t, connectDecision(t, withDirect, scenarioSrvC, clientProject).Allowed())
}

// Scenario 4: role C may connect to every server but only call read-only
// tools. The restriction is a disposition on the stored grant; the engine
// applies it to tool calls. A tool with no annotations has no disposition, so
// the check does not constrain it and the read-only grant matches: this test
// documents that gap.
func TestScenario4_readOnlyToolsEverywhere(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{scenarioGrant(scenarioRoleC, authz.ScopeMCPConnect, map[string]string{
		authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyDisposition: authz.DispositionReadOnly,
	})}

	decision := connectDecision(t, grants, scenarioSrvA, clientProject)
	require.True(t, decision.Allowed(), "connecting does not carry a disposition")
	require.Equal(t, BasisLegacyBroadGrant, decision.Basis)

	require.True(t, toolCallAllowed(t, grants, scenarioSrvA, "search", authz.DispositionReadOnly))
	require.False(t, toolCallAllowed(t, grants, scenarioSrvA, "delete_page", authz.DispositionDestructive))
	require.False(t, toolCallAllowed(t, grants, scenarioSrvA, "update_page", authz.DispositionIdempotent))
	require.True(t, toolCallAllowed(t, grants, scenarioSrvA, "unannotated_tool", ""), "gap: an unannotated tool is not constrained")
}

// Scenario 5: role D may connect to server A and call only tool A on it.
func TestScenario5_oneToolOnOneServer(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{scenarioGrant(scenarioRoleD, authz.ScopeMCPConnect, map[string]string{
		authz.SelectorKeyResourceID: scenarioSrvA, authz.SelectorKeyTool: "tool_a",
	})}

	decision := connectDecision(t, grants, scenarioSrvA, clientProject)
	require.True(t, decision.Allowed(), "connecting does not carry a tool")
	require.Equal(t, BasisNone, decision.Basis)
	require.False(t, connectDecision(t, grants, scenarioSrvC, clientProject).Allowed())

	require.True(t, toolCallAllowed(t, grants, scenarioSrvA, "tool_a", authz.DispositionReadOnly))
	require.False(t, toolCallAllowed(t, grants, scenarioSrvA, "tool_b", authz.DispositionReadOnly))
	require.False(t, toolCallAllowed(t, grants, scenarioSrvC, "tool_a", authz.DispositionReadOnly))
}

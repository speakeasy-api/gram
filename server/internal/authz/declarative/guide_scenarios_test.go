package declarative

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/declarative/relschema"
)

// The scenarios in GUIDE.md step 2, each asserted against the stored rows the
// guide shows. Project PA holds servers S1, S2 and SC; project PO holds SO.
const (
	gsPA = "proj_a"
	gsPO = "proj_other"
	gsS1 = "mcp_server_1"
	gsS2 = "mcp_server_2"
	gsSC = "mcp_server_c"
	gsSO = "mcp_server_other"
)

var (
	gsRefS1 = MCPServerRef{ID: gsS1, ProjectID: gsPA, OrganizationID: fxOrg}
	gsRefS2 = MCPServerRef{ID: gsS2, ProjectID: gsPA, OrganizationID: fxOrg}
	gsRefSC = MCPServerRef{ID: gsSC, ProjectID: gsPA, OrganizationID: fxOrg}
	gsRefSO = MCPServerRef{ID: gsSO, ProjectID: gsPO, OrganizationID: fxOrg}
)

func gsRow(principal string, scope authz.Scope, selector authz.Selector) authz.Grant {
	return authz.Grant{PrincipalUrn: principal, Scope: scope, Selector: selector, Inferred: false}
}

// gsDecide is what RequireMCPServer decides for a human whose one policy holds
// rows, with no tool known yet.
func gsDecide(t *testing.T, permission MCPServerPermission, server MCPServerRef, rows ...authz.Grant) MCPServerDecision {
	t.Helper()
	decision, err := decideMCPServer(authz.PolicySet{Enforced: true, OrganizationID: fxOrg, Policies: [][]authz.Grant{rows}}, permission, server)
	require.NoError(t, err)
	return decision
}

// gsDecideTool evaluates connect with tool facts. No entry point passes facts
// yet: the tool-call path still uses the legacy check.
func gsDecideTool(t *testing.T, server MCPServerRef, facts map[string]string, rows ...authz.Grant) relschema.Decision {
	t.Helper()
	rels, err := decodeMCPServerPolicy(fxOrg, rows)
	require.NoError(t, err)
	object := relschema.Object{Type: typeMCPServer, ID: server.ID, Ancestors: []relschema.ObjectRef{
		{Type: typeProject, ID: server.ProjectID},
		{Type: typeOrganization, ID: server.OrganizationID},
	}}
	return mcpServerSchema.Evaluate(fxOrg, rels, object, string(MCPServerConnect), relschema.Request{Facts: facts})
}

func TestGuideScenarios(t *testing.T) {
	t.Parallel()

	t.Run("1 connect to every server in a project", func(t *testing.T) {
		t.Parallel()
		row := gsRow("role:a", authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": gsPA})
		for _, server := range []MCPServerRef{gsRefS1, gsRefS2, gsRefSC} {
			d := gsDecide(t, MCPServerConnect, server, row)
			require.True(t, d.Allowed, server.ID)
			require.Contains(t, d.trace(), "legacy broad grant", "a project-wide connect row is honored as legacy")
		}
		require.False(t, gsDecide(t, MCPServerConnect, gsRefSO, row).Allowed, "another project")

		orgWide := gsRow("role:a", authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": "*"})
		for _, server := range []MCPServerRef{gsRefS1, gsRefSO} {
			d := gsDecide(t, MCPServerConnect, server, orgWide)
			require.True(t, d.Allowed, server.ID)
			require.Contains(t, d.trace(), "legacy broad grant")
		}
		// Switched off, the legacy check allows the same row.
		require.True(t, authz.GrantsSatisfy([]authz.Grant{row}, authz.MCPCheck(authz.ScopeMCPConnect, gsS1, gsPA)))
	})

	t.Run("2 manage one server, which includes viewing it", func(t *testing.T) {
		t.Parallel()
		row := gsRow("role:a", authz.ScopeMCPWrite, authz.Selector{"resource_kind": "mcp", "resource_id": gsS1, "project_id": gsPA})
		require.True(t, gsDecide(t, MCPServerEdit, gsRefS1, row).Allowed)
		require.True(t, gsDecide(t, MCPServerRead, gsRefS1, row).Allowed)
		require.False(t, gsDecide(t, MCPServerConnect, gsRefS1, row).Allowed, "managing is not using")
		require.False(t, gsDecide(t, MCPServerEdit, gsRefS2, row).Allowed)
		require.False(t, gsDecide(t, MCPServerRead, gsRefS2, row).Allowed)

		wrongProject := gsRow("role:a", authz.ScopeMCPWrite, authz.Selector{"resource_kind": "mcp", "resource_id": gsS1, "project_id": gsPO})
		for _, permission := range []MCPServerPermission{MCPServerRead, MCPServerEdit, MCPServerConnect} {
			require.False(t, gsDecide(t, permission, gsRefS1, wrongProject).Allowed, "narrowing to the wrong project: %s", permission)
		}
	})

	t.Run("3 a block from one role beats an allow from another", func(t *testing.T) {
		t.Parallel()
		rows := []authz.Grant{
			gsRow("role:a", authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": gsPA}),
			gsRow("role:b", authz.ScopeMCPBlockedConnect, authz.Selector{"resource_kind": "mcp", "resource_id": gsSC}),
		}
		require.True(t, gsDecide(t, MCPServerConnect, gsRefS1, rows...).Allowed)
		require.True(t, gsDecide(t, MCPServerConnect, gsRefS2, rows...).Allowed)
		blocked := gsDecide(t, MCPServerConnect, gsRefSC, rows...)
		require.False(t, blocked.Allowed)
		require.Contains(t, blocked.trace(), "withdrawn by a matching block")

		onUser := []authz.Grant{rows[0], gsRow(fxUser, authz.ScopeMCPBlockedConnect, authz.Selector{"resource_kind": "mcp", "resource_id": gsSC})}
		require.False(t, gsDecide(t, MCPServerConnect, gsRefSC, onUser...).Allowed, "the block's principal is not consulted")
		require.True(t, gsDecide(t, MCPServerConnect, gsRefS1, onUser...).Allowed)

		direct := append(rows, gsRow(fxUser, authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": gsSC}))
		require.False(t, gsDecide(t, MCPServerConnect, gsRefSC, direct...).Allowed, "a direct allow does not outrank the block")

		projectBlock := []authz.Grant{rows[0], gsRow("role:b", authz.ScopeMCPBlockedConnect, authz.Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": gsPA})}
		for _, server := range []MCPServerRef{gsRefS1, gsRefS2, gsRefSC} {
			require.False(t, gsDecide(t, MCPServerConnect, server, projectBlock...).Allowed, server.ID)
		}
	})

	t.Run("4 every server, read-only tools", func(t *testing.T) {
		t.Parallel()
		row := gsRow("role:c", authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": "*", "disposition": authz.DispositionReadOnly})
		require.True(t, gsDecide(t, MCPServerConnect, gsRefS1, row).Allowed, "the connection opens before any tool is known")
		require.True(t, gsDecide(t, MCPServerConnect, gsRefSO, row).Allowed, "every project")
		require.Equal(t, relschema.Allowed, gsDecideTool(t, gsRefS1, map[string]string{"tool": "search", "disposition": authz.DispositionReadOnly}, row).Outcome)
		require.Equal(t, relschema.Denied, gsDecideTool(t, gsRefS1, map[string]string{"tool": "delete_all", "disposition": authz.DispositionDestructive}, row).Outcome)
		// Gap: a fact the request omits reads as a server-level question, so a
		// tool-call entry point must always pass the tool's disposition.
		require.Equal(t, relschema.Allowed, gsDecideTool(t, gsRefS1, map[string]string{"tool": "unannotated"}, row).Outcome)
		// Today's check behaves the same way: an unannotated tool has an empty
		// disposition, which MCPToolCallCheck leaves out of the check.
		require.True(t, authz.GrantsSatisfy([]authz.Grant{row}, authz.MCPToolCallCheck(gsS1, authz.MCPToolCallDimensions{Tool: "unannotated", Disposition: "", ProjectID: gsPA})))
		require.False(t, authz.GrantsSatisfy([]authz.Grant{row}, authz.MCPToolCallCheck(gsS1, authz.MCPToolCallDimensions{Tool: "delete_all", Disposition: authz.DispositionDestructive, ProjectID: gsPA})))

		apiKeyRow := gsRow("api_key:key_1", authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": "*", "disposition": authz.DispositionReadOnly})
		require.False(t, gsDecide(t, MCPServerConnect, gsRefS1, apiKeyRow).Allowed, "a strict row needs every fact")
	})

	t.Run("5 one server, one tool", func(t *testing.T) {
		t.Parallel()
		row := gsRow("role:d", authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": gsS1, "tool": "tool_a"})
		require.True(t, gsDecide(t, MCPServerConnect, gsRefS1, row).Allowed)
		require.False(t, gsDecide(t, MCPServerConnect, gsRefS2, row).Allowed)
		require.Equal(t, relschema.Allowed, gsDecideTool(t, gsRefS1, map[string]string{"tool": "tool_a"}, row).Outcome)
		require.Equal(t, relschema.Denied, gsDecideTool(t, gsRefS1, map[string]string{"tool": "tool_b"}, row).Outcome)

		withBlock := []authz.Grant{row, gsRow("role:b", authz.ScopeMCPBlockedConnect, authz.Selector{"resource_kind": "mcp", "resource_id": gsS1, "tool": "tool_a"})}
		require.True(t, gsDecide(t, MCPServerConnect, gsRefS1, withBlock...).Allowed, "a tool-limited block never withdraws the whole server")
		require.Equal(t, relschema.Denied, gsDecideTool(t, gsRefS1, map[string]string{"tool": "tool_a"}, withBlock...).Outcome)
	})
}

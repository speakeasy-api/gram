package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// refusesLogReadOnly admits project reads but refuses log:read, as a caller
// holding project or organization admin access without log:read is treated.
func refusesLogReadOnly() refusingProjectReader {
	return refusingProjectReader{diagnosticsProjectReader: diagnosticsProjectReader{output: ListProjectsOutput{}}, projectErr: nil, logReadErr: ErrForbidden, mcpErr: nil}
}

func TestSearchToolCallsRequiresLogReadBeforeSpendingBudget(t *testing.T) {
	t.Parallel()

	search := &recordingToolCallSearchReader{}
	service := newToolCallSearchService(t, search, &recordingDrilldownAuditor{})
	service.reader = refusesLogReadOnly()
	connection := &recordingOperationLimiter{}
	service.sensitiveBudget = OperationBudget{Connection: connection, Organization: allowOperationLimiter{}}

	_, err := service.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject})

	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, search.calls, "no tool-call rows are read without log:read")
	require.Empty(t, connection.keys, "a refused call never reaches the sensitive budget")
}

func TestQueryMCPTracesRequiresLogReadBeforeSpendingBudget(t *testing.T) {
	t.Parallel()

	service := newToolCallSearchService(t, &recordingToolCallSearchReader{}, &recordingDrilldownAuditor{})
	service.reader = refusesLogReadOnly()
	connection := &recordingOperationLimiter{}
	service.sensitiveBudget = OperationBudget{Connection: connection, Organization: allowOperationLimiter{}}

	_, err := service.QueryMCPTraces(t.Context(), testPrincipal(), QueryMCPTracesInput{ProjectID: toolCallSearchTestProject, MCPID: "mcp-1"})

	require.ErrorIs(t, err, ErrForbidden)
	require.Empty(t, connection.keys, "a refused call never reaches the sensitive budget")
}

func TestQueryMCPTracesValidatesTargetBeforeAuthorizing(t *testing.T) {
	t.Parallel()

	service := newToolCallSearchService(t, &recordingToolCallSearchReader{}, &recordingDrilldownAuditor{})
	service.reader = refusesLogReadOnly()

	_, err := service.QueryMCPTraces(t.Context(), testPrincipal(), QueryMCPTracesInput{ProjectID: toolCallSearchTestProject})

	require.ErrorContains(t, err, "project_id and mcp_id are required")
}

// The individual-call tools are discoverable only by members holding log:read
// somewhere; project and organization admin access do not reveal them.
func TestLogReadToolsDiscoverableOnlyWithLogRead(t *testing.T) {
	t.Parallel()

	principal := testPrincipal()
	_, registrar := newServer(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	admin := []authz.Grant{
		authz.NewGrant(authz.ScopeProjectRead, toolCallSearchTestProject),
		authz.NewGrant(authz.ScopeProjectWrite, toolCallSearchTestProject),
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
	}
	logReader := []authz.Grant{authz.NewGrant(authz.ScopeLogRead, toolCallSearchTestProject)}

	for _, name := range []string{"search_tool_calls", "list_recent_tool_calls", "query_mcp_traces"} {
		meta := descriptorByName(t, registrar, name).Meta
		require.False(t, externalToolDiscoverable(admin, true, principal, meta), "%s must stay hidden without log:read", name)
		require.True(t, externalToolDiscoverable(logReader, true, principal, meta), "%s must be discoverable with log:read", name)
	}
}

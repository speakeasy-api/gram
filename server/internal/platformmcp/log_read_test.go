package platformmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// refusesLogReadOnly admits project reads but refuses log:read, as a non-human
// caller without log:read is treated: API keys, the assistant runtime and
// embedded chat sessions get no own-scope fallback.
func refusesLogReadOnly() refusingProjectReader {
	return refusingProjectReader{diagnosticsProjectReader: diagnosticsProjectReader{output: ListProjectsOutput{}}, projectErr: nil, logReadErr: ErrForbidden, mcpErr: nil}
}

// ownScopeProjectReader resolves every project into a member's own scope, as a
// member without log:read is treated.
type ownScopeProjectReader struct {
	diagnosticsProjectReader
	scope *telemetryrepo.OwnScope
}

func (r ownScopeProjectReader) ResolveProjectLogScope(context.Context, Principal, FindMCPInput) (ResolvedProject, *telemetryrepo.OwnScope, error) {
	return ResolvedProject{}, r.scope, nil
}

func TestSearchToolCallsRefusesNonHumanCallersBeforeSpendingBudget(t *testing.T) {
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

// A member's own scope is handed to the telemetry query unchanged and ANDed
// with the caller's filters there, so a filter can only narrow it.
func TestSearchToolCallsPassesOwnScopeToTheQuery(t *testing.T) {
	t.Parallel()

	search := &recordingToolCallSearchReader{}
	service := newToolCallSearchService(t, search, &recordingDrilldownAuditor{})
	scope := &telemetryrepo.OwnScope{UserID: "user-1", UserEmail: "member@example.com", AgentIDs: []string{"agent-1"}, IdentityOrg: ""}
	service.reader = ownScopeProjectReader{diagnosticsProjectReader: diagnosticsProjectReader{output: ListProjectsOutput{}}, scope: scope}

	_, err := service.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Outcome: ToolCallOutcomeFailure})
	require.NoError(t, err)
	require.Equal(t, 1, search.calls)
	require.Equal(t, scope, search.traceParams.OwnScope)
	require.Equal(t, []string{ToolCallOutcomeFailure}, search.traceParams.Statuses, "the caller's filters still apply inside the own scope")
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

// The tools that list individual calls are discoverable by every member, who
// sees their own calls through them; the trace reader still needs log:read.
func TestLogReadToolsDiscoverability(t *testing.T) {
	t.Parallel()

	principal := testPrincipal()
	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	admin := []authz.Grant{
		authz.NewGrant(authz.ScopeProjectRead, toolCallSearchTestProject),
		authz.NewGrant(authz.ScopeProjectWrite, toolCallSearchTestProject),
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
	}
	logReader := []authz.Grant{authz.NewGrant(authz.ScopeLogRead, toolCallSearchTestProject)}

	for _, name := range []string{"search_tool_calls", "list_recent_tool_calls"} {
		meta := descriptorByName(t, registrar, name).Meta
		require.True(t, externalToolDiscoverable([]authz.Grant{}, true, principal, meta), "%s must be discoverable by a member with no grants", name)
		require.True(t, externalToolDiscoverable(admin, true, principal, meta), "%s must be discoverable without log:read", name)
		require.True(t, externalToolDiscoverable(logReader, true, principal, meta), "%s must be discoverable with log:read", name)
	}
	meta := descriptorByName(t, registrar, "query_mcp_traces").Meta
	require.False(t, externalToolDiscoverable(admin, true, principal, meta), "query_mcp_traces must stay hidden without log:read")
	require.True(t, externalToolDiscoverable(logReader, true, principal, meta), "query_mcp_traces must be discoverable with log:read")
}

package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A check that names a tool or a disposition has to be built as a tool call.
// Anything else naming one is refused, so it is never read as a connection.
func TestToolAndDispositionBelongToToolCalls(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	cases := []struct {
		name  string
		check Check
	}{
		{
			name:  "connect check with a tool added by hand",
			check: Check{Scope: ScopeMCPConnect, ResourceID: "server-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search"}},
		},
		{
			name:  "connect check with a disposition added by hand",
			check: Check{Scope: ScopeMCPConnect, ResourceID: "server-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyDisposition: DispositionReadOnly}},
		},
		{
			name:  "server read with a tool",
			check: Check{Scope: ScopeMCPRead, ResourceID: "server-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(root, tc.check)
			require.ErrorIs(t, err, ErrInvalidCheck)
			require.False(t, allowed)
		})
	}
}

func TestToolCallConstructorsBuildToolCalls(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	server := uuid.New()
	project := uuid.New()
	checks := []Check{
		MCPToolCallCheck(server.String(), MCPToolCallDimensions{Tool: "search", Disposition: DispositionReadOnly, ProjectID: project.String()}),
		MCPToolCallCheck(server.String(), MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: project.String()}),
		MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("search", DispositionReadOnly),
	}
	for _, check := range checks {
		require.True(t, check.isToolCall())
		allowed, err := GrantsAuthorize(root, check)
		require.NoError(t, err)
		require.True(t, allowed)
	}

	connect := MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).Connect()
	require.False(t, connect.isToolCall())
	allowed, err := GrantsAuthorize(root, connect)
	require.NoError(t, err)
	require.True(t, allowed)
}

// A grant limited to read-only tools admits the connection and read-only
// calls, and nothing built as a tool call slips through as a connection.
func TestDispositionLimitedGrantSeparatesConnectFromCalls(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	selector := NewSelector(ScopeMCPConnect, server.String())
	selector[SelectorKeyDisposition] = DispositionReadOnly
	grants := []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)}
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	allowed, err := GrantsAuthorize(grants, ref.Connect())
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, ref.ToolCall("search", DispositionReadOnly))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, ref.ToolCall("delete_all", DispositionDestructive))
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = GrantsAuthorize(grants, ref.ToolCall("mystery", ""))
	require.NoError(t, err)
	require.False(t, allowed)
}

package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A tool call recorded as a denial is rebuilt as a tool call, so a grant that
// covers the original call also covers the rebuilt one.
func TestRecordedCheckRebuildsToolCalls(t *testing.T) {
	t.Parallel()

	grant := NewGrantWithSelector(ScopeMCPConnect, Selector{
		SelectorKeyResourceKind: "mcp",
		SelectorKeyResourceID:   "server-one",
		SelectorKeyTool:         "search",
	})
	dimensions := map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}

	recorded := RecordedCheck(ScopeMCPConnect, "", "server-one", dimensions)
	require.True(t, recorded.isToolCall())
	allowed, err := GrantsAuthorize([]Grant{grant}, recorded)
	require.NoError(t, err)
	require.True(t, allowed)

	other := RecordedCheck(ScopeMCPConnect, "", "server-one", map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "delete", SelectorKeyDisposition: DispositionDestructive})
	allowed, err = GrantsAuthorize([]Grant{grant}, other)
	require.NoError(t, err)
	require.False(t, allowed)
}

// Only connect has tool calls: a recorded view check that names a tool is
// still refused as invalid, and a recorded connection stays a connection.
func TestRecordedCheckKeepsOtherChecksAsTheyWere(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	read := RecordedCheck(ScopeMCPRead, "", "server-one", map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search"})
	_, err := GrantsAuthorize(root, read)
	require.ErrorIs(t, err, ErrInvalidCheck)

	connection := RecordedCheck(ScopeMCPConnect, "", "server-one", map[string]string{SelectorKeyProjectID: "project-one"})
	require.False(t, connection.isToolCall())
	allowed, err := GrantsAuthorize(root, connection)
	require.NoError(t, err)
	require.True(t, allowed)
}

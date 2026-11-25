package mcpversions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

func TestDefinesMethodKeepsCommonOperationsInEveryRevision(t *testing.T) {
	t.Parallel()
	for _, method := range []string{mcpversions.MethodToolsList, mcpversions.MethodToolsCall, mcpversions.MethodPromptsList, mcpversions.MethodPromptsGet, mcpversions.MethodResourcesList, mcpversions.MethodResourcesTemplatesList, mcpversions.MethodResourcesRead, mcpversions.MethodNotificationsCancelled} {
		for _, version := range mcpversions.All() {
			require.True(t, mcpversions.DefinesMethod(method, version), "%s/%s", version, method)
		}
	}
}

func TestDefinesMethodSplitsAtTheStatelessBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method string

		// stateless is whether the method belongs to 2026-07-28 rather than
		// to the handshake-based revisions before it.
		stateless bool
	}{
		{method: mcpversions.MethodInitialize, stateless: false},
		{method: mcpversions.MethodNotificationsInitialized, stateless: false},
		{method: mcpversions.MethodPing, stateless: false},
		{method: mcpversions.MethodLoggingSetLevel, stateless: false},
		{method: mcpversions.MethodResourcesSubscribe, stateless: false},
		{method: mcpversions.MethodResourcesUnsubscribe, stateless: false},
		{method: mcpversions.MethodNotificationsRootsListChanged, stateless: false},
		{method: mcpversions.MethodServerDiscover, stateless: true},
		{method: mcpversions.MethodSubscriptionsListen, stateless: true},
	} {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, !tc.stateless, mcpversions.DefinesMethod(tc.method, mcpversions.Version20251125))
			require.Equal(t, tc.stateless, mcpversions.DefinesMethod(tc.method, mcpversions.Version20260728))
		})
	}
}

func TestDefinesMethodScopesTasksToTheirCoreRevision(t *testing.T) {
	t.Parallel()
	for _, method := range []string{mcpversions.MethodTasksGet, mcpversions.MethodTasksResult, mcpversions.MethodTasksList, mcpversions.MethodTasksCancel, mcpversions.MethodNotificationsTasksStatus} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			require.False(t, mcpversions.DefinesMethod(method, mcpversions.Version20250618))
			require.True(t, mcpversions.DefinesMethod(method, mcpversions.Version20251125))
			require.False(t, mcpversions.DefinesMethod(method, mcpversions.Version20260728))
		})
	}
}

func TestDefinesMethodRejectsUnrecognizedInput(t *testing.T) {
	t.Parallel()
	require.False(t, mcpversions.DefinesMethod(mcpversions.MethodToolsList, "unknown"))
	require.False(t, mcpversions.DefinesMethod(mcpversions.MethodToolsList, ""))
	require.False(t, mcpversions.DefinesMethod("unknown/method", mcpversions.Version20251125))
	require.False(t, mcpversions.DefinesMethod("", mcpversions.Version20251125))
}

func TestKnownMethodSpansEveryRevision(t *testing.T) {
	t.Parallel()
	// Removed methods stay known: clients on older revisions still send them.
	require.True(t, mcpversions.KnownMethod(mcpversions.MethodInitialize))
	require.True(t, mcpversions.KnownMethod(mcpversions.MethodTasksResult))
	require.True(t, mcpversions.KnownMethod(mcpversions.MethodServerDiscover))
	require.False(t, mcpversions.KnownMethod("tasks/update"))
	require.False(t, mcpversions.KnownMethod(""))
}

package organizations

import (
	"slices"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/stretchr/testify/require"
)

func TestSetupWorkstreamMembershipPartitionsCatalog(t *testing.T) {
	t.Parallel()
	views := setupWorkstreamViews()
	require.Len(t, views, len(setupWorkstreams))
	seenIDs := map[string]bool{}
	seen := map[string]bool{}
	for i, workstream := range setupWorkstreams {
		require.NotEmpty(t, workstream.ID)
		require.NotEmpty(t, workstream.Title)
		require.NotEmpty(t, workstream.TaskKeys)
		require.False(t, seenIDs[workstream.ID], "workstream IDs must be unique")
		seenIDs[workstream.ID] = true
		require.Same(t, &setupWorkstreams[i], setupWorkstreamForID(workstream.ID), "assignment uses the canonical catalog")
		require.Equal(t, workstream.ID, views[i].ID)
		require.Equal(t, workstream.Title, views[i].Title)
		require.Equal(t, workstream.TaskKeys, views[i].TaskKeys, "API preserves task order, including hidden tasks")
		for _, key := range workstream.TaskKeys {
			require.NotNil(t, setupTaskDefinitionForKey(key))
			require.False(t, seen[key], "task must belong to only one workstream: %s", key)
			seen[key] = true
		}
	}
	require.Len(t, seen, len(setupTaskCatalog), "every task, including hidden tasks, needs a workstream")
	require.Nil(t, setupWorkstreamForID("unknown"))
}

func TestSetupWorkstreamViewsFilterMembership(t *testing.T) {
	t.Parallel()
	views := setupWorkstreamViewsForTasks([]*gen.SetupTask{{Key: "directory-sync"}, {Key: "domain-verification"}})
	require.Len(t, views, len(setupWorkstreams))
	require.Equal(t, []string{"domain-verification", "directory-sync"}, views[0].TaskKeys, "catalog order is preserved")
	for _, view := range views[1:] {
		require.Empty(t, view.TaskKeys, "empty workstreams must not disclose hidden members")
		require.NotNil(t, view.TaskKeys, "serialize empty membership as an array")
	}
	require.Equal(t, []string{"domain-verification", "identity-provider", "connect-idp", "directory-sync"}, setupWorkstreams[0].TaskKeys, "filtering must not mutate assignment membership")
}

func TestSetupWorkstreamViewsDoNotAliasCatalog(t *testing.T) {
	t.Parallel()
	title := setupWorkstreams[0].Title
	keys := slices.Clone(setupWorkstreams[0].TaskKeys)
	views := setupWorkstreamViews()
	views[0].Title = "Changed"
	views[0].TaskKeys[0] = "unknown"
	fresh := setupWorkstreamViews()
	require.Equal(t, title, setupWorkstreams[0].Title)
	require.Equal(t, keys, setupWorkstreams[0].TaskKeys)
	require.Equal(t, title, fresh[0].Title)
	require.Equal(t, keys, fresh[0].TaskKeys)
}

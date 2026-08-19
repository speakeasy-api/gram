package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
)

// The identity page asks "which plugins does this person get". A plugin
// assigned to them directly counts, and so does one distributed to everyone —
// from the subject's side an org-wide plugin is one they receive.
func TestPluginsService_ListPlugins_FiltersByPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	mine, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Assigned To Me"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      mine.ID,
		PrincipalUrns: []string{"user:user_subject"},
	})
	require.NoError(t, err)

	theirs, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Assigned To Someone Else"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      theirs.ID,
		PrincipalUrns: []string{"user:user_other"},
	})
	require.NoError(t, err)

	everyone, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Assigned To Everyone"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      everyone.ID,
		PrincipalUrns: []string{"*"},
	})
	require.NoError(t, err)

	result, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{
		PrincipalUrns:    []string{"user:user_subject"},
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	names := make([]string, 0, len(result.Plugins))
	for _, plugin := range result.Plugins {
		names = append(names, plugin.Name)
	}
	require.Contains(t, names, "Assigned To Me")
	require.Contains(t, names, "Assigned To Everyone")
	require.NotContains(t, names, "Assigned To Someone Else")
}

// Without the filter the listing is unchanged, including plugins targeted at
// somebody else entirely.
func TestPluginsService_ListPlugins_UnfilteredIsUnnarrowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	theirs, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Assigned To Someone Else"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      theirs.ID,
		PrincipalUrns: []string{"user:user_other"},
	})
	require.NoError(t, err)

	unfiltered, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{
		PrincipalUrns:    nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	found := false
	for _, plugin := range unfiltered.Plugins {
		if plugin.ID == theirs.ID {
			found = true
		}
	}
	require.True(t, found, "an unfiltered listing must not be narrowed by assignment")

	filtered, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{
		PrincipalUrns:    []string{"user:user_subject"},
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	for _, plugin := range filtered.Plugins {
		require.NotEqual(t, theirs.ID, plugin.ID, "a plugin assigned to someone else must not appear")
	}
}

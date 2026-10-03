package servernames_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/servernames"
)

const (
	chatServerID    = "0192a3b4-0000-7000-8000-000000000001"
	billingServerID = "0192a3b4-0000-7000-8000-000000000002"
)

func fixtureResolver() *servernames.Resolver {
	return servernames.NewResolver([]servernames.ConfiguredServer{
		{
			ID:          chatServerID,
			Name:        "External Acme Chat",
			Slug:        "acme-chat",
			ToolsetSlug: "",
			Plugins: []servernames.PluginMembership{
				{PluginSlug: "acme-tools", DisplayName: "External Acme Chat"},
			},
		},
		{
			ID:          billingServerID,
			Name:        "Billing",
			Slug:        "billing",
			ToolsetSlug: "billing-tools",
			Plugins: []servernames.PluginMembership{
				{PluginSlug: "finance", DisplayName: "Billing"},
			},
		},
	})
}

func TestResolver_ResolvesPluginRoutedName(t *testing.T) {
	t.Parallel()

	resolver := fixtureResolver()

	id, ok := resolver.Resolve("plugin_acme-tools_External_Acme_Chat")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)

	// Claude Code's prefix is compared case-insensitively.
	id, ok = resolver.Resolve("PLUGIN_ACME-TOOLS_EXTERNAL_ACME_CHAT")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)

	id, ok = resolver.Resolve("plugin_finance_Billing")
	require.True(t, ok)
	require.Equal(t, billingServerID, id)
}

func TestResolver_ResolvesBareSlugAndToolsetSlug(t *testing.T) {
	t.Parallel()

	resolver := fixtureResolver()

	id, ok := resolver.Resolve("billing")
	require.True(t, ok)
	require.Equal(t, billingServerID, id)

	id, ok = resolver.Resolve("acme-chat")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)

	id, ok = resolver.Resolve("billing-tools")
	require.True(t, ok)
	require.Equal(t, billingServerID, id)
}

func TestResolver_ResolvesDisplayNameInAnySpelling(t *testing.T) {
	t.Parallel()

	resolver := fixtureResolver()

	// An inventory-resolved hook reports the display name verbatim.
	id, ok := resolver.Resolve("External Acme Chat")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)

	// A tool-prefix derived name has its spaces rewritten.
	id, ok = resolver.Resolve("External_Acme_Chat")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)

	id, ok = resolver.Resolve("external_acme_chat")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)
}

func TestResolver_ResolvesConfiguredID(t *testing.T) {
	t.Parallel()

	resolver := fixtureResolver()

	id, ok := resolver.Resolve(billingServerID)
	require.True(t, ok)
	require.Equal(t, billingServerID, id)

	id, ok = resolver.Resolve("0192A3B4-0000-7000-8000-000000000002")
	require.True(t, ok)
	require.Equal(t, billingServerID, id)
}

func TestResolver_LeavesUnknownNamesUnresolved(t *testing.T) {
	t.Parallel()

	resolver := fixtureResolver()

	for _, reported := range []string{
		"claude-in-chrome",
		"/usr/local/bin/some-stdio-server",
		"0192a3b4-0000-7000-8000-00000000ffff",
		"plugin_acme-tools_Other",
		"claude_ai_External_Acme_Chat",
		"",
		"   ",
	} {
		_, ok := resolver.Resolve(reported)
		require.False(t, ok, reported)
	}
}

func TestResolver_RefusesNamesTwoServersShare(t *testing.T) {
	t.Parallel()

	resolver := servernames.NewResolver([]servernames.ConfiguredServer{
		{
			ID:   chatServerID,
			Name: "Slack",
			Slug: "slack-support",
			Plugins: []servernames.PluginMembership{
				{PluginSlug: "support", DisplayName: "Slack"},
			},
		},
		{
			ID:   billingServerID,
			Name: "Slack",
			Slug: "slack-finance",
			Plugins: []servernames.PluginMembership{
				{PluginSlug: "finance", DisplayName: "Slack"},
			},
		},
	})

	// The shared display name identifies neither server...
	_, ok := resolver.Resolve("Slack")
	require.False(t, ok)

	// ...but each plugin-routed prefix and slug still identifies its own.
	id, ok := resolver.Resolve("plugin_support_Slack")
	require.True(t, ok)
	require.Equal(t, chatServerID, id)
	id, ok = resolver.Resolve("plugin_finance_Slack")
	require.True(t, ok)
	require.Equal(t, billingServerID, id)
	id, ok = resolver.Resolve("slack-finance")
	require.True(t, ok)
	require.Equal(t, billingServerID, id)

	// The shared spelling is excluded from both servers' telemetry filters.
	require.NotContains(t, resolver.ReportedNames(chatServerID), "Slack")
	require.NotContains(t, resolver.ReportedNames(billingServerID), "Slack")
	require.Contains(t, resolver.ReportedNames(chatServerID), "plugin_support_Slack")
	require.Contains(t, resolver.ReportedNames(chatServerID), "plugin_support_slack")
	require.Contains(t, resolver.ReportedNames(billingServerID), "plugin_finance_Slack")
}

func TestResolver_ReportedNamesCoverEverySpelling(t *testing.T) {
	t.Parallel()

	resolver := fixtureResolver()

	// Every spelling Resolve accepts is listed, as configured and lower-cased,
	// so the telemetry read can compare the indexed column by exact value.
	require.Equal(t, []string{
		chatServerID,
		"External Acme Chat",
		"External_Acme_Chat",
		"acme-chat",
		"external acme chat",
		"external_acme_chat",
		"plugin_acme-tools_External_Acme_Chat",
		"plugin_acme-tools_external_acme_chat",
	}, resolver.ReportedNames(chatServerID))

	require.Nil(t, resolver.ReportedNames("0192a3b4-0000-7000-8000-00000000ffff"))
}

func TestResolver_NamesServersByNameThenSlugThenID(t *testing.T) {
	t.Parallel()

	resolver := servernames.NewResolver([]servernames.ConfiguredServer{
		{ID: "id-named", Name: "Named", Slug: "named-slug"},
		{ID: "id-slug-only", Slug: "slug-only"},
		{ID: "id-bare"},
	})

	require.Equal(t, "Named", resolver.Name("id-named"))
	require.Equal(t, "slug-only", resolver.Name("id-slug-only"))
	require.Equal(t, "id-bare", resolver.Name("id-bare"))
	require.Equal(t, "unknown", resolver.Name("unknown"))
}

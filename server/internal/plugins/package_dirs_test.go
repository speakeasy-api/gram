package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginSlugPackageCollision(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		slug   string
		others []string
		dir    string
		holder string
	}{
		{name: "codex package of an existing plugin", slug: "foo-codex", others: []string{"foo"}, dir: "foo-codex/", holder: "plugin foo"},
		{name: "existing plugin named after the codex package", slug: "foo", others: []string{"foo-codex"}, dir: "foo-codex/", holder: "plugin foo-codex"},
		{name: "cursor package root", slug: "cursor-plugins", dir: "cursor-plugins/", holder: "the marketplace's Cursor packages"},
		{name: "agent plugins root", slug: "agent-plugins", dir: "agent-plugins/", holder: "the marketplace's Agent Plugins packages"},
		{name: "platform plugin", slug: "speakeasy", dir: "speakeasy/", holder: "the Speakeasy Platform MCP plugin"},
		{name: "platform codex package", slug: "speakeasy-codex", dir: "speakeasy-codex/", holder: "the Speakeasy Platform MCP plugin"},
		{name: "observability plugin", slug: "acme-observability", dir: "acme-observability/", holder: "the observability plugin"},
		{name: "observability opencode package", slug: "acme-observability-opencode", dir: "acme-observability-opencode/", holder: "the observability plugin"},
		{name: "observability under the published org name", slug: "old-acme-observability", dir: "old-acme-observability/", holder: "the observability plugin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := pluginSlugPackageCollision(test.slug, test.others, []string{"Acme", "Old Acme"})
			require.ErrorIs(t, err, ErrPluginSlugUnavailable)
			require.Equal(t, "invalid slug: "+test.slug+" would publish into "+test.dir+", which "+test.holder+" already uses; choose a different slug", err.Error())
		})
	}

	for _, test := range []struct {
		name   string
		slug   string
		others []string
	}{
		{name: "unrelated plugins", slug: "support", others: []string{"sales", "sales-tools"}},
		{name: "same slug is left to the unique check", slug: "foo", others: []string{"foo"}},
		{name: "cursor suffix only names the cursor package", slug: "foo-cursor", others: []string{"foo"}},
		{name: "a plugin named observability", slug: "observability", others: nil},
		{name: "another org's observability name", slug: "other-observability", others: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, pluginSlugPackageCollision(test.slug, test.others, []string{"Acme"}))
		})
	}
}

// A project may already hold plugins whose packages share a directory. That
// directory belongs to neither, in whichever order the plugins arrive, while
// their other directories keep their owners.
func TestOwnersOfPluginsSharedDirectoryBelongsToNeither(t *testing.T) {
	t.Parallel()

	foo := PluginInfo{Name: "Foo", Slug: "foo", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil}
	fooCodex := PluginInfo{Name: "Foo Codex", Slug: "foo-codex", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil}
	for _, order := range [][]PluginInfo{{foo, fooCodex}, {fooCodex, foo}} {
		owners := ownersOfPlugins(order)

		_, ok := owners.ownerOf("foo-codex/.mcp.json")
		require.False(t, ok, "both plugins write into the shared directory")

		owner, ok := owners.ownerOf("foo/.mcp.json")
		require.True(t, ok)
		require.Equal(t, "foo", owner)
		owner, ok = owners.ownerOf("cursor-plugins/foo-cursor/mcp.json")
		require.True(t, ok)
		require.Equal(t, "foo", owner)
		owner, ok = owners.ownerOf("foo-codex-codex/.mcp.json")
		require.True(t, ok)
		require.Equal(t, "foo-codex", owner)
	}
}

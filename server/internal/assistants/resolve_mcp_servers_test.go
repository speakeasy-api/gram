package assistants

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

func TestResolveAssistantMCPServers(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	assistants := platformtools.NewAssistantsToolset()
	platformToolsets := []platformtools.Toolset{assistants}

	t.Run("empty user toolsets emit only platform toolsets", func(t *testing.T) {
		t.Parallel()

		servers, err := resolveAssistantMCPServers(serverURL, nil, platformToolsets)
		require.NoError(t, err)
		require.Len(t, servers, 1)

		assert.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[0].ID)
		assert.Equal(
			t,
			platformtools.PlatformToolsetURL(serverURL, platformtools.AssistantsPlatformToolsetSlug),
			servers[0].URL,
		)
		assert.Empty(t, servers[0].Headers)
	})

	t.Run("user toolsets are emitted before platform toolsets", func(t *testing.T) {
		t.Parallel()

		rows := []assistantToolsetRow{
			{
				ToolsetSlug:     "billing",
				McpEnabled:      true,
				McpSlug:         pgtype.Text{String: "billing-mcp", Valid: true},
				EnvironmentSlug: pgtype.Text{String: "prod", Valid: true},
			},
		}

		servers, err := resolveAssistantMCPServers(serverURL, rows, platformToolsets)
		require.NoError(t, err)
		require.Len(t, servers, 2)

		assert.Equal(t, "billing", servers[0].ID)
		assert.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)
		assert.Equal(t, "prod", servers[0].Headers["Gram-Environment"])

		assert.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[1].ID)
	})

	t.Run("nil platform toolsets produces no platform servers", func(t *testing.T) {
		t.Parallel()

		servers, err := resolveAssistantMCPServers(serverURL, nil, nil)
		require.NoError(t, err)
		require.Empty(t, servers)
	})
}

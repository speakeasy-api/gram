package assistants

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

func TestResolveAssistantMCPServers_AppendsPlatformServerForEveryAssistant(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	t.Run("empty user toolsets still gets platform server", func(t *testing.T) {
		t.Parallel()

		servers, err := resolveAssistantMCPServers(serverURL, nil)
		require.NoError(t, err)
		require.Len(t, servers, 1)

		assert.Equal(t, assistantPlatformMCPServerID, servers[0].ID)
		assert.Equal(
			t,
			"https://gram.test/x/platform-mcp/"+platformtools.AssistantsPlatformToolsetSlug,
			servers[0].URL,
		)
		assert.Empty(t, servers[0].Headers)
	})

	t.Run("user toolsets are listed before the platform server", func(t *testing.T) {
		t.Parallel()

		rows := []assistantToolsetRow{
			{
				ToolsetSlug:     "billing",
				McpEnabled:      true,
				McpSlug:         pgtype.Text{String: "billing-mcp", Valid: true},
				EnvironmentSlug: pgtype.Text{String: "prod", Valid: true},
			},
		}

		servers, err := resolveAssistantMCPServers(serverURL, rows)
		require.NoError(t, err)
		require.Len(t, servers, 2)

		assert.Equal(t, "billing", servers[0].ID)
		assert.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)
		assert.Equal(t, "prod", servers[0].Headers["Gram-Environment"])

		assert.Equal(t, assistantPlatformMCPServerID, servers[1].ID)
		assert.Equal(
			t,
			"https://gram.test/x/platform-mcp/"+platformtools.AssistantsPlatformToolsetSlug,
			servers[1].URL,
		)
	})
}

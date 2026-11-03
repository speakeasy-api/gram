package assistants

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/stretchr/testify/require"
)

func TestKitBuiltinDefaultThreadTargetIsPerTurn(t *testing.T) {
	t.Parallel()
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range ids {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			prompt, err := kitProtocolPrompt(runTurnRequest{ThreadID: id, Prompt: "Use a built-in tool"})
			require.NoError(t, err)
			require.Len(t, prompt, 2)
			require.Contains(t, prompt[0]["text"], id.String())
			require.NotContains(t, prompt[0]["text"], ids[1-i].String())
			require.Equal(t, "Use a built-in tool", prompt[1]["text"])
		})
	}
}

func TestKitBuiltinConnectionsUseSeparateResourceBoundWorkloadTokens(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example")
	require.NoError(t, err)
	bootstrap := kitTestWorkloadBootstrap()
	slugs := []string{platformtools.AssistantsPlatformToolsetSlug, platformtools.ManagedAssistantPlatformToolsetSlug, platformtools.PlatformMCPReadToolsetSlug}
	connections := kitBuiltinMCPConnections(base, bootstrap.Workload.Identity.ProjectID, slugs)
	config := KitLocalCompositionConfig{mcpConnections: connections}
	bootstrap.MCPJSON, err = kitCompositionMCP(config, bootstrap.Workload)
	require.NoError(t, err)
	require.NoError(t, validateKitBootstrap(bootstrap))
	var parsed struct {
		Servers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Auth    struct {
				Resource      string            `json:"resource"`
				TokenEndpoint string            `json:"tokenEndpoint"`
				Extensions    map[string]string `json:"extensions"`
			} `json:"auth"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(bootstrap.MCPJSON, &parsed))
	require.Len(t, parsed.Servers, len(slugs))
	for _, slug := range slugs {
		server := parsed.Servers["builtin-"+slug]
		require.Equal(t, platformtools.PlatformToolsetURL(base, slug), server.URL)
		require.Equal(t, server.URL, server.Auth.Resource)
		require.Equal(t, bootstrap.Workload.Audience+"/token", server.Auth.TokenEndpoint)
		require.Equal(t, map[string]string{"project_id": bootstrap.Workload.Identity.ProjectID.String()}, server.Auth.Extensions)
		require.Empty(t, server.Headers)
	}
	require.NotContains(t, string(bootstrap.MCPJSON), "urn:speakeasy:inference")
	require.NotContains(t, string(bootstrap.MCPJSON), string(bootstrap.GatewayToken))
	// A different project in deployment-provided exchange parameters is not a
	// valid bootstrap for this workload identity.
	config.mcpConnections[0].ProjectID = uuid.New()
	bootstrap.MCPJSON, err = kitCompositionMCP(config, bootstrap.Workload)
	require.NoError(t, err)
	require.ErrorContains(t, validateKitBootstrap(bootstrap), "workload project")
}

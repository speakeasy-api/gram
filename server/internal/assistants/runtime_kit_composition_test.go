package assistants

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestKitCompositionMachineConfigUsesFiles(t *testing.T) {
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	issuer, _ := url.Parse("https://issuer.example/oauth/project")
	raw, files, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource, MCPIssuer: issuer}, KitMCPProvisionResult{ClientID: "registered-client", Secret: "never-in-configuration"})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "never-in-configuration")
	require.Equal(t, []byte("never-in-configuration"), files["mcp-workload-secret"])
	var config map[string]any
	require.NoError(t, json.Unmarshal(raw, &config))
	auth := config["mcpServers"].(map[string]any)["workload"].(map[string]any)["auth"].(map[string]any)
	require.Equal(t, "client_credentials", auth["type"])
	require.Equal(t, resource.String(), auth["resource"])
	require.Equal(t, issuer.String(), auth["expected_issuer"])
	require.Equal(t, []any{"mcp:connect"}, auth["scopes"])
	require.Equal(t, "/home/kit/.kit/credentials/mcp-workload-secret", auth["method"].(map[string]any)["client_secret_file"])
	require.NotContains(t, string(raw), "Authorization")
}

func TestKitCompositionRejectsInvalidConfig(t *testing.T) {
	origin, _ := url.Parse("https://api.example")
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	issuer, _ := url.Parse("https://issuer.example")
	valid := KitLocalCompositionConfig{Environment: "local", Image: "image:tag", Origin: origin, MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: uuid.New(), TurnStream: new(chat.TurnStream), ProvisionMCP: func(_ context.Context, _ KitMCPProvisionRequest) (KitMCPProvisionResult, error) {
		return KitMCPProvisionResult{}, nil
	}}
	require.NoError(t, validateKitLocalComposition(valid))
	for _, mutate := range []func(*KitLocalCompositionConfig){
		func(c *KitLocalCompositionConfig) { c.Environment = "production" },
		func(c *KitLocalCompositionConfig) { c.Image = "" },
		func(c *KitLocalCompositionConfig) { c.ProvisionMCP = nil },
		func(c *KitLocalCompositionConfig) { c.MCPUserSessionIssuerID = uuid.Nil },
		func(c *KitLocalCompositionConfig) { c.MCPResource, _ = url.Parse("http://mcp.example/mcp/resource") },
		func(c *KitLocalCompositionConfig) {
			c.MCPResource, _ = url.Parse("https://mcp.example/mcp/platform/tools")
		},
		func(c *KitLocalCompositionConfig) { c.Origin, _ = url.Parse("http://api.example") },
	} {
		c := valid
		mutate(&c)
		require.Error(t, validateKitLocalComposition(c))
	}
	c := valid
	c.Origin, _ = url.Parse("http://api.example")
	c.AllowInsecureInference = true
	require.NoError(t, validateKitLocalComposition(c))
}

func TestKitCompositionRequiresExplicitAttachedResource(t *testing.T) {
	resource, _ := url.Parse("https://mcp.example/mcp/attached")
	a := assistantRecord{MCPServers: []assistantMCPServerRow{{EndpointSlug: "attached", Visibility: "private"}}}
	require.True(t, kitResourceAttached(a, resource))
	a.MCPServers[0].Visibility = "disabled"
	require.False(t, kitResourceAttached(a, resource))
	a.MCPServers[0].Visibility = "private"
	a.MCPServers[0].EndpointSlug = "other"
	require.False(t, kitResourceAttached(a, resource))
	_, _, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource, MCPIssuer: resource}, KitMCPProvisionResult{})
	require.Error(t, err)
}

package assistants

import (
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestKitCompositionMachineConfigUsesAssertions(t *testing.T) {
	t.Parallel()
	resource, _ := url.Parse("https://mcp.example/mcp/gateway/test-org/projects/test-project")
	raw, err := kitCompositionMCP([]kitMCPConnection{{Name: "project-gateway", Resource: resource}}, &KitWorkloadBinding{Audience: "https://issuer.example/oauth/usi/test"})
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"project-gateway":{"headers":{},"type":"streamable-http","url":"https://mcp.example/mcp/gateway/test-org/projects/test-project","auth":{"type":"workload_identity","tokenFile":"/var/run/secrets/speakeasy/workload/assertion","endpointMode":"explicit","grant":"jwtBearer","tokenEndpoint":"https://issuer.example/oauth/usi/test/token","resource":"https://mcp.example/mcp/gateway/test-org/projects/test-project","scope":"mcp:connect"}}}}`, string(raw))
	require.NotContains(t, string(raw), "client_id")
}

func TestKitCompositionRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	origin, _ := url.Parse("https://ai.speakeasy.com")
	valid := KitLocalCompositionConfig{Environment: "local", Image: "image:tag", Network: "isolated-test", ExtraCACertFile: "/test/ca.pem", Origin: origin, TurnStream: new(chat.TurnStream), WorkloadJWKSURL: "https://external.example/jwks", WorkloadIssuerURL: "https://external.example", WorkloadIssuerBaseURL: "https://issuer.example", WorkloadSubject: "operator-subject", WorkloadAssertionFile: "/run/assertion"}

	require.NoError(t, validateKitLocalComposition(valid))
	for _, mutate := range []func(*KitLocalCompositionConfig){
		func(c *KitLocalCompositionConfig) { c.Environment = "production" },
		func(c *KitLocalCompositionConfig) { c.Image = "" },
		func(c *KitLocalCompositionConfig) { c.WorkloadJWKSURL = "" },
		func(c *KitLocalCompositionConfig) { c.WorkloadJWKSURL = "http://external.example/keys" },
		func(c *KitLocalCompositionConfig) { c.WorkloadIssuerURL = "" },
		func(c *KitLocalCompositionConfig) { c.WorkloadSubject = "" },
		func(c *KitLocalCompositionConfig) { c.WorkloadAssertionFile = "" },
		func(c *KitLocalCompositionConfig) { c.WorkloadIssuerBaseURL = "" },
		func(c *KitLocalCompositionConfig) { c.WorkloadIssuerBaseURL = "http://issuer.example" },
		func(c *KitLocalCompositionConfig) { c.Origin, _ = url.Parse("http://api.example") },
	} {
		c := valid
		mutate(&c)
		require.Error(t, validateKitLocalComposition(c))
	}
	c := valid
	c.Origin, _ = url.Parse("http://api.example")
	c.AllowInsecureInference = true
	require.Error(t, validateKitLocalComposition(c))
}

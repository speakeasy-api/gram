package assistants

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/stretchr/testify/require"
)

func kitTestWorkloadBootstrap() KitLocalBootstrap {
	workload := &KitWorkloadBinding{
		MCPProjectGateway: true,
		IssuerID:          uuid.New(),
		Subject:           "system:serviceaccount:test:kit-generation",
		Audience:          "https://issuer.example/oauth/usi/test",
		Identity:          assistantidentity.RuntimeIdentity{ProjectID: uuid.New()},
	}
	gatewayURL, _ := url.Parse("https://api.example/mcp/gateway/org-test/projects/proj-test")
	mcpJSON, err := kitCompositionMCP(gatewayURL, workload)
	if err != nil {
		panic(err)
	}
	return KitLocalBootstrap{Model: "openai/gpt-4.1-mini", AgentsMD: []byte("caller instructions"), GatewayToken: []byte("private-test-token"), MCPJSON: mcpJSON, AssertionFile: KitWorkloadAssertionPath, InferenceEndpoint: "https://ai.speakeasy.com/chat/completions", Workload: workload}
}

func TestKitLocalBootstrapContractValidation(t *testing.T) {
	t.Parallel()
	b := kitTestWorkloadBootstrap()
	require.NoError(t, validateKitBootstrap(b))
	for _, mutate := range []func(*KitLocalBootstrap){
		func(b *KitLocalBootstrap) { b.Model = "" },
		func(b *KitLocalBootstrap) {
			b.MCPJSON = []byte(`{"mcpServers":{}}`)
		},
		func(b *KitLocalBootstrap) {
			b.MCPJSON = []byte(`{"mcpServers":{"builtin-assistants":{"type":"streamable-http","url":"https://api.example/platform/mcp/assistants","headers":{},"auth":{"type":"workload_identity","tokenFile":"/var/run/secrets/speakeasy/workload/assertion","endpointMode":"explicit","grant":"jwtBearer","tokenEndpoint":"https://issuer.example/oauth/usi/test/token","resource":"https://api.example/platform/mcp/assistants","scope":"mcp:connect","extensions":{"project_id":"00000000-0000-0000-0000-000000000001"}}}}}`)
		},
		func(b *KitLocalBootstrap) {
			b.MCPJSON = []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://api.example/mcp/gateway/org/projects/p","headers":{},"auth":{"type":"workload_identity","tokenFile":"/var/run/secrets/speakeasy/workload/assertion","endpointMode":"explicit","grant":"jwtBearer","tokenEndpoint":"https://issuer.example/oauth/usi/test/token","resource":"https://api.example/mcp/gateway/org/projects/p","scope":"mcp:connect","extensions":{"project_id":"00000000-0000-0000-0000-000000000001"}}}}}`)
		},
		func(b *KitLocalBootstrap) {
			b.MCPJSON = []byte(`{"mcpServers":{"legacy":{"type":"streamable-http","url":"https://mcp.example/mcp/tools","auth":{"type":"client_credentials","client_secret":"forbidden"}}}}`)
		},
		func(b *KitLocalBootstrap) {
			b.MCPJSON = []byte(`{"mcpServers":{"legacy":{"type":"streamable-http","url":"https://mcp.example/mcp/tools","auth":{"type":"private_key_jwt","private_key_file":"/secret"}}}}`)
		},
		func(b *KitLocalBootstrap) { b.Workload = nil },
		func(b *KitLocalBootstrap) { b.AssertionFile = "relative" },
		func(b *KitLocalBootstrap) { b.InferenceEndpoint = "http://ai.speakeasy.com/chat/completions" },
		func(b *KitLocalBootstrap) { b.GatewayToken = []byte("invalid token") },
		func(b *KitLocalBootstrap) { b.MCPJSON = []byte(`{}`) },
	} {
		bad := b
		mutate(&bad)
		require.Error(t, validateKitBootstrap(bad))
	}
}

func TestKitWorkloadInferenceConfiguration(t *testing.T) {
	t.Parallel()
	b := kitTestWorkloadBootstrap()
	config := kitBootstrapConfig(b)
	for _, expected := range []string{`provider = "speakeasy"`, `model = "openai/gpt-4.1-mini"`, `[inference_providers.speakeasy.workload]`, `tokenFile = "/var/run/secrets/speakeasy/workload/assertion"`, `grant = "jwtBearer"`, `endpointMode = "explicit"`, `tokenEndpoint = "https://issuer.example/oauth/usi/test/token"`, `resource = "urn:speakeasy:inference"`, `project_id = "` + b.Workload.Identity.ProjectID.String() + `"`} {
		require.Contains(t, config, expected)
	}
	for _, forbidden := range []string{"api_key", "client_secret", "private_key_jwt"} {
		require.NotContains(t, config, forbidden)
	}
	require.NotContains(t, config, string(b.GatewayToken))
}

func TestKitLocalRequiresExplicitImage(t *testing.T) {
	t.Parallel()
	k, engine, _, _ := kitLocalFixture(t)
	for _, image := range []string{"kit", "registry.example/kit", "registry.example:5000/kit", "kit:latest", "kit:LATEST"} {
		config := k.config
		config.Image = image
		_, err := newKitLocalRuntimeBackend(config, engine)
		require.Error(t, err, image)
	}
	for _, image := range []string{"registry.example:5000/kit:0.2.19", "kit@sha256:" + strings.Repeat("a", 64)} {
		config := k.config
		config.Image = image
		_, err := newKitLocalRuntimeBackend(config, engine)
		require.NoError(t, err, image)
	}
}

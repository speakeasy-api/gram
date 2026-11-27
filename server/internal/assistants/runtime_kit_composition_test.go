package assistants

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/stretchr/testify/require"
)

func TestKitCompositionMachineConfigUsesAssertions(t *testing.T) {
	t.Parallel()
	gateway, _ := url.Parse("https://mcp.example/mcp/gateway/org-test/projects/proj-test")
	raw, err := kitCompositionMCP(gateway, &KitWorkloadBinding{Audience: "https://issuer.example/oauth/usi/test"})
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"project-gateway":{"headers":{},"type":"streamable-http","url":"https://mcp.example/mcp/gateway/org-test/projects/proj-test","auth":{"type":"workload_identity","tokenFile":"/var/run/secrets/speakeasy/workload/assertion","endpointMode":"explicit","grant":"jwtBearer","tokenEndpoint":"https://issuer.example/oauth/usi/test/token","resource":"https://mcp.example/mcp/gateway/org-test/projects/proj-test","scope":"mcp:connect"}}}}`, string(raw))
	require.NotContains(t, string(raw), "client_id")
	require.NotContains(t, string(raw), "builtin-")

	_, err = kitCompositionMCP(gateway, nil)
	require.Error(t, err)
	insecure, _ := url.Parse("http://mcp.example/mcp/gateway/org-test/projects/proj-test")
	_, err = kitCompositionMCP(insecure, &KitWorkloadBinding{Audience: "https://issuer.example/oauth/usi/test"})
	require.Error(t, err)
}

func TestKitCompositionRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	origin, _ := url.Parse("https://ai.speakeasy.com")
	valid := KitLocalCompositionConfig{
		Environment: "local", Image: "image:tag", Network: "isolated-test", ExtraCACertFile: "/test/ca.pem",
		Origin: origin, TurnStream: new(chat.TurnStream),
		WorkloadJWKSURL: "https://external.example/jwks", WorkloadIssuerURL: "https://external.example",
		WorkloadIssuerBaseURL: "https://issuer.example", WorkloadSubject: "operator-subject", WorkloadAssertionFile: "/run/assertion",
	}

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
		func(c *KitLocalCompositionConfig) { c.TurnStream = nil },
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

func TestKitAgentsMDAndTurnPromptUseProjectGatewayMode(t *testing.T) {
	t.Parallel()
	managedBase := RenderManagedAssistantInstructions(managedAssistantInstructions, MCPDeliveryProjectGateway)
	agentsMD := renderKitAgentsMD(managedBase)
	require.Contains(t, agentsMD, "`project-gateway`")
	require.Contains(t, agentsMD, "list_servers")
	require.Contains(t, agentsMD, "execute_read_only_tool")
	require.Contains(t, agentsMD, "execute_tool")
	require.Contains(t, agentsMD, "platform--<tool_name>")
	require.NotContains(t, agentsMD, "builtin-")
	require.NotContains(t, agentsMD, "mcp__p-assistants_skills_load")

	slackThread := assistantThreadRecord{
		ID:            uuid.New(),
		SourceKind:    sourceKindSlack,
		SourceRefJSON: []byte(`{"channel_id":"C123","thread_ts":"1700.1"}`),
	}
	skills := []assistantSkillSnapshot{{SkillID: uuid.New(), Name: "triage", Description: "Triage bugs"}}
	sourceInstructions, err := ComposeInstructionsForMode("", slackThread, skills, "", MCPDeliveryProjectGateway, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.AssistantSlackPlatformToolsetSlug,
	})
	require.NoError(t, err)
	require.Contains(t, sourceInstructions, "execute_read_only_tool with assistants--skills_load")
	require.Contains(t, sourceInstructions, "execute_tool with assistant-slack--")
	require.NotContains(t, sourceInstructions, "mcp__p-assistants_skills_load")
	require.NotContains(t, sourceInstructions, "builtin-")
}

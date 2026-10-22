package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestRedeployDeploymentToolManifest(t *testing.T) {
	t.Parallel()
	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "redeploy-manifest", Version: "1"}, nil))
	registerDeploymentRedeployTool(registrar, nil)

	descriptor := descriptorByName(t, registrar, redeployDeploymentToolName)
	require.Equal(t, "redeploy_deployment", descriptor.Name)
	require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
	require.Equal(t, externalOnly, descriptor.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
	require.Equal(t, discoveryProjectWrite, descriptor.Meta.DiscoveryScopes)
	require.NotNil(t, descriptor.Annotations)
	require.False(t, descriptor.Annotations.ReadOnlyHint)
	require.True(t, descriptor.Annotations.IdempotentHint)
	require.NotNil(t, descriptor.Annotations.DestructiveHint)
	require.True(t, *descriptor.Annotations.DestructiveHint)
	require.NotNil(t, descriptor.Annotations.OpenWorldHint)
	require.True(t, *descriptor.Annotations.OpenWorldHint)

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
	require.ElementsMatch(t, []string{"project_id", "idempotency_key", "confirmed"}, schema.Required)
	require.ElementsMatch(t, []string{"project_id", "deployment_id", "expected_active_deployment_id", "idempotency_key", "confirmed"}, mapKeys(schema.Properties))
}

func TestRedeployDeploymentToolUnavailableKeepsSchemaAndHidesSecrets(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "redeploy-live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "redeploy-unavailable", Version: "1"}, nil))
	service, _, _, _, _, _, _ := newRedeployServiceFixture(t)
	registerDeploymentRedeployTool(live, service)
	registerDeploymentRedeployTool(unavailable, nil)

	liveDescriptor := descriptorByName(t, live, redeployDeploymentToolName)
	unavailableDescriptor := descriptorByName(t, unavailable, redeployDeploymentToolName)
	require.JSONEq(t, string(liveDescriptor.InputSchema), string(unavailableDescriptor.InputSchema))
	require.Equal(t, liveDescriptor.Meta, unavailableDescriptor.Meta)
	require.Equal(t, liveDescriptor.Annotations, unavailableDescriptor.Annotations)

	_, err := unavailableDescriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001","deployment_id":"00000000-0000-0000-0000-000000000002","expected_active_deployment_id":"00000000-0000-0000-0000-000000000003","idempotency_key":"secret-looking-key","confirmed":true}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.NotContains(t, strings.ToLower(refusal.Payload), "secret-looking-key")
	require.NotContains(t, strings.ToLower(refusal.Payload), "token")
	require.Contains(t, refusal.Payload, "temporarily unavailable")
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

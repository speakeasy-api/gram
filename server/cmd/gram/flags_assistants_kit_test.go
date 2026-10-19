package gram

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKitRuntimeCompositionOptIn(t *testing.T) {
	origin, _ := url.Parse("https://api.example")
	c := newAssistantRuntimeCLIContext(t, map[string]string{"environment": "local", "assistant-runtime-provider": "local"})
	require.Empty(t, c.String("assistant-kit-image"))
	require.NoError(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, origin), "disabled composition must not touch dependencies or change legacy behavior")
}

func TestKitRuntimeCompositionRejectsNonLocalOptIn(t *testing.T) {
	origin, _ := url.Parse("https://api.example")
	c := newAssistantRuntimeCLIContext(t, map[string]string{"environment": "production", "assistant-runtime-provider": "local", "assistant-kit-image": "image:tag"})
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, origin), "local environment")
}

func TestKitRuntimeCompositionRequiresExplicitIssuer(t *testing.T) {
	origin, _ := url.Parse("https://api.example")
	c := newAssistantRuntimeCLIContext(t, map[string]string{"environment": "local", "assistant-runtime-provider": "local", "assistant-kit-image": "image:tag", "assistant-kit-mcp-resource": "https://mcp.example/mcp/resource", "assistant-kit-mcp-issuer": "https://issuer.example"})
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, origin), "assistant-kit-mcp-issuer-id")
}

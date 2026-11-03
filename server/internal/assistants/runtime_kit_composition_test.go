package assistants

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestKitCompositionMachineConfigUsesAssertions(t *testing.T) {
	t.Parallel()
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	raw, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource}, &KitWorkloadBinding{Audience: "https://issuer.example/oauth/usi/test"})
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"workload":{"headers":{},"type":"streamable-http","url":"https://mcp.example/mcp/resource","auth":{"type":"workload_identity","tokenFile":"/var/run/secrets/speakeasy/workload/assertion","endpointMode":"explicit","grant":"jwtBearer","tokenEndpoint":"https://issuer.example/oauth/usi/test/token","resource":"https://mcp.example/mcp/resource","scope":"mcp:connect"}}}}`, string(raw))
	require.NotContains(t, string(raw), "client_id")
}

func TestKitCompositionRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	origin, _ := url.Parse("https://ai.speakeasy.com")
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	issuer, _ := url.Parse("https://issuer.example")
	valid := KitLocalCompositionConfig{Environment: "local", Image: "image:tag", Network: "isolated-test", ExtraCACertFile: "/test/ca.pem", Origin: origin, MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: uuid.New(), TurnStream: new(chat.TurnStream), WorkloadJWKSURL: "https://external.example/jwks", WorkloadIssuerURL: "https://external.example", WorkloadIssuerBaseURL: "https://issuer.example", WorkloadSubject: "operator-subject", WorkloadAssertionFile: "/run/assertion"}

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
	require.Error(t, validateKitLocalComposition(c))
}

func TestKitCompositionRequiresExplicitAttachedResource(t *testing.T) {
	t.Parallel()
	resource, _ := url.Parse("https://mcp.example/mcp/attached")
	origin, _ := url.Parse("https://mcp.example")
	a := assistantRecord{MCPServers: []assistantMCPServerRow{{EndpointSlug: "attached", Visibility: "private"}}}
	require.True(t, kitResourceAttached(a, origin, resource))
	constructed := a.MCPServers[0].endpointURL(origin)
	require.Equal(t, "mcp/attached", constructed.Path)
	require.Equal(t, resource.String(), constructed.String())
	require.True(t, kitResourceAttached(a, origin, constructed))
	for _, raw := range []string{
		"https://other.example/mcp/attached",
		"https://mcp.example:8443/mcp/attached",
		"http://mcp.example/mcp/attached",
		"https://user@mcp.example/mcp/attached",
		"https://mcp.example/mcp/attached?redirect=1",
		"https://mcp.example/mcp/attached#fragment",
	} {
		other, err := url.Parse(raw)
		require.NoError(t, err)
		require.False(t, kitResourceAttached(a, origin, other), raw)
	}
	require.False(t, kitResourceAttached(a, nil, resource))
	require.False(t, kitResourceAttached(a, origin, nil))
	a.MCPServers[0].Visibility = "disabled"
	require.False(t, kitResourceAttached(a, origin, resource))
	a.MCPServers[0].Visibility = "private"
	a.MCPServers[0].EndpointSlug = "other"
	require.False(t, kitResourceAttached(a, origin, resource))
	a.MCPServers[0].EndpointSlug = ""
	require.False(t, kitResourceAttached(a, origin, a.MCPServers[0].endpointURL(origin)))
	_, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource, MCPIssuer: resource}, nil)
	require.Error(t, err)
}

// A custom-domain attachment matches its own host, never the platform alias.
func TestKitCompositionCustomDomainResource(t *testing.T) {
	t.Parallel()
	origin, _ := url.Parse("https://platform.example")
	a := assistantRecord{MCPServers: []assistantMCPServerRow{{EndpointSlug: "attached", EndpointDomain: "mcp.customer.example", Visibility: "private"}}}
	onDomain, _ := url.Parse("https://mcp.customer.example/mcp/attached")
	require.True(t, kitResourceAttached(a, origin, onDomain))
	constructed := a.MCPServers[0].endpointURL(origin)
	require.Equal(t, "mcp/attached", constructed.Path)
	require.Equal(t, onDomain.String(), constructed.String())
	require.True(t, kitResourceAttached(a, origin, constructed))
	onPlatform, _ := url.Parse("https://platform.example/mcp/attached")
	require.False(t, kitResourceAttached(a, origin, onPlatform))
}

func TestKitCompositionBootstrapRejectsUnattachedDisabledAndWrongOrigin(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "kit_bootstrap_attachment_gate")
	origin, err := url.Parse("https://mcp.example")
	require.NoError(t, err)
	f.core.serverURL = origin
	resource := origin.JoinPath("mcp", "team-remote")
	config := KitLocalCompositionConfig{
		MCPResource: resource,
	}
	runtime := assistantRuntimeRecord{ID: uuid.New(), ProjectID: f.projectID, AssistantID: f.assistant, RuntimeVersion: 3}
	assertRejected := func() {
		t.Helper()
		_, err := f.core.kitCompositionBootstrap(t.Context(), runtime, config)
		require.ErrorContains(t, err, "not an enabled attachment")
	}
	assertRejected()
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
	config.MCPResource, err = url.Parse("https://other.example/mcp/team-remote")
	require.NoError(t, err)
	assertRejected()
	config.MCPResource = resource
	f.remote.Visibility = "disabled"
	f.updateWrapper(t, f.remote)
	assertRejected()
}

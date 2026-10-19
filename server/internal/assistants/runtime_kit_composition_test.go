package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestKitCompositionMachineConfigUsesFiles(t *testing.T) {
	t.Parallel()
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	issuer, _ := url.Parse("https://issuer.example/oauth/project")
	raw, files, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource, MCPIssuer: issuer}, KitMCPProvisionResult{ClientID: "registered-client", Secret: "never-in-configuration"})
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"workload":{"type":"streamable-http","url":"https://mcp.example/mcp/resource","auth":{"type":"client_credentials","client_id":"registered-client","resource":"https://mcp.example/mcp/resource","expected_issuer":"https://issuer.example/oauth/project","scopes":["mcp:connect"],"method":{"type":"client_secret","client_secret_file":"/home/kit/.kit/credentials/mcp-workload-secret"}}}}}`, string(raw))
	require.NotContains(t, string(raw), "never-in-configuration")
	require.Equal(t, []byte("never-in-configuration"), files["mcp-workload-secret"])
	var config map[string]any
	require.NoError(t, json.Unmarshal(raw, &config))
	servers, ok := config["mcpServers"].(map[string]any)
	require.True(t, ok)
	workload, ok := servers["workload"].(map[string]any)
	require.True(t, ok)
	auth, ok := workload["auth"].(map[string]any)
	require.True(t, ok)
	method, ok := auth["method"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "client_credentials", auth["type"])
	require.Equal(t, resource.String(), auth["resource"])
	require.Equal(t, issuer.String(), auth["expected_issuer"])
	require.Equal(t, []any{"mcp:connect"}, auth["scopes"])
	require.Equal(t, "/home/kit/.kit/credentials/mcp-workload-secret", method["client_secret_file"])
	require.NotContains(t, string(raw), "Authorization")
}

func TestKitCompositionRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	origin, _ := url.Parse("https://ai.speakeasy.com")
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	issuer, _ := url.Parse("https://issuer.example")
	valid := KitLocalCompositionConfig{Environment: "local", Image: "image:tag", Network: "isolated-test", ExtraCACertFile: "/test/ca.pem", Origin: origin, MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: uuid.New(), TurnStream: new(chat.TurnStream), ProvisionMCP: func(_ context.Context, _ KitMCPProvisionRequest) (KitMCPProvisionResult, error) {
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
	require.Error(t, validateKitLocalComposition(c))
}

func TestKitCompositionRequiresExplicitAttachedResource(t *testing.T) {
	t.Parallel()
	resource, _ := url.Parse("https://mcp.example/mcp/attached")
	origin, _ := url.Parse("https://mcp.example")
	a := assistantRecord{MCPServers: []assistantMCPServerRow{{EndpointSlug: "attached", Visibility: "private"}}}
	require.True(t, kitResourceAttached(a, origin, resource))
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
	a.MCPServers[0].Visibility = "disabled"
	require.False(t, kitResourceAttached(a, origin, resource))
	a.MCPServers[0].Visibility = "private"
	a.MCPServers[0].EndpointSlug = "other"
	require.False(t, kitResourceAttached(a, origin, resource))
	_, _, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource, MCPIssuer: resource}, KitMCPProvisionResult{})
	require.Error(t, err)
}

func TestKitCompositionPrivateKeyUsesProtectedFile(t *testing.T) {
	t.Parallel()
	resource, _ := url.Parse("https://mcp.example/mcp/resource")
	issuer, _ := url.Parse("https://issuer.example/oauth/project")
	private := []byte("bootstrap-private-key-bytes")
	raw, files, err := kitCompositionMCP(KitLocalCompositionConfig{MCPResource: resource, MCPIssuer: issuer}, KitMCPProvisionResult{ClientID: "registered-client", AuthMethod: "private_key_jwt", PrivateKeyPEM: private})
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"workload":{"type":"streamable-http","url":"https://mcp.example/mcp/resource","auth":{"type":"client_credentials","client_id":"registered-client","resource":"https://mcp.example/mcp/resource","expected_issuer":"https://issuer.example/oauth/project","scopes":["mcp:connect"],"method":{"type":"private_key_jwt","private_key_file":"/home/kit/.kit/credentials/mcp-workload-private-key.pem","signing_algorithm":"ES256"}}}}}`, string(raw))
	require.NotContains(t, string(raw), string(private))
	require.Equal(t, map[string][]byte{"mcp-workload-private-key.pem": private}, files)
	k, _, runtime, _ := kitLocalFixture(t)
	bootstrap, err := k.config.Bootstrap(t.Context(), runtime)
	require.NoError(t, err)
	bootstrap.MCPJSON, bootstrap.MCPFiles = raw, files
	archive, err := kitLocalBootstrapArchive(bootstrap)
	require.NoError(t, err)
	reader := tar.NewReader(bytes.NewReader(archive))
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if header.Name == ".kit/credentials/mcp-workload-private-key.pem" {
			found = true
			require.Equal(t, int64(0600), header.Mode)
			require.Equal(t, 1000, header.Uid)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, private, data)
		}
	}
	require.True(t, found)
}

func TestKitCompositionRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()
	for _, client := range []KitMCPProvisionResult{
		{ClientID: "id", AuthMethod: "invalid", Secret: "secret"},
		{ClientID: "id", AuthMethod: "private_key_jwt"},
		{ClientID: "id", AuthMethod: "private_key_jwt", Secret: "secret", PrivateKeyPEM: []byte("key")},
		{ClientID: "id", AuthMethod: "client_secret_basic", Secret: "secret", PrivateKeyPEM: []byte("key")},
	} {
		_, _, err := kitCompositionMCP(KitLocalCompositionConfig{}, client)
		require.Error(t, err)
	}
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
		ProvisionMCP: func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error) {
			t.Fatal("rejected attachment must not provision credentials")
			return KitMCPProvisionResult{}, nil
		},
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

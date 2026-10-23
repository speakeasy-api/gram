package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
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

func TestKitCompositionBootstrapWithoutAttachments(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_composition_no_attachments")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "kit-composition")
	seedProjectRead(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Startup", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}))
	require.NoError(t, q.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant.ID, AssistantThreadID: uuid.Nil, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	row, err := q.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant.ID})
	require.NoError(t, err)
	core.assistantTokens = assistanttokens.New("test", db, nil, nil, nil)
	core.encryptionClient = testenv.NewEncryptionClient(t)
	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	config := KitLocalCompositionConfig{Environment: "local", Origin: origin, PersistedMCP: true, ProvisionMCP: func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error) {
		t.Fatal("per-turn MCP must not provision a singleton machine connection")
		return KitMCPProvisionResult{}, nil
	}}
	bootstrap, err := core.kitCompositionBootstrap(t.Context(), assistantRuntimeRecord{ID: row.ID, ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}, config)
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{}}`, string(bootstrap.MCPJSON))
	require.Empty(t, bootstrap.MCPFiles)
	require.NotEmpty(t, bootstrap.InferenceJSON)
	require.NotContains(t, string(bootstrap.AgentsMD), "unavailable in this runtime")
	// Additional roots must not turn bootstrap into a singleton query.
	for i := range 2 {
		tx := testenv.BeginTx(t, t.Context(), db)
		trigger, err := triggerrepo.New(tx).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{OrganizationID: "org-test", ProjectID: project, DefinitionSlug: sourceKindSlack, Name: fmt.Sprintf("Slack %d", i), TargetKind: "assistant", TargetRef: assistant.ID.String(), TargetDisplay: "Startup", ConfigJson: []byte(`{"event_types":["message"]}`), Status: StatusActive})
		require.NoError(t, err)
		require.NoError(t, core.identities.BindRootTrigger(t.Context(), tx, project, trigger.ID))
		require.NoError(t, tx.Commit(t.Context()))
		event := assistantThreadEventRecord{TriggerInstanceID: uuid.NullUUID{UUID: trigger.ID, Valid: true}}
		turnCtx := context.WithValue(t.Context(), kitExecutionEventKey{}, event)
		got, err := core.kitCompositionBootstrap(turnCtx, assistantRuntimeRecord{ID: row.ID, ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}, config)
		require.NoError(t, err)
		var credential kitInferenceFile
		require.NoError(t, json.Unmarshal(got.InferenceJSON, &credential))
		hash, err := auth.GetAPIKeyHash(credential.APIKey)
		require.NoError(t, err)
		key, err := keysrepo.New(db).GetAPIKeyByKeyHash(t.Context(), hash)
		require.NoError(t, err)
		binding, err := tokenrepo.New(db).ReadKitAPIKeyBinding(t.Context(), key.ID)
		require.NoError(t, err)
		var identity assistantidentity.Identity
		require.NoError(t, json.Unmarshal(binding.IdentityJson, &identity))
		expected, err := core.identities.Resolve(t.Context(), db, "org-test", project, assistant.ID, trigger.ID)
		require.NoError(t, err)
		require.Equal(t, *expected.Identity, identity)
	}

}

func TestKitCompositionUnavailableDiscoveryDoesNotGateTurn(t *testing.T) {
	t.Parallel()
	// No core or database: a discovery retry would panic before the protocol
	// can validate the turn. Nil means preserve existing MCP configuration.
	protocol := &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: &KitRuntimeProtocol{}}
	err := protocol.RunTurn(t.Context(), assistantRuntimeRecord{RuntimeVersion: 3}, runTurnRequest{MCPServers: nil})
	require.ErrorContains(t, err, "kit turn requires the persisted assistant model")
}

func TestKitMachineMCPConfigMultipleConnectionsWithoutHTTP(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer endpoint.Close()
	resource, err := url.Parse(endpoint.URL + "/mcp/unavailable")
	require.NoError(t, err)
	issuer, err := url.Parse(endpoint.URL + "/oauth")
	require.NoError(t, err)
	clients := map[string]KitMCPProvisionResult{
		"platform":       {Resource: resource, Issuer: issuer, ClientID: "platform-client", AuthMethod: "private_key_jwt", PrivateKeyPEM: []byte("platform-key")},
		"../../attached": {Resource: resource, Issuer: issuer, ClientID: "attached-client", AuthMethod: "private_key_jwt", PrivateKeyPEM: []byte("attached-key")},
	}
	raw, files, err := kitMachineMCPConfig(clients)
	require.NoError(t, err)
	require.Zero(t, requests.Load(), "configuration must not discover or authenticate MCP endpoints")
	var document struct {
		Servers map[string]struct {
			URL  string `json:"url"`
			Auth struct {
				Resource string `json:"resource"`
				Issuer   string `json:"expected_issuer"`
				Method   struct {
					Type string `json:"type"`
					File string `json:"private_key_file"`
				} `json:"method"`
			} `json:"auth"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &document))
	require.Len(t, document.Servers, 2)
	require.Len(t, files, 2)
	for name, client := range clients {
		server := document.Servers[name]
		require.Equal(t, resource.String(), server.URL)
		require.Equal(t, resource.String(), server.Auth.Resource)
		require.Equal(t, issuer.String(), server.Auth.Issuer)
		require.Equal(t, "private_key_jwt", server.Auth.Method.Type)
		require.Equal(t, "/home/kit/.kit/credentials", path.Dir(server.Auth.Method.File))
		require.Equal(t, client.PrivateKeyPEM, files[path.Base(server.Auth.Method.File)])
		require.NotContains(t, string(raw), string(client.PrivateKeyPEM))
	}
	// Neither zero connections nor nil input requires a singleton attachment.
	for _, clients := range []map[string]KitMCPProvisionResult{nil, {}} {
		raw, files, err := kitMachineMCPConfig(clients)
		require.NoError(t, err)
		require.JSONEq(t, `{"mcpServers":{}}`, string(raw))
		require.Empty(t, files)
	}
}

func TestKitMachineMCPConfigRejectsInvalidLocalAddresses(t *testing.T) {
	t.Parallel()
	valid, err := url.Parse("https://example.invalid/mcp")
	require.NoError(t, err)
	for _, raw := range []string{"", "http://example.invalid/mcp", "https://user:password@example.invalid/mcp", "https://example.invalid/mcp#fragment", "https:///mcp"} {
		invalid, err := url.Parse(raw)
		require.NoError(t, err)
		for _, addresses := range [][2]*url.URL{{nil, valid}, {valid, nil}, {invalid, valid}, {valid, invalid}} {
			_, _, err := kitMachineMCPConfig(map[string]KitMCPProvisionResult{"connection": {Resource: addresses[0], Issuer: addresses[1], ClientID: "client", AuthMethod: "private_key_jwt", PrivateKeyPEM: []byte("private-key")}})
			require.Error(t, err)
		}
	}
}

package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestKitLocalBootstrapContractValidation(t *testing.T) {
	t.Parallel()
	backend, _, r, _ := kitLocalFixture(t)
	good, err := backend.config.Bootstrap(t.Context(), r)
	require.NoError(t, err)
	require.NoError(t, validateKitBootstrap(good))
	for _, name := range []string{"../escape", "gateway-token", "AGENTS.md", "config.toml", KitInferenceCredentialFilename, "bad name"} {
		bad := good
		bad.MCPFiles = map[string][]byte{name: []byte("test")}
		require.Error(t, validateKitBootstrap(bad))
	}
	good.MCPFiles = map[string][]byte{"mcp-secret": []byte("test")}
	require.NoError(t, validateKitBootstrap(good))
	good.GatewayToken = []byte("newline\ntoken")
	require.Error(t, validateKitBootstrap(good))
	origin, _ := url.Parse("http://host.docker.internal:8080")
	config := DefaultKitLocalRuntimeConfig(origin)
	require.Equal(t, KitIntegrationImage, config.Image)
	require.NoError(t, validateKitGatewayCommand(config.Command, config.Args, config.GuestPort))
	require.Contains(t, config.Args, KitWorkspacePath)
	require.Contains(t, config.Args, "--allow-wildcard-bind")
}

func TestKitLocalStartupBootstrapRotatesCredential(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_startup_rotation")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "kit-startup-rotation")
	seedProjectRead(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Startup", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	trigger, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolved, err := core.identities.Resolve(t.Context(), db, "org-test", project, assistant.ID, trigger)
	require.NoError(t, err)
	require.NotNil(t, resolved.Identity)
	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}))
	require.NoError(t, q.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant.ID, AssistantThreadID: uuid.Nil, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	row, err := q.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant.ID})
	require.NoError(t, err)
	runtime := row.ID
	manager := assistanttokens.New("test", db, nil, nil, nil)
	in := KitStartupBootstrapInput{Identity: *resolved.Identity, RuntimeID: runtime, Lifetime: 24 * time.Hour, Origin: "https://api.example.invalid", AgentsMD: []byte("global instructions"), MCPJSON: []byte(`{"mcpServers":{}}`), GatewayToken: []byte("test-token")}
	first, id, err := PrepareKitStartupBootstrap(t.Context(), manager, in)
	require.NoError(t, err)
	second, next, err := PrepareKitStartupBootstrap(t.Context(), manager, in)
	require.NoError(t, err)
	require.NotEqual(t, id, next)
	require.NotEqual(t, string(first.InferenceJSON), string(second.InferenceJSON))
	var firstFile, secondFile kitInferenceFile
	require.NoError(t, json.Unmarshal(first.InferenceJSON, &firstFile))
	require.NoError(t, json.Unmarshal(second.InferenceJSON, &secondFile))
	firstHash := sha256.Sum256([]byte(firstFile.APIKey))
	secondHash := sha256.Sum256([]byte(secondFile.APIKey))
	tokens := tokenrepo.New(db)
	_, err = tokens.ReadInferenceCredential(t.Context(), firstHash[:])
	require.ErrorIs(t, err, pgx.ErrNoRows, "previous startup credential must no longer authorize")
	current, err := tokens.ReadInferenceCredential(t.Context(), secondHash[:])
	require.NoError(t, err)
	require.Equal(t, next, current.ID)
	firstKeyHash, err := auth.GetAPIKeyHash(firstFile.APIKey)
	require.NoError(t, err)
	secondKeyHash, err := auth.GetAPIKeyHash(secondFile.APIKey)
	require.NoError(t, err)
	_, err = keysrepo.New(db).GetAPIKeyByKeyHash(t.Context(), firstKeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "rotation must revoke the native API key")
	activeKey, err := keysrepo.New(db).GetAPIKeyByKeyHash(t.Context(), secondKeyHash)
	require.NoError(t, err)
	require.Equal(t, next, activeKey.ID)
	require.Equal(t, "gram_local_", secondFile.APIKey[:11])
	require.NoError(t, validateKitBootstrap(second))
	ac := &contextvalues.AuthContext{APIKeyID: next.String(), ProjectID: &project, ActiveOrganizationID: "org-test"}
	authed := contextvalues.WithAuthenticatedActor(t.Context(), ac, urn.NewPrincipal(urn.PrincipalTypeAgent, resolved.Identity.AgentID.String()))
	trusted, managed, err := manager.AuthorizeKitAPIKey(authed, secondFile.APIKey)
	require.NoError(t, err)
	require.True(t, managed)
	require.True(t, assistanttokens.InferenceTranscriptOwned(trusted))
	_, managed, err = manager.AuthorizeKitAPIKey(authed, firstFile.APIKey)
	require.True(t, managed)
	require.Error(t, err, "a rotated credential cannot inherit the new key's authority")
	wrongActor := contextvalues.WithAuthenticatedActor(t.Context(), ac, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	_, managed, err = manager.AuthorizeKitAPIKey(wrongActor, secondFile.APIKey)
	require.True(t, managed)
	require.Error(t, err)
	require.NoError(t, manager.RevokeRuntimeInferenceCredentials(t.Context(), project, runtime))
	_, managed, err = manager.AuthorizeKitAPIKey(authed, secondFile.APIKey)
	require.True(t, managed, "revoked runtime credentials must not fall back to ordinary capture")
	require.Error(t, err)
}

// Explicit image smoke: native TLS authentication with a synthetic local response,
// canonical session creation, settlement observation and durable session close.
// The isolated internal network prevents production egress and paid inference.
func TestKitLocalExactImageGatewaySmoke(t *testing.T) {
	t.Parallel()
	if os.Getenv("SPEAKEASY_KIT_IMAGE_SMOKE") != "1" {
		t.Skip("set SPEAKEASY_KIT_IMAGE_SMOKE=1 to exercise the pinned local Kit image") //nolint:forbidigo // Opt-in smoke requires a locally built image and running container engine.
	}
	image := os.Getenv("SPEAKEASY_KIT_SMOKE_IMAGE")
	if image == "" {
		image = KitIntegrationImage
	}
	fixture := newKitNativeCatalogFixture(t, image)
	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	config := DefaultKitLocalRuntimeConfig(origin)
	config.Image = image
	config.Network = fixture.network
	config.ExtraCACertFile = fixture.caFile
	config.Bootstrap = func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error) {
		raw, _ := json.Marshal(kitInferenceFile{APIKey: "gram_local_" + strings.Repeat("a", 64), Project: "smoke"})
		return KitLocalBootstrap{AgentsMD: []byte("Smoke-test assistant. Do not call tools."), InferenceJSON: raw, MCPJSON: []byte(`{"mcpServers":{}}`), GatewayToken: []byte("kit-smoke-token")}, nil
	}
	config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &fakeKitLocalProtocol{}, nil
	}
	var client *kitacp.Client
	httpClient := kitProtocolTestHTTPClient(t)
	config.Ready = func(ctx context.Context, _ assistantRuntimeRecord, endpoint *url.URL) error {
		deadline := time.Now().Add(20 * time.Second)
		for {
			var err error
			client, _, err = kitacp.Dial(ctx, endpoint.String(), "kit-smoke-token", httpClient, json.RawMessage(`{"protocolVersion":2,"info":{"name":"provisioning-smoke","version":"1"},"capabilities":{}}`))
			if err == nil {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("wait for kit smoke gateway: %w", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	backend, err := NewKitLocalRuntimeBackend(config)
	require.NoError(t, err)
	runtime := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), RuntimeVersion: 3, Backend: runtimeBackendLocal}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if client != nil {
			_ = client.CloseTransport(ctx)
		}
		require.NoError(t, backend.Stop(ctx, runtime))
		require.NoError(t, backend.ReapStoppedMachine(ctx, runtime))
	})
	result, err := backend.Ensure(t.Context(), runtime)
	require.NoError(t, err)
	runtime.BackendMetadataJSON = result.BackendMetadataJSON
	endpoint, err := backend.endpoint(t.Context(), runtime)
	require.NoError(t, err)
	unauthenticated, err := http.Get(endpoint.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, unauthenticated.StatusCode)
	_ = unauthenticated.Body.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	raw, err := client.Call(ctx, "session/new", map[string]any{"cwd": KitWorkspacePath, "mcpServers": []any{}, "_meta": map[string]string{"ai.speakeasy/gram-chat-id": uuid.NewString()}}, "")
	require.NoError(t, err)
	var session struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(raw, &session))
	require.NotEmpty(t, session.SessionID)
	require.NoError(t, client.AttachSession(ctx, session.SessionID))
	// This exact image discovers models from public OpenRouter, not an
	// authenticated Speakeasy catalog. Exercise its actual native-key path
	// against the isolated synthetic completion endpoint instead.
	_, err = client.Call(ctx, "session/prompt", map[string]any{"sessionId": session.SessionID, "prompt": []map[string]string{{"type": "text", "text": "Reply with smoke-ok. Do not call tools."}}}, session.SessionID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, err := client.Call(ctx, "kit/session/status", map[string]string{"sessionId": session.SessionID}, session.SessionID)
		if err != nil {
			return false
		}
		var state struct {
			Settled bool `json:"settled"`
		}
		return json.Unmarshal(status, &state) == nil && state.Settled
	}, 20*time.Second, 100*time.Millisecond, "synthetic completion must settle before close")
	_, err = client.Call(ctx, "session/close", map[string]string{"sessionId": session.SessionID}, session.SessionID)
	require.NoError(t, err)
	stats := fixture.stats(t)
	require.Positive(t, stats.Completions, "Kit must authenticate fixed HTTPS completion with native API key and project")
	require.Zero(t, stats.Rejected, "all provider requests must carry native authentication")
	require.Zero(t, stats.Unexpected, "session lifecycle must not call unexpected provider endpoints")
}

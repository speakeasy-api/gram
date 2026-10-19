package assistants

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestKitGKECompositionRejectsLocalAndMissingAuthority(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	resource, err := url.Parse("https://mcp.example/mcp/attached")
	require.NoError(t, err)
	issuer, err := url.Parse("https://mcp.example/oauth/issuer")
	require.NoError(t, err)
	valid := KitGKECompositionConfig{
		KitLocalCompositionConfig: KitLocalCompositionConfig{
			Environment: "production", Image: "registry.example/kit:0.2.17", Origin: origin,
			MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: uuid.New(),
			TurnStream: new(chat.TurnStream),
			ProvisionMCP: func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error) {
				return KitMCPProvisionResult{}, nil
			},
		},
		GKE: GKERuntimeConfig{Dynamic: fake.NewSimpleDynamicClient(runtime.NewScheme()), Namespace: "assistants", RunnerCIDRBlocks: []string{"10.0.0.0/16"}},
	}
	require.NoError(t, validateKitGKEComposition(valid))
	for _, tc := range []struct {
		name   string
		mutate func(*KitGKECompositionConfig)
	}{
		{"local", func(c *KitGKECompositionConfig) { c.Environment = "local" }},
		{"missing environment", func(c *KitGKECompositionConfig) { c.Environment = "" }},
		{"local network", func(c *KitGKECompositionConfig) { c.Network = "local-relay" }},
		{"local CA", func(c *KitGKECompositionConfig) { c.ExtraCACertFile = "ca.pem" }},
		{"HTTP inference", func(c *KitGKECompositionConfig) { c.AllowInsecureInference = true }},
		{"missing Kubernetes", func(c *KitGKECompositionConfig) { c.GKE.Dynamic = nil }},
		{"missing namespace", func(c *KitGKECompositionConfig) { c.GKE.Namespace = "" }},
		{"missing network bounds", func(c *KitGKECompositionConfig) { c.GKE.RunnerCIDRBlocks = nil }},
		{"missing issuer", func(c *KitGKECompositionConfig) { c.MCPUserSessionIssuerID = uuid.Nil }},
		{"missing provisioner", func(c *KitGKECompositionConfig) { c.ProvisionMCP = nil }},
		{"missing transcript", func(c *KitGKECompositionConfig) { c.TurnStream = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := valid
			tc.mutate(&c)
			require.Error(t, validateKitGKEComposition(c))
		})
	}
	core := new(ServiceCore)
	require.ErrorContains(t, core.InstallKitGKERuntime(valid), "guarded HTTP")
	require.Nil(t, core.runtime, "failed installation must not alter dispatch")
}

func TestKitGKEFencePreservesCanonicalSession(t *testing.T) {
	t.Parallel()
	core, db, key, chatID := kitSessionFixture(t)
	core.assistantTokens = assistanttokens.New("test", db, nil, nil, nil)
	session, err := core.StoreKitRuntimeSession(t.Context(), key, "persisted-kit-session")
	require.NoError(t, err)
	require.Equal(t, chatID, session.ChatID)
	old, err := core.EnsureKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "old-gateway-token")
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	require.NoError(t, core.fenceKitRuntimeAlreadyLocked(t.Context(), r))
	current, err := core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.NoError(t, err)
	require.NotEqual(t, old, current)
	restored, found, err := core.LoadKitRuntimeSession(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, session, restored, "checkpoint fencing must not retire canonical transcript/session authority")
	require.NoError(t, assistantrepo.New(db).StopAssistantRuntime(t.Context(), assistantrepo.StopAssistantRuntimeParams{
		ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, State: runtimeStateStopped,
		StartingState: runtimeStateStarting, ActiveState: runtimeStateActive, ExpiringState: runtimeStateExpiring,
	}))
	require.NoError(t, core.fenceKitRuntimeAlreadyLocked(t.Context(), r), "permanent cleanup must not resurrect retired gateway authority")
	_, err = core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.Error(t, err)
	r.RuntimeVersion = 2
	require.ErrorIs(t, core.fenceKitRuntimeAlreadyLocked(t.Context(), r), ErrKitRuntimeBinding)
}

func TestKitGKEExplicitVersionTagsRender(t *testing.T) {
	t.Parallel()
	for _, image := range []string{"registry.example/kit:0.2.17", "registry.example:5000/kit:release-1"} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			in := kitGKEInput()
			in.Image = image
			_, err := RenderKitGKEProvision(in)
			require.NoError(t, err)
		})
	}
	for _, image := range []string{"registry.example/kit", "registry.example/kit:latest", "registry.example/kit:LATEST"} {
		require.False(t, kitGKEExplicitImage(image), image)
	}
}

func TestKitGKERejectsMalformedOCIReferences(t *testing.T) {
	t.Parallel()
	for _, image := range []string{"bad!name:tag", "/kit:tag", "registry.example//kit:tag", "registry.example/kit:", "registry.example/Kit:tag", "registry.example/kit:latest", "registry.example/kit"} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			require.False(t, validKitGKEImage(image))
			in := kitGKEInput()
			in.Image = image
			_, err := RenderKitGKEProvision(in)
			require.Error(t, err)
		})
	}
	in := kitGKEInput()
	in.MCPFiles = map[string][]byte{"speakeasy.json": []byte("must-not-shadow-inference")}
	_, err := RenderKitGKEProvision(in)
	require.Error(t, err)
}

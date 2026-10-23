package assistants

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
		{"invalid network bounds", func(c *KitGKECompositionConfig) { c.GKE.RunnerCIDRBlocks = []string{"10.0.0.0/16", "garbage"} }},
		{"missing network bounds", func(c *KitGKECompositionConfig) { c.GKE.RunnerCIDRBlocks = nil }},
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
	for _, image := range []string{"registry.example/kit:0.2.17", "registry.example:5000/kit:release-1", "kit:latest@sha256:" + strings.Repeat("a", 64), "kit:LATEST@sha256:" + strings.Repeat("b", 64)} {
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
	for _, image := range []string{"bad!name:tag", "/kit:tag", "registry.example//kit:tag", "registry.example/kit:", "registry.example/Kit:tag", "registry.example/kit:latest", "registry.example/kit", "kit:latest@sha256:short"} {
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

func TestKitGKELifecycleReloadsAndCommitsRecoveryEvidence(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	err := assistantrepo.New(db).SetAssistantRuntimeBackendForTest(t.Context(), assistantrepo.SetAssistantRuntimeBackendForTestParams{Backend: runtimeBackendGKE, RuntimeID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	stale := assistantRuntimeRecord{ID: key.RuntimeID, AssistantID: key.AssistantID, ProjectID: key.ProjectID, RuntimeVersion: 3, Backend: runtimeBackendGKE}
	failure := errors.New("provisioning interrupted")
	progress := []byte(`{"recovery":"required"}`)
	err = core.kitGKELifecycle(t.Context(), stale, func(ctx context.Context, current assistantRuntimeRecord) error {
		require.NotNil(t, current.LifecycleTx)
		require.False(t, current.Retired)
		require.Equal(t, runtimeBackendGKE, current.Backend)
		writer, ok := ctx.Value(kitAdmissionProgressKey{}).(kitAdmissionProgressWriter)
		require.True(t, ok)
		require.NoError(t, writer(ctx, progress))
		return failure
	})
	require.ErrorIs(t, err, failure)
	row, err := assistantrepo.New(db).GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	require.JSONEq(t, string(progress), string(row.BackendMetadataJson))
	err = assistantrepo.New(db).EndAssistantRuntimeForTest(t.Context(), assistantrepo.EndAssistantRuntimeForTestParams{RuntimeID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	require.NoError(t, core.kitGKELifecycle(t.Context(), stale, func(_ context.Context, current assistantRuntimeRecord) error {
		require.True(t, current.Retired)
		require.JSONEq(t, string(progress), string(current.BackendMetadataJSON))
		return nil
	}))
}

func TestKitGKEEligibilityIndependentOfAttachments(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "kit_attachment_parity")
	a := assistantRecord{ID: f.assistant, ProjectID: f.projectID, OrganizationID: "org-test"}
	check := func(want bool) {
		t.Helper()
		tx := testenv.BeginTx(t, t.Context(), f.db)
		defer func() { _ = tx.Rollback(t.Context()) }()
		eligible, err := f.core.kitGKEAssistantEligible(t.Context(), tx, a, KitLocalCompositionConfig{PersistedMCP: true})
		require.NoError(t, err)
		require.Equal(t, want, eligible)
	}
	check(true) // No attachments: inference and platform tools remain available.
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
	check(true) // No configured issuer: connection failure is not startup failure.
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
	check(true) // Multiple connections do not change eligibility.
	f.remote.Visibility = "disabled"
	f.updateWrapper(t, f.remote)
	check(true)
	a.OrganizationID = "another-org"
	check(false)
}

package assistants

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
		KitGKEIdentityConfig: testKitGKEIdentityConfig(),
		KitLocalCompositionConfig: KitLocalCompositionConfig{
			Environment: "production", Image: "registry.example/kit:0.2.17", Origin: origin,
			MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: uuid.New(),
			TurnStream:      new(chat.TurnStream),
			WorkloadJWKSURL: "https://external.example/keys", WorkloadIssuerURL: "https://external.example", WorkloadIssuerBaseURL: "https://issuer.example",
		},
		GKE: GKERuntimeConfig{Dynamic: fake.NewSimpleDynamicClient(runtime.NewScheme()), Namespace: "assistants", RunnerCIDRBlocks: []string{"10.0.0.0/16"}},
	}
	valid = kitGKENativeIssuerConfig(valid)
	require.NoError(t, validateKitGKEComposition(valid))
	for _, tc := range []struct {
		name   string
		mutate func(*KitGKECompositionConfig)
	}{
		{"local", func(c *KitGKECompositionConfig) { c.Environment = "local" }},
		{"missing JWKS", func(c *KitGKECompositionConfig) { c.WorkloadJWKSURL = "" }},
		{"insecure JWKS", func(c *KitGKECompositionConfig) { c.WorkloadJWKSURL = "http://external.example/keys" }},
		{"missing environment", func(c *KitGKECompositionConfig) { c.Environment = "" }},
		{"local network", func(c *KitGKECompositionConfig) { c.Network = "local-relay" }},
		{"local CA", func(c *KitGKECompositionConfig) { c.ExtraCACertFile = "ca.pem" }},
		{"HTTP inference", func(c *KitGKECompositionConfig) { c.AllowInsecureInference = true }},
		{"missing Kubernetes", func(c *KitGKECompositionConfig) { c.GKE.Dynamic = nil }},
		{"missing namespace", func(c *KitGKECompositionConfig) { c.GKE.Namespace = "" }},
		{"invalid network bounds", func(c *KitGKECompositionConfig) { c.GKE.RunnerCIDRBlocks = []string{"10.0.0.0/16", "garbage"} }},
		{"missing network bounds", func(c *KitGKECompositionConfig) { c.GKE.RunnerCIDRBlocks = nil }},
		{"missing issuer", func(c *KitGKECompositionConfig) { c.MCPUserSessionIssuerID = uuid.Nil }},
		{"missing workload issuer", func(c *KitGKECompositionConfig) { c.WorkloadIssuerURL = "" }},
		{"missing transcript", func(c *KitGKECompositionConfig) { c.TurnStream = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := valid
			tc.mutate(&c)
			require.Error(t, validateKitGKEComposition(c))
		})
	}
	for _, tc := range []struct {
		name      string
		mutate    func(*KitGKECompositionConfig)
		wantError string
	}{
		{"missing project", func(c *KitGKECompositionConfig) { c.ProjectNumber = "" }, "numeric project number"},
		{"missing organization", func(c *KitGKECompositionConfig) { c.OrganizationID = "" }, "numeric project number"},
		{"invalid project", func(c *KitGKECompositionConfig) { c.ProjectNumber = "invalid/project" }, "numeric project number"},
		{"invalid cluster", func(c *KitGKECompositionConfig) { c.ClusterName = "invalid/cluster" }, "valid cluster location"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := valid
			tc.mutate(&c)
			require.ErrorContains(t, validateKitGKEComposition(c), tc.wantError)
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
	// Retirement must not bypass validation of supplied workload authority.
	r.BackendMetadataJSON = []byte(`{"workload":{"subject":"retired-binding"}}`)
	require.ErrorIs(t, core.fenceKitRuntimeAlreadyLocked(t.Context(), r), ErrKitRuntimeBinding)
	// This fixture has no workload binding; use its persisted metadata instead.
	r.BackendMetadataJSON = nil
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

func TestKitGKEEligibilityUsesPersistedAttachmentAndIssuer(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "kit_gke_eligibility")
	origin, err := url.Parse("https://mcp.example")
	require.NoError(t, err)
	f.core.serverURL = origin
	issuer, err := usersessionsrepo.New(f.db).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: f.projectID, Slug: "kit-workload", AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	c := KitLocalCompositionConfig{MCPResource: origin.JoinPath("mcp", "team-remote"), MCPUserSessionIssuerID: issuer.ID}
	a := assistantRecord{ID: f.assistant, ProjectID: f.projectID, OrganizationID: "org-test"}
	check := func(want bool) {
		t.Helper()
		tx := testenv.BeginTx(t, t.Context(), f.db)
		defer func() { _ = tx.Rollback(t.Context()) }()
		eligible, err := f.core.kitGKEAssistantEligible(t.Context(), tx, a, c)
		require.NoError(t, err)
		require.Equal(t, want, eligible)
	}
	check(false)
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
	check(false)
	err = assistantrepo.New(f.db).SetAttachedMCPServerIssuerForTest(t.Context(), assistantrepo.SetAttachedMCPServerIssuerForTestParams{UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, ID: f.remote.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	check(false) // Endpoint-mode issuers do not serve the shared authorization server.
	updated, err := testrepo.New(f.db).SetUserSessionIssuerAuthorizationServerModeFixture(t.Context(), testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{AuthorizationServerMode: "shared", IssuerID: issuer.ID, OrganizationID: a.OrganizationID, PinnedIssuerUrl: pgtype.Text{String: "", Valid: false}})
	require.NoError(t, err)
	require.EqualValues(t, 1, updated)
	check(true)
	c.MCPUserSessionIssuerID = uuid.New()
	check(false)
	c.MCPUserSessionIssuerID = issuer.ID
	c.MCPResource = origin.JoinPath("mcp", "another-project")
	check(false)
	// Production derives the connection from persisted rows, not deployment-
	// global issuer or resource flags. Stale flags cannot change its authority.
	c.PersistedMCP = true
	c.MCPResource = nil
	c.MCPUserSessionIssuerID = uuid.Nil
	check(true)
	err = assistantrepo.New(f.db).DisableAttachedMCPServerForTest(t.Context(), assistantrepo.DisableAttachedMCPServerForTestParams{ID: f.remote.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	check(false)
}

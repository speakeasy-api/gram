package assistants

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
)

func TestKitATEAssertionProjectionPath(t *testing.T) {
	t.Parallel()
	resource, err := url.Parse("https://tools.example.com/mcp/test")
	require.NoError(t, err)
	c := KitLocalCompositionConfig{MCPResource: resource}
	binding := &KitWorkloadBinding{Audience: "https://auth.example.com/oauth/test"}
	require.Equal(t, KitWorkloadAssertionPath, kitCompositionAssertionFile(c))
	legacy, err := kitCompositionMCP(c, binding)
	require.NoError(t, err)
	require.Contains(t, string(legacy), KitWorkloadAssertionPath)
	c.ateIdentity = &kitATEWorkloadIdentity{Atespace: "preview-one", ActorName: "actor-one"}
	require.Equal(t, "/var/run/speakeasy/workload/assertion", kitCompositionAssertionFile(c))
	ate, err := kitCompositionMCP(c, binding)
	require.NoError(t, err)
	require.Contains(t, string(ate), KitATEWorkloadAssertionPath)
	require.NotContains(t, string(ate), KitWorkloadAssertionPath)
}

func TestKitATEStoredWorkloadRecoveryValidation(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	c.Environment, c.nativeIdentity = "dev", nil
	c.WorkloadIssuerURL, c.WorkloadJWKSURL = "https://ATE-ISSUER.example.com:443/", "https://ate-issuer.example.com/jwks"
	c.ateIdentity = &kitATEWorkloadIdentity{Atespace: "preview-one", ActorName: "actor-one"}
	binding, err := core.provisionKitWorkload(t.Context(), r, c, identity, "actor-generation-one")
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), db)
	r.LifecycleTx = tx
	q := workloadrepo.New(tx)
	params := workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}}
	before, err := q.ListWorkloadAdmissions(t.Context(), params)
	require.NoError(t, err)
	require.NoError(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-one"))
	require.NoError(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-one"))
	after, err := q.ListWorkloadAdmissions(t.Context(), params)
	require.NoError(t, err)
	require.Len(t, after, len(before), "stored-workload validation must not change admission count")
	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-two"), ErrKitRuntimeBinding)
	foreign := *binding
	foreign.Subject = "actor/preview-one/other-actor"
	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, &foreign, identity, "actor-generation-one"), ErrKitRuntimeBinding)
	require.NoError(t, core.kitATERevoke(t.Context(), r, binding))
	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-one"), ErrKitRuntimeBinding, "withdrawn admissions cannot recover")
}

func TestKitATEIdleCheckpointCapability(t *testing.T) {
	t.Parallel()
	r := assistantRuntimeRecord{RuntimeVersion: 3, Backend: runtimeBackendATE}
	_, err := kitIdleCheckpointCapability(&KitATERuntimeBackend{}, r)
	require.NoError(t, err)
	r.RuntimeVersion = 2
	_, err = kitIdleCheckpointCapability(&KitATERuntimeBackend{}, r)
	require.Error(t, err)
	require.ErrorIs(t, (&ServiceCore{}).kitATECheckSettled(t.Context(), r), ErrKitRuntimeBinding)
}

func TestKitATEPreviewGate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, environment, preview string
		enabled, valid             bool
	}{
		{"off", "production", "", false, true},
		{"production", "production", "preview-one", true, false},
		{"local", "local", "preview-one", true, false},
		{"unnamed", "dev", "", true, false},
		{"implicit", "dev", "preview-one", false, false},
		{"malformed", "dev", "../preview", true, false},
		{"trailing-dash", "dev", "preview-", true, false},
		{"explicit", "dev", "preview-one", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateKitATEPreview(tt.environment, tt.enabled, tt.preview)
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestKitATEActorTokenIsolation(t *testing.T) {
	t.Parallel()
	first := kitATEActorToken("generation-one", "thread-one")
	require.Equal(t, first, kitATEActorToken("generation-one", "thread-one"))
	require.NotEqual(t, first, kitATEActorToken("generation-one", "thread-two"))
	require.NotEqual(t, first, kitATEActorToken("generation-two", "thread-one"))
	require.Len(t, first, 43)
}

func TestKitATEWorkloadSubject(t *testing.T) {
	t.Parallel()
	r := assistantRuntimeRecord{ID: uuid.New()}
	c := KitLocalCompositionConfig{Environment: "dev", ateIdentity: &kitATEWorkloadIdentity{Atespace: "preview-one", ActorName: "actor-one"}}
	subject, serviceAccount, err := kitWorkloadSubject(r, c, "actor-gateway-generation")
	require.NoError(t, err)
	require.Equal(t, "actor/preview-one/actor-one", subject)
	require.Empty(t, serviceAccount)
	c.Environment = "production"
	_, _, err = kitWorkloadSubject(r, c, "actor-gateway-generation")
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
}

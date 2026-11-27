package assistants

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func TestKitATEEgressInjectedBootstrapOmitsSandboxCredentials(t *testing.T) {
	t.Parallel()
	resource, err := url.Parse("https://tools.example.com/mcp/gateway/org/projects/proj")
	require.NoError(t, err)

	binding := &KitWorkloadBinding{
		MCPProjectGateway: true,
		IssuerID:          uuid.New(),
		Subject:           "actor/prod-space/actor-one",
		Audience:          "https://auth.example.com/oauth/test",
		Identity:          assistantidentity.RuntimeIdentity{ProjectID: uuid.New()},
	}

	ateMCP, err := kitProjectGatewayMCPJSON(resource, nil, nil)
	require.NoError(t, err)
	require.NotContains(t, string(ateMCP), KitWorkloadAssertionPath)
	require.NotContains(t, string(ateMCP), "tokenFile")
	require.NotContains(t, string(ateMCP), "workload_identity")
	require.Contains(t, string(ateMCP), resource.String())
	require.Contains(t, string(ateMCP), `"`+kitProjectGatewayServerName+`"`)

	bootstrap := KitLocalBootstrap{
		Model:              "openai/gpt-4.1-mini",
		ReasoningEffort:    "medium",
		Workload:           binding,
		AssertionFile:      "",
		EgressInjectedAuth: true,
		InferenceEndpoint:  "https://ai.speakeasy.com/chat/completions",
		AgentsMD:           []byte("# Assistant\n"),
		MCPJSON:            ateMCP,
		GatewayToken:       []byte(strings.Repeat("a", 43)),
	}
	require.NoError(t, validateKitBootstrap(bootstrap))

	cfg := kitBootstrapConfig(bootstrap)
	require.Contains(t, cfg, `endpoint = "https://ai.speakeasy.com/chat/completions"`)
	require.NotContains(t, cfg, "tokenFile")
	require.NotContains(t, cfg, "jwtBearer")

	// Egress-injected bootstrap must reject any assertion file path.
	bad := bootstrap
	bad.AssertionFile = "/var/run/speakeasy/workload/assertion"
	require.Error(t, validateKitBootstrap(bad))
}

func TestKitATEStoredWorkloadRecoveryValidation(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	c.Environment, c.nativeIdentity = "production", nil
	c.WorkloadIssuerURL, c.WorkloadJWKSURL = "https://ATE-ISSUER.example.com:443/", "https://ate-issuer.example.com/jwks"
	c.ateIdentity = &kitATEWorkloadIdentity{Atespace: "prod-space", ActorName: "actor-one"}

	binding, err := core.provisionKitWorkload(t.Context(), r, c, identity, "actor-generation-one")
	require.NoError(t, err)
	require.Equal(t, "actor/prod-space/actor-one", binding.Subject)

	tx := testenv.BeginTx(t, t.Context(), db)
	r.LifecycleTx = tx
	q := workloadrepo.New(tx)
	params := workloadrepo.ListWorkloadAdmissionsParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: r.ProjectID, Valid: true},
	}
	before, err := q.ListWorkloadAdmissions(t.Context(), params)
	require.NoError(t, err)

	require.NoError(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-one"))
	require.NoError(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-one"))

	after, err := q.ListWorkloadAdmissions(t.Context(), params)
	require.NoError(t, err)
	require.Len(t, after, len(before), "stored-workload validation must not change admission count")

	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-two"), ErrKitRuntimeBinding)

	foreign := *binding
	foreign.Subject = "actor/prod-space/other-actor"
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
	c := KitLocalCompositionConfig{
		Environment: "production",
		ateIdentity: &kitATEWorkloadIdentity{Atespace: "prod-space", ActorName: "actor-one"},
	}
	subject, serviceAccount, err := kitWorkloadSubject(r, c, "actor-gateway-generation")
	require.NoError(t, err)
	require.Equal(t, "actor/prod-space/actor-one", subject)
	require.Empty(t, serviceAccount)
}

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
		IssuerID:          uuid.Nil,
		Subject:           "actor/prod-space/actor-one",
		Audience:          "",
		Identity:          assistantidentity.RuntimeIdentity{ProjectID: uuid.New()},
	}

	ateMCP, err := kitProjectGatewayMCPJSON(resource, map[string]string{"Authorization": "Bearer " + kitATEEgressAuthPlaceholder}, nil)
	require.NoError(t, err)
	require.NotContains(t, string(ateMCP), KitWorkloadAssertionPath)
	require.NotContains(t, string(ateMCP), "tokenFile")
	require.NotContains(t, string(ateMCP), "workload_identity")
	require.Contains(t, string(ateMCP), resource.String())
	require.Contains(t, string(ateMCP), `"`+kitProjectGatewayServerName+`"`)
	require.Contains(t, string(ateMCP), "Bearer "+kitATEEgressAuthPlaceholder)

	// Egress-injected ATE bootstrap requires no workload issuer or token-exchange Audience.
	ateBinding := *binding
	bootstrap := KitLocalBootstrap{
		Model:              "openai/gpt-4.1-mini",
		ReasoningEffort:    "medium",
		Workload:           &ateBinding,
		AssertionFile:      "",
		EgressInjectedAuth: true,
		InferenceEndpoint:  "https://api.speakeasy.test/chat/completions",
		AgentsMD:           []byte("# Assistant\n"),
		MCPJSON:            ateMCP,
		GatewayToken:       []byte(strings.Repeat("a", 43)),
	}
	require.NoError(t, validateKitBootstrap(bootstrap))

	cfg := kitBootstrapConfig(bootstrap)
	require.Contains(t, cfg, `provider = "openrouter"`)
	require.NotContains(t, cfg, "tokenFile")
	require.NotContains(t, cfg, "jwtBearer")
	require.NotContains(t, cfg, "[inference_providers.speakeasy.workload]")

	// Egress-injected bootstrap must reject any assertion file path, workload issuer, or missing placeholder bearer header.
	bad := bootstrap
	bad.AssertionFile = "/var/run/speakeasy/workload/assertion"
	require.Error(t, validateKitBootstrap(bad))

	badIssuer := bootstrap
	issuerBinding := ateBinding
	issuerBinding.IssuerID = uuid.New()
	badIssuer.Workload = &issuerBinding
	require.Error(t, validateKitBootstrap(badIssuer))

	noAuthMCP, err := kitProjectGatewayMCPJSON(resource, nil, nil)
	require.NoError(t, err)
	badNoHeader := bootstrap
	badNoHeader.MCPJSON = noAuthMCP
	require.Error(t, validateKitBootstrap(badNoHeader))
}

func TestKitATEStoredWorkloadRecoveryValidation(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	c.Environment, c.nativeIdentity = "production", nil
	c.WorkloadIssuerURL, c.WorkloadJWKSURL = "", ""
	c.ateIdentity = &kitATEWorkloadIdentity{Atespace: "prod-space", ActorName: "actor-one"}

	tx := testenv.BeginTx(t, t.Context(), db)
	r.LifecycleTx = tx
	q := workloadrepo.New(tx)
	params := workloadrepo.ListWorkloadAdmissionsParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: r.ProjectID, Valid: true},
	}
	before, err := q.ListWorkloadAdmissions(t.Context(), params)
	require.NoError(t, err)

	binding, err := core.provisionKitATEWorkload(t.Context(), r, c, identity, "actor-generation-one")
	require.NoError(t, err)
	require.Equal(t, "actor/prod-space/actor-one", binding.Subject)
	require.Equal(t, uuid.Nil, binding.IssuerID)

	after, err := q.ListWorkloadAdmissions(t.Context(), params)
	require.NoError(t, err)
	require.Len(t, after, len(before), "ATE workload provisioning must not create tenant workload admissions")

	require.NoError(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-one"))
	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, binding, identity, "actor-generation-two"), ErrKitRuntimeBinding)

	foreign := *binding
	foreign.Subject = "actor/prod-space/other-actor"
	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, &foreign, identity, "actor-generation-one"), ErrKitRuntimeBinding)

	withIssuer := *binding
	withIssuer.IssuerID = uuid.New()
	require.ErrorIs(t, core.validateKitATEBootstrapWorkload(t.Context(), r, c, &withIssuer, identity, "actor-generation-one"), ErrKitRuntimeBinding)

	require.NoError(t, core.kitATERevoke(t.Context(), r, binding))
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
	subject, err := kitATEWorkloadSubject(&kitATEWorkloadIdentity{Atespace: "prod-space", ActorName: "actor-one"})
	require.NoError(t, err)
	require.Equal(t, "actor/prod-space/actor-one", subject)
}

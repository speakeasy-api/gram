package assistants

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/gen/types"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/stretchr/testify/require"
)

func TestKitLocalCompositionSelectsOnlyNewAssistants(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_local_selection")
	require.NoError(t, err)
	project, existing, _, _ := insertAssistantFixture(t, db)
	seedIdentityMembers(t, db)
	core := newProvisioningCore(t, db)
	core.serverURL = &url.URL{Scheme: "https", Host: "api.example"}
	core.guardianPolicy = guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	core.chatWriter = new(chat.ChatMessageWriter)
	core.assistantTokens = assistanttokens.New("test", db, nil, nil, nil)
	core.encryptionClient = new(encryption.Client)
	flags := &feature.InMemory{}
	core.SetFeatureProvider(flags)

	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	config := KitLocalCompositionConfig{
		Environment: "local", Image: "example.invalid/kit:local", Network: "isolated-test", ExtraCACertFile: "/test/ca.pem", Origin: origin,
		TurnStream:      new(chat.TurnStream),
		WorkloadJWKSURL: "https://external.example/jwks", WorkloadIssuerURL: "https://external.example", WorkloadIssuerBaseURL: "https://issuer.example", WorkloadSubject: "operator-subject", WorkloadAssertionFile: "/run/assertion",
	}
	require.NoError(t, core.InstallKitLocalRuntime(config))
	backend := core.runtime
	if wrapped, ok := backend.(*telemetryRuntimeBackend); ok {
		backend = wrapped.inner
	}
	versioned, ok := backend.(*versionedRuntimeBackend)
	require.True(t, ok)
	require.NotNil(t, versioned.kit.ServerURL())
	require.NotEqual(t, origin.String(), versioned.kit.ServerURL().String(), "Kit's own server URL is not the inference provider origin")

	// Without Project Gateway availability, new assistants stay on Default (v2).
	ineligible, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "No Gateway", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, ineligible.RuntimeVersion)

	// Enable Project Gateway with shared organization issuer + machine access.
	enableTestOrganizationGateway(t, db, "org-test")
	flags.SetFlag(feature.FlagProjectGatewayMachineAccess, "org-test", true)
	fixtures := testrepo.New(db)
	issuerID, err := fixtures.InsertOrganizationTierUserSessionIssuerFixture(t.Context(), testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:     pgtype.Text{String: "org-test", Valid: true},
		Slug:               "kit-local-shared",
		AuthnChallengeMode: "none",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	_, err = fixtures.SetUserSessionIssuerAuthorizationServerModeFixture(t.Context(), testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: "shared",
		IssuerID:                issuerID,
		OrganizationID:          "org-test",
	})
	require.NoError(t, err)

	// Unattached new assistant with identity selects Kit (v3) and gets explicit
	// built-in grants WITHOUT wildcard mcp:connect *.
	created, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Local Kit", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 3, created.RuntimeVersion)
	require.NotNil(t, created.AgentID)

	q := assistantrepo.New(db)
	saved, err := q.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: created.ID})
	require.NoError(t, err)
	require.EqualValues(t, 3, saved.RuntimeVersion)
	old, err := q.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: existing})
	require.NoError(t, err)
	require.EqualValues(t, 2, old.RuntimeVersion)

	grants, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: "org-test",
		AgentID:        uuid.MustParse(*created.AgentID),
	})
	require.NoError(t, err)
	require.True(t, hasExactBuiltinConnectGrant(t, grants, project, platformtools.AssistantsPlatformToolsetSlug), "Kit assistant must receive explicit built-in mcp:connect grant")
	for _, g := range grants {
		if g.Scope == string(authz.ScopeMCPConnect) {
			sel, selErr := authz.SelectorFromRow(g.Selectors)
			require.NoError(t, selErr)
			require.NotEqual(t, authz.WildcardResource, sel[authz.SelectorKeyResourceID], "Kit assistant must not start with wildcard mcp:connect *")
		}
	}

	// Creates with explicit MCP attachments stay on Default (v2).
	hosted, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: project, Name: "Billing", Slug: "billing",
	})
	require.NoError(t, err)
	createCanonicalWrapper(t, db, hosted, "org-test-billing", "private")
	attachedCreate, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Attached Default", "openai/gpt-4o-mini", "", []*types.AssistantToolsetRef{{ToolsetSlug: "billing"}}, nil, 300, 1, StatusActive, true, allowAllMCPEnable, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, attachedCreate.RuntimeVersion)
}

func TestKitLocalBootstrapRejectsUnknownEnvironment(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"", "staging-unknown"} {
		err := validateKitLocalComposition(KitLocalCompositionConfig{Environment: environment})
		require.ErrorContains(t, err, "requires the local environment")
	}
}

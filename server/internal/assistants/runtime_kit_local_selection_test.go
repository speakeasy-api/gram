package assistants

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestKitLocalCompositionSelectsOnlyNewAssistants(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_local_selection")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "kit-local-selection")
	existing := createLegacyAssistant(t, db, project, "Legacy")
	core := newProvisioningCore(t, db)
	core.serverURL = &url.URL{Scheme: "https", Host: "api.example"}
	core.guardianPolicy = guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	core.chatWriter = new(chat.ChatMessageWriter)
	core.assistantTokens = assistanttokens.New("test", db, nil, nil, nil)
	core.encryptionClient = new(encryption.Client)
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
	flags := new(feature.InMemory)
	core.SetFeatureProvider(flags)
	create := func(name string, provisionIdentity bool) assistantRecord {
		t.Helper()
		created, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", name, "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, provisionIdentity, nil, nil)
		require.NoError(t, err)
		return created
	}
	setting := featuresrepo.EnableFeatureParams{OrganizationID: "org-test", FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = featuresrepo.New(db).EnableFeature(t.Context(), setting)
	require.NoError(t, err)
	_, err = featuresrepo.New(db).DeleteFeature(t.Context(), featuresrepo.DeleteFeatureParams(setting))
	require.NoError(t, err)
	flags.SetFlag(feature.FlagProjectGatewayMachineAccess, "org-test", true)
	require.EqualValues(t, 2, create("No issuer", true).RuntimeVersion)
	flags.SetFlag(feature.FlagProjectGatewayMachineAccess, "org-test", false)
	fixtures := testrepo.New(db)
	issuer, err := fixtures.InsertOrganizationTierUserSessionIssuerFixture(t.Context(), testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{OrganizationID: pgtype.Text{String: "org-test", Valid: true}, Slug: "kit-local", AuthnChallengeMode: "none", SessionDuration: pgtype.Interval{Microseconds: 3600000000, Valid: true}})
	require.NoError(t, err)
	_, err = fixtures.SetUserSessionIssuerAuthorizationServerModeFixture(t.Context(), testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{OrganizationID: "org-test", IssuerID: issuer, AuthorizationServerMode: "shared"})
	require.NoError(t, err)
	require.EqualValues(t, 2, create("Rollout off", true).RuntimeVersion)
	flags.SetFlag(feature.FlagProjectGatewayMachineAccess, "org-test", true)
	require.EqualValues(t, 2, create("No identity", false).RuntimeVersion)
	created := create("Local Kit", true)
	require.EqualValues(t, 3, created.RuntimeVersion)
	require.NotNil(t, created.AgentID)
	grants, err := authz.LoadGrants(t.Context(), db, "org-test", []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeAgent, *created.AgentID)})
	require.NoError(t, err)
	var scopes []authz.Scope
	for _, grant := range grants {
		scopes = append(scopes, grant.Scope)
	}
	require.Contains(t, scopes, authz.ScopeMCPRead)
	require.NotContains(t, scopes, authz.ScopeMCPConnect, "Kit starts without any server assignment")
	q := assistantrepo.New(db)
	saved, err := q.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: created.ID})
	require.NoError(t, err)
	require.EqualValues(t, 3, saved.RuntimeVersion)
	old, err := q.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: existing.ID})
	require.NoError(t, err)
	require.EqualValues(t, 2, old.RuntimeVersion)
}

func TestKitLocalBootstrapRejectsUnknownEnvironment(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"", "staging-unknown"} {
		err := validateKitLocalComposition(KitLocalCompositionConfig{Environment: environment})
		require.ErrorContains(t, err, "requires the local environment")
	}
}

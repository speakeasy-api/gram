package agentmanagement

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func enableOrganizationGateway(t *testing.T, service *Service, organizationID string) {
	t.Helper()
	_, err := usersessionsrepo.New(service.db).CreateOrganizationUserSessionIssuer(t.Context(), usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(organizationID),
		Slug:               "runtime-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	settings := featuresrepo.New(service.db)
	flag := featuresrepo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = settings.EnableFeature(t.Context(), flag)
	require.NoError(t, err)
	_, err = settings.DeleteFeature(t.Context(), featuresrepo.DeleteFeatureParams(flag))
	require.NoError(t, err)
}

func runtimeGatewayService(t *testing.T, organizationID string) (*Service, *feature.InMemory) {
	t.Helper()
	conn := newTestDB(t)
	seedOrganization(t, conn, organizationID)
	seedOrganizationUser(t, conn, organizationID, "owner")
	flags := &feature.InMemory{}
	base, err := url.Parse("https://runtime.example.test")
	require.NoError(t, err)
	service := newProjectScopedService(t, conn).WithRuntimeGateway(flags, orghost.New(orghost.Config{ServerURL: base}))
	return service, flags
}

func TestGetRuntimeGatewayUsesAgentProjectWithoutProjectRead(t *testing.T) {
	t.Parallel()
	const organizationID = "org-runtime-bound"
	service, flags := runtimeGatewayService(t, organizationID)
	project := seedProject(t, service.db, organizationID, "bound-project")
	agent, err := agentsrepo.New(service.db).CreateAgent(t.Context(), agentsrepo.CreateAgentParams{
		OrganizationID: organizationID,
		OwnerUserID:    "owner",
		ProjectID:      conv.ToNullUUID(project.ID),
		Name:           "Bound agent",
	})
	require.NoError(t, err)
	enableOrganizationGateway(t, service, organizationID)
	flags.SetFlag(feature.FlagProjectGatewayMachineAccess, organizationID, true)

	// This owner context has no project:read grant. Agent ownership is the same
	// credential-management authorization key issuance uses.
	result, err := service.GetRuntimeGateway(validatedHumanContext(t, organizationID, "owner"), &gen.GetRuntimeGatewayPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.True(t, result.Available)
	require.NotNil(t, result.URL)
	require.Equal(t, "https://runtime.example.test/mcp/gateway/"+organizationID+"/projects/"+project.Slug, *result.URL)
	require.Nil(t, result.UnavailableReason)
}

func TestGetRuntimeGatewayFailsClosedWithoutMachineAccess(t *testing.T) {
	t.Parallel()
	const organizationID = "org-runtime-machine-off"
	service, _ := runtimeGatewayService(t, organizationID)
	project := seedProject(t, service.db, organizationID, "bound-project")
	agent, err := agentsrepo.New(service.db).CreateAgent(t.Context(), agentsrepo.CreateAgentParams{
		OrganizationID: organizationID,
		OwnerUserID:    "owner",
		ProjectID:      conv.ToNullUUID(project.ID),
		Name:           "Bound agent",
	})
	require.NoError(t, err)
	enableOrganizationGateway(t, service, organizationID)

	result, err := service.GetRuntimeGateway(validatedHumanContext(t, organizationID, "owner"), &gen.GetRuntimeGatewayPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.False(t, result.Available)
	require.Nil(t, result.URL)
	require.NotNil(t, result.UnavailableReason)
	require.Equal(t, runtimeGatewayMachineDisabled, *result.UnavailableReason)
}

func TestGetRuntimeGatewayRequiresAgentProject(t *testing.T) {
	t.Parallel()
	const organizationID = "org-runtime-unscoped"
	service, flags := runtimeGatewayService(t, organizationID)
	agent := createAgent(t, service.db, organizationID, "owner", "Unscoped agent")
	flags.SetFlag(feature.FlagProjectGatewayMachineAccess, organizationID, true)

	result, err := service.GetRuntimeGateway(validatedHumanContext(t, organizationID, "owner"), &gen.GetRuntimeGatewayPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.False(t, result.Available)
	require.Nil(t, result.URL)
	require.NotNil(t, result.UnavailableReason)
	require.Equal(t, runtimeGatewayProjectRequired, *result.UnavailableReason)
}

func TestGetRuntimeGatewayRequiresCredentialManagementAuthorization(t *testing.T) {
	t.Parallel()
	const organizationID = "org-runtime-authorization"
	service, _ := runtimeGatewayService(t, organizationID)
	seedOrganizationUser(t, service.db, organizationID, "viewer")
	agent := createAgent(t, service.db, organizationID, "owner", "Owned agent")

	_, err := service.GetRuntimeGateway(validatedHumanContext(t, organizationID, "viewer"), &gen.GetRuntimeGatewayPayload{AgentID: agent.ID.String()})
	require.Error(t, err)
}

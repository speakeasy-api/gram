package platformmcp

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func newOrganizationGatewayFixture(t *testing.T) (context.Context, *pgxpool.Pool, *OrganizationGatewayService, Principal, string) {
	t.Helper()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_organization_gateway")
	require.NoError(t, err)

	organizationID := "org-" + uuid.NewString()
	slug := "org-" + uuid.NewString()[:8]
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Organization gateway test organization",
		Slug:        slug,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	settings := featuresrepo.New(conn)
	flag := featuresrepo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = settings.EnableFeature(ctx, flag)
	require.NoError(t, err)
	_, err = settings.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(flag))
	require.NoError(t, err)

	serverURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	principal := Principal{UserID: "user-" + uuid.NewString()[:8], OrganizationID: organizationID}
	return ctx, conn, NewOrganizationGatewayService(conn, serverURL), principal, slug
}

func createOrganizationIssuerForGateway(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := usersessionsrepo.New(conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(organizationID),
		Slug:               "org-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
}

func TestOrganizationGatewayAvailableWithOneOrganizationIssuer(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, slug := newOrganizationGatewayFixture(t)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)

	output, err := service.Get(ctx, principal)

	require.NoError(t, err)
	require.True(t, output.Available)
	require.Equal(t, "https://app.example.test/mcp/gateway/"+slug, output.URL)
	require.Empty(t, output.UnavailableReason)
	// Each discovery mode gets its own first step; only Progressive has list_servers.
	require.Contains(t, output.NextAction, "Progressive: call list_servers")
	require.Contains(t, output.NextAction, "Direct: call the server--tool tools")
	require.Contains(t, output.NextAction, "Code Mode: run Python with the execute tool")
	require.Equal(t, "progressive", output.DefaultDiscoveryMode, "an organization that never set a default serves Progressive")
}

func TestOrganizationGatewayReportsStoredDefaultDiscoveryMode(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, _ := newOrganizationGatewayFixture(t)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	_, err := organizationsrepo.New(conn).SetOrganizationGatewayDiscoveryMode(ctx, organizationsrepo.SetOrganizationGatewayDiscoveryModeParams{
		GatewayDiscoveryMode: conv.ToPGText("direct"),
		ID:                   principal.OrganizationID,
	})
	require.NoError(t, err)

	output, err := service.Get(ctx, principal)

	require.NoError(t, err)
	require.True(t, output.Available)
	require.Equal(t, "direct", output.DefaultDiscoveryMode)
}

func TestOrganizationGatewayReportsCodeModeDefaultOnlyWhileAvailable(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, _ := newOrganizationGatewayFixture(t)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	_, err := organizationsrepo.New(conn).SetOrganizationGatewayDiscoveryMode(ctx, organizationsrepo.SetOrganizationGatewayDiscoveryModeParams{
		GatewayDiscoveryMode: conv.ToPGText("code_mode"),
		ID:                   principal.OrganizationID,
	})
	require.NoError(t, err)
	available := false
	service.WithCodeModeAvailability(func(context.Context, string) bool { return available })

	output, err := service.Get(ctx, principal)
	require.NoError(t, err)
	require.Equal(t, "progressive", output.DefaultDiscoveryMode, "a Code Mode default serves Progressive while Code Mode is unavailable")

	available = true
	output, err = service.Get(ctx, principal)
	require.NoError(t, err)
	require.Equal(t, "code_mode", output.DefaultDiscoveryMode)
}

func TestOrganizationGatewayUnavailableWithoutOrganizationIssuer(t *testing.T) {
	t.Parallel()
	ctx, _, service, principal, _ := newOrganizationGatewayFixture(t)

	output, err := service.Get(ctx, principal)

	require.NoError(t, err)
	require.False(t, output.Available)
	require.Empty(t, output.URL)
	require.Equal(t, "no_organization_issuer", output.UnavailableReason)
	require.Empty(t, output.DefaultDiscoveryMode)
}

func TestOrganizationGatewayUnavailableWithSeveralOrganizationIssuers(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, _ := newOrganizationGatewayFixture(t)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)

	output, err := service.Get(ctx, principal)

	require.NoError(t, err)
	require.False(t, output.Available)
	require.Empty(t, output.URL)
	require.Equal(t, "multiple_organization_issuers", output.UnavailableReason)
}

func TestOrganizationGatewayUnavailableWhenTurnedOff(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, _ := newOrganizationGatewayFixture(t)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	_, err := featuresrepo.New(conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{
		OrganizationID: principal.OrganizationID,
		FeatureName:    string(productfeatures.FeatureOrganizationGatewayDisabled),
	})
	require.NoError(t, err)

	output, err := service.Get(ctx, principal)

	require.NoError(t, err)
	require.False(t, output.Available)
	require.Empty(t, output.URL)
	require.Equal(t, "turned_off", output.UnavailableReason)
	require.Contains(t, output.NextAction, "review MCP Assignments")
}

// The gateway URL uses the platform host stored as the organization's default
// host, where the dashboard serves the organization, even when the request
// arrived on another platform host.
func TestOrganizationGatewayFollowsOrganizationDefaultHost(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, slug := newOrganizationGatewayFixture(t)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	require.NoError(t, organizationsrepo.New(conn).SetOrganizationDefaultHostForTest(ctx, organizationsrepo.SetOrganizationDefaultHostForTestParams{ID: principal.OrganizationID, DefaultHost: pgtype.Text{String: testExtraHostBaseURL, Valid: true}}))
	service = service.WithOrganizationHosts(testOrganizationHosts(t))
	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: testCanonicalBaseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})

	output, err := service.Get(ctx, principal)

	require.NoError(t, err)
	require.True(t, output.Available)
	require.Equal(t, testExtraHostBaseURL+"/mcp/gateway/"+slug, output.URL)
}

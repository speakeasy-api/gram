package mcp

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestResolveSharedProjectGatewayResource(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_shared_project_gateway")
	require.NoError(t, err)
	ctx := t.Context()
	org := "org_" + uuid.NewString()
	projectID := uuid.New()
	q := authclientbackfill.New(pool)
	require.NoError(t, q.SeedOrganizationFixture(ctx, org))
	featureQuery := featuresrepo.New(pool)
	setting := featuresrepo.EnableFeatureParams{OrganizationID: org, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = featureQuery.EnableFeature(ctx, setting)
	require.NoError(t, err)
	_, err = featureQuery.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(setting))
	require.NoError(t, err)
	require.NoError(t, q.SeedProjectFixture(ctx, authclientbackfill.SeedProjectFixtureParams{ID: projectID, Slug: "project", OrganizationID: org}))
	issuer, err := usersessionsrepo.New(pool).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "issuer", AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	rows, err := testrepo.New(pool).SetUserSessionIssuerAuthorizationServerModeFixture(ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{AuthorizationServerMode: "shared", IssuerID: issuer.ID, OrganizationID: org})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	base, err := url.Parse("https://platform.example")
	require.NoError(t, err)
	s := &Service{db: pool, orgsRepo: organizationsrepo.New(pool), serverURL: base, logger: testenv.NewLogger(t)}
	shared := &sharedAuthorizationServer{issuerID: issuer.ID}
	slug := org + "/projects/project"
	resource := base.String() + "/mcp/gateway/" + slug
	endpoint, rejection, err := s.resolveSharedResourceIndicators(ctx, s.logger, shared, []string{resource})
	require.NoError(t, err)
	require.Equal(t, sharedResourceAccepted, rejection)
	require.NotNil(t, endpoint)
	require.Equal(t, projectID, endpoint.ProjectID)
	require.Equal(t, slug, endpoint.Slug)
	require.Equal(t, mcproute.OrganizationGateway, endpoint.RouteBase)
	require.NotNil(t, endpoint.sharedResource)
	ref, err := endpoint.EndpointRef(ctx, pool, base.String())
	require.NoError(t, err)
	require.Equal(t, slug, ref.McpSlug)
	require.NoError(t, endpoint.ValidateRef(ref))

	for _, path := range []string{
		"/mcp/gateway/" + org,
		"/mcp/gateway/" + org + "/projects/project",
	} {
		t.Run("accepted_"+path, func(t *testing.T) {
			t.Parallel()
			resolved, rejection, err := s.resolveSharedResource(ctx, s.logger, shared, base.String()+path)
			require.NoError(t, err)
			require.Equal(t, sharedResourceAccepted, rejection)
			require.NotNil(t, resolved)
		})
	}
	for _, path := range []string{
		"/mcp/gateway/", "/mcp/gateway//projects/project",
		"/mcp/gateway/" + org + "/projects/",
		"/mcp/gateway/" + org + "/project/project",
		"/mcp/gateway/" + org + "/projects/project/",
		"/mcp/gateway/" + org + "/projects/project/extra",
		"/mcp/gateway/" + org + "/extra/projects/project",
		"/mcp/gateway/" + org + "/projects/project/projects/another",
		"/mcp/" + org + "/projects/project",
		"/mcp/gateway/" + org + "%2Fprojects%2Fproject",
	} {
		t.Run("malformed_"+path, func(t *testing.T) {
			t.Parallel()
			resolved, rejection, err := s.resolveSharedResource(ctx, s.logger, shared, base.String()+path)
			require.NoError(t, err)
			require.Equal(t, sharedResourceMalformed, rejection)
			require.Nil(t, resolved)
		})
	}
	_, rejection, err = s.resolveSharedResource(ctx, s.logger, &sharedAuthorizationServer{issuerID: uuid.New()}, resource)
	require.NoError(t, err)
	require.Equal(t, sharedResourceForeignIssuer, rejection)
}

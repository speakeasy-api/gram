package mcp

import (
	"context"
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
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestSharedOrganizationGatewayResource(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_shared_gateway_default")
	require.NoError(t, err)
	ctx := t.Context()
	org := "org_" + uuid.NewString()
	projectID := uuid.New()
	q := authclientbackfill.New(pool)
	require.NoError(t, q.SeedOrganizationFixture(ctx, org))
	require.NoError(t, q.SeedProjectFixture(ctx, authclientbackfill.SeedProjectFixtureParams{ID: projectID, Slug: "project", OrganizationID: org}))
	featureQuery := featuresrepo.New(pool)
	setting := featuresrepo.EnableFeatureParams{OrganizationID: org, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = featureQuery.EnableFeature(ctx, setting)
	require.NoError(t, err)
	_, err = featureQuery.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(setting))
	require.NoError(t, err)

	sessionDuration := pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}
	repo := usersessionsrepo.New(pool)
	orgIssuer, err := repo.CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "org-issuer", AuthnChallengeMode: "interactive", SessionDuration: sessionDuration,
	})
	require.NoError(t, err)
	projectIssuer, err := repo.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: projectID, OrganizationID: conv.ToPGText(org), Slug: "project-issuer", AuthnChallengeMode: "interactive", SessionDuration: sessionDuration,
		AuthorizationServerMode: pgtype.Text{String: "", Valid: false}, UseAuthenticationHost: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	for _, id := range []uuid.UUID{orgIssuer.ID, projectIssuer.ID} {
		rows, err := testrepo.New(pool).SetUserSessionIssuerAuthorizationServerModeFixture(ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{AuthorizationServerMode: "shared", IssuerID: id, OrganizationID: org})
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
	}

	base, err := url.Parse("https://platform.example")
	require.NoError(t, err)
	s := &Service{db: pool, orgsRepo: organizationsrepo.New(pool), serverURL: base, logger: testenv.NewLogger(t)}
	issuerFor := func(id uuid.UUID) *sharedIssuer {
		return &sharedIssuer{authorizationServer: &sharedAuthorizationServer{issuerID: id}, organizationID: org, logger: s.logger}
	}

	endpoint, resource, err := s.sharedOrganizationGatewayResource(ctx, s.logger, issuerFor(orgIssuer.ID))
	require.NoError(t, err)
	require.NotNil(t, endpoint)
	require.Equal(t, base.String()+"/mcp/gateway/"+org, resource)
	require.Equal(t, mcproute.OrganizationGateway, endpoint.RouteBase)
	require.True(t, endpoint.sharedResource.organizationGatewayDefault)

	endpoint, resource, err = s.sharedOrganizationGatewayResource(ctx, s.logger, issuerFor(projectIssuer.ID))
	require.NoError(t, err)
	require.Nil(t, endpoint)
	require.Empty(t, resource)

	s.platformHosts = map[string]string{"a.example": "https://a.example", "b.example": "https://b.example"}
	authHostCtx := context.WithValue(ctx, authenticationHostContextKey{}, "https://auth.example")
	platformCtx := func(baseURL string) context.Context {
		return requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: baseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})
	}
	gatewayFor := func(reqCtx context.Context) string {
		t.Helper()
		endpoint, resource, err := s.sharedOrganizationGatewayResource(reqCtx, s.logger, issuerFor(orgIssuer.ID))
		require.NoError(t, err)
		require.NotNil(t, endpoint)
		return resource
	}

	require.Equal(t, base.String()+"/mcp/gateway/"+org, gatewayFor(authHostCtx))

	orgs := organizationsrepo.New(pool)
	require.NoError(t, orgs.SetOrganizationDefaultHostForTest(ctx, organizationsrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: conv.ToPGText("https://a.example"), ID: org}))
	require.Equal(t, "https://b.example/mcp/gateway/"+org, gatewayFor(platformCtx("https://b.example")))
	require.Equal(t, "https://a.example/mcp/gateway/"+org, gatewayFor(platformCtx("https://a.example")))
	require.Equal(t, base.String()+"/mcp/gateway/"+org, gatewayFor(platformCtx(base.String())))
	require.Equal(t, "https://a.example/mcp/gateway/"+org, gatewayFor(authHostCtx))

	_, err = featureQuery.EnableFeature(ctx, setting)
	require.NoError(t, err)
	endpoint, _, err = s.sharedOrganizationGatewayResource(ctx, s.logger, issuerFor(orgIssuer.ID))
	require.NoError(t, err)
	require.Nil(t, endpoint)
}

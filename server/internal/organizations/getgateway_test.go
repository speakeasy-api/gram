package organizations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createGatewayIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) {
	t.Helper()
	_, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(organizationID),
		Slug:               "org-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	settings := featuresrepo.New(ti.conn)
	flag := featuresrepo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = settings.EnableFeature(ctx, flag)
	require.NoError(t, err)
	_, err = settings.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(flag))
	require.NoError(t, err)
}

func TestService_GetGateway_Available(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	createGatewayIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})

	require.NoError(t, err)
	require.True(t, res.Available)
	require.NotNil(t, res.URL)
	require.Equal(t, "http://localhost:35291/mcp/gateway/"+authCtx.OrganizationSlug, *res.URL)
	require.Nil(t, res.UnavailableReason)
}

func TestService_GetGateway_NoOrganizationIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})

	require.NoError(t, err)
	require.False(t, res.Available)
	require.Nil(t, res.URL)
	require.NotNil(t, res.UnavailableReason)
	require.Equal(t, "turned_off", *res.UnavailableReason)
}

func TestService_GetGateway_SeveralOrganizationIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	createGatewayIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	createGatewayIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})

	require.NoError(t, err)
	require.False(t, res.Available)
	require.NotNil(t, res.UnavailableReason)
	require.Equal(t, "multiple_organization_issuers", *res.UnavailableReason)
}

func TestService_GetGateway_CustomDomainOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	createGatewayIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	_, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "mcp-" + uuid.NewString()[:8] + ".example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})

	require.NoError(t, err)
	require.False(t, res.Available)
	require.Nil(t, res.URL)
	require.NotNil(t, res.UnavailableReason)
	require.Equal(t, "custom_domain_only", *res.UnavailableReason)
}

func TestService_GetGateway_TurnedOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	createGatewayIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	_, err := featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureOrganizationGatewayDisabled),
	})
	require.NoError(t, err)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})

	require.NoError(t, err)
	require.False(t, res.Available)
	require.Nil(t, res.URL)
	require.NotNil(t, res.UnavailableReason)
	require.Equal(t, "turned_off", *res.UnavailableReason)
}

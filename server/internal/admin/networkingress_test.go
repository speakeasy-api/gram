package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

func seedNetworkIngressOrg(t *testing.T, ctx context.Context, db *pgxpool.Pool) string {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Private Network Org", Slug: "private-network-" + uuid.NewString()[:8], WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	return orgID
}

func withGatewayMigrator(t *testing.T, svc *Service, db *pgxpool.Pool) {
	t.Helper()
	migrator, err := networkingress.NewGatewayMigrator(testenv.NewLogger(t), db, testenv.NewEncryptionClient(t), tailscaleapi.NewFake(), "tag:speakeasy",
		networkingress.NewOutboxRequester("test-network-ingress"), audit.NewLogger())
	require.NoError(t, err)
	svc.SetNetworkIngressGatewayMigrator(migrator)
}

func TestOrganizationNetworkIngressIsUnavailableWithoutTheMigrator(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	orgID := seedNetworkIngressOrg(t, ctx, db)

	_, err := svc.GetOrganizationNetworkIngress(ctx, &gen.GetOrganizationNetworkIngressPayload{OrganizationID: orgID, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
	_, err = svc.MoveOrganizationNetworkIngressToGateway(ctx, &gen.MoveOrganizationNetworkIngressToGatewayPayload{OrganizationID: orgID, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestOrganizationNetworkIngressReportsAnOrganizationWithoutOne(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	withGatewayMigrator(t, svc, db)
	orgID := seedNetworkIngressOrg(t, ctx, db)

	result, err := svc.GetOrganizationNetworkIngress(ctx, &gen.GetOrganizationNetworkIngressPayload{OrganizationID: orgID, AdminSessionToken: nil})
	require.NoError(t, err)
	require.Nil(t, result.Ingress)
	require.Equal(t, "tag:speakeasy", result.GatewayTag)

	_, err = svc.MoveOrganizationNetworkIngressToGateway(ctx, &gen.MoveOrganizationNetworkIngressToGatewayPayload{OrganizationID: orgID, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationNetworkIngressShowsAnOperatorIngressAndRefusesToMoveItWithoutCredentials(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	withGatewayMigrator(t, svc, db)
	orgID := seedNetworkIngressOrg(t, ctx, db)
	ingressID := uuid.New()
	require.NoError(t, testrepo.New(db).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: ingressID, OrganizationID: orgID, DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))

	result, err := svc.GetOrganizationNetworkIngress(ctx, &gen.GetOrganizationNetworkIngressPayload{OrganizationID: orgID, AdminSessionToken: nil})
	require.NoError(t, err)
	require.NotNil(t, result.Ingress)
	require.Equal(t, ingressID.String(), result.Ingress.ID)
	require.Equal(t, networkingress.RuntimeOperator, result.Ingress.Runtime)
	require.Equal(t, "private.example.ts.net", *result.Ingress.DNSName)
	require.False(t, result.Ingress.CredentialsConfigured)
	require.Empty(t, result.Ingress.GatewayNodes)

	_, err = svc.MoveOrganizationNetworkIngressToGateway(ctx, &gen.MoveOrganizationNetworkIngressToGatewayPayload{OrganizationID: orgID, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	result, err = svc.GetOrganizationNetworkIngress(ctx, &gen.GetOrganizationNetworkIngressPayload{OrganizationID: orgID, AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, networkingress.RuntimeOperator, result.Ingress.Runtime, "a refused move leaves the operator hosting the ingress")
}

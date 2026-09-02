package networkingress_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service  *networkingress.Service
	conn     *pgxpool.Pool
	features *productfeatures.Client
	flags    *feature.InMemory
	orgID    string
	orgSlug  string
}

func TestValidateLiveRejectsDisabledDeletedAndMismatchedAuthority(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "networkingressauthority")
	require.NoError(t, err)
	ingressID := insertAuthorityIngress(t, ctx, db)
	valid := networkingress.Authority{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          "https://private.example.ts.net",
		OrganizationID:   "org_authority",
		NetworkIngressID: ingressID,
		NamespaceKind:    networkingress.NamespacePlatform,
		CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	require.NoError(t, valid.ValidateLive(ctx, db))

	wrongOrg := valid
	wrongOrg.OrganizationID = "org_other"
	require.Error(t, wrongOrg.ValidateLive(ctx, db))
	wrongNamespace := valid
	wrongNamespace.NamespaceKind = networkingress.NamespaceCustomDomain
	wrongNamespace.CustomDomainID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	require.Error(t, wrongNamespace.ValidateLive(ctx, db))
	wrongOrigin := valid
	wrongOrigin.BaseURL = "https://other.example.ts.net"
	require.Error(t, wrongOrigin.ValidateLive(ctx, db))

	fixtures := testrepo.New(db)
	require.NoError(t, fixtures.SetNetworkIngressEnabledFixture(ctx, testrepo.SetNetworkIngressEnabledFixtureParams{Enabled: false, ID: ingressID}))
	require.Error(t, valid.ValidateLive(ctx, db))
	require.NoError(t, fixtures.SetNetworkIngressEnabledFixture(ctx, testrepo.SetNetworkIngressEnabledFixtureParams{Enabled: true, ID: ingressID}))
	require.NoError(t, fixtures.SoftDeleteNetworkIngressFixture(ctx, ingressID))
	require.Error(t, valid.ValidateLive(ctx, db))
}

func insertAuthorityIngress(t *testing.T, ctx context.Context, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ingressID := uuid.New()
	require.NoError(t, testrepo.New(db).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID:             ingressID,
		OrganizationID: "org_authority",
		DnsName:        pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	return ingressID
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("netingress-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgID := "org_" + uuid.NewString()
	orgSlug := "netingress-" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Network Ingress Test", Slug: orgSlug, WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	flags := &feature.InMemory{}
	checker := networkingress.NewRepositoryActiveIngressChecker(conn)
	admission := networkingress.NewExpansionAdmission(features, flags, orgrepo.New(conn), checker)
	enc := testenv.NewEncryptionClient(t)
	service := networkingress.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), enc, audit.NewLogger(), admission, nil)

	ti := &testInstance{service: service, conn: conn, features: features, flags: flags, orgID: orgID, orgSlug: orgSlug}
	productfeaturestest.Enable(t, ctx, conn, features, orgID, productfeatures.FeatureNetworkIngress)
	flags.SetFlag(feature.FlagNetworkIngressRollout, orgID, true)
	return ctx, ti
}

func (ti *testInstance) create(t *testing.T, ctx context.Context) *gen.NetworkIngress {
	t.Helper()
	result, err := ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{
		Provider: networkingress.ProviderTailscale, Hostname: "private-mcp", OauthClientID: "client-id", OauthClientSecret: "client-secret",
	})
	require.NoError(t, err)
	return result
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareErr *oops.ShareableError
	require.ErrorAs(t, err, &shareErr)
	require.Equal(t, code, shareErr.Code)
}

func loadRow(t *testing.T, ctx context.Context, ti *testInstance) repo.NetworkIngress {
	t.Helper()
	row, err := repo.New(ti.conn).GetNetworkIngressByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	return row
}

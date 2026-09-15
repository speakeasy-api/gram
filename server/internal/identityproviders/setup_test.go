package identityproviders_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/identityproviders"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	service    *identityproviders.Service
	conn       *pgxpool.Pool
	encryption *encryption.Client
	serverURL  *url.URL
	orgID      string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(
		t,
		logger,
		tracerProvider,
		conn,
		redisClient,
		cache.Suffix("identity-providers"),
		billing.NewStubClient(logger, tracerProvider),
	)
	ctx := authztest.InitAuthContext(t, t.Context(), conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	serverURL, err := url.Parse("https://api.example.test/base/")
	require.NoError(t, err)
	encryptionClient := testenv.NewEncryptionClient(t)
	service := identityproviders.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		audit.NewLogger(),
		encryptionClient,
		serverURL,
	)

	return ctx, &testInstance{
		service:    service,
		conn:       conn,
		encryption: encryptionClient,
		serverURL:  serverURL,
		orgID:      authCtx.ActiveOrganizationID,
	}
}

func createConnection(t *testing.T, ctx context.Context, ti *testInstance, tenantURL string) *gen.IdentityProviderConnection {
	t.Helper()

	connection, err := ti.service.Create(ctx, &gen.CreatePayload{
		Kind:         "okta",
		TenantURL:    tenantURL,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	return connection
}

func withExactScope(t *testing.T, ctx context.Context, ti *testInstance, scope authz.Scope) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(scope, ti.orgID))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()

	id, err := uuid.Parse(value)
	require.NoError(t, err)
	return id
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(value)
	require.NoError(t, err)
	return parsed
}

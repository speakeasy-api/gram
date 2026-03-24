package access_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type testInstance struct {
	service *access.Service
	conn    *pgxpool.Pool
}

func newTestAccessService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := access.NewService(logger, tracerProvider, conn, sessionManager, nil)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
	}
}

// upsertGrant is a test helper that upserts a single grant via the batch API.
func upsertGrant(t *testing.T, ctx context.Context, svc *access.Service, principalUrnStr, scope, resource string) *gen.Grant {
	t.Helper()

	principal, err := urn.ParsePrincipal(principalUrnStr)
	require.NoError(t, err)

	result, err := svc.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: principal, Scope: scope, Resource: resource},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)

	return result.Grants[0]
}

func mustParsePrincipal(t *testing.T, s string) urn.Principal {
	t.Helper()
	p, err := urn.ParsePrincipal(s)
	require.NoError(t, err)
	return p
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return conn
}

func seedOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:              organizationID,
		Name:            "Test Org",
		Slug:            "test-org",
		SsoConnectionID: conv.PtrToPGText(nil),
	})
	require.NoError(t, err)
}

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope access.Scope, resource string) {
	t.Helper()

	_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Resource:       resource,
	})
	require.NoError(t, err)
}

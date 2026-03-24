package access

import (
	"context"
	"log"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	accessTestInfra     *testenv.Environment
	accessTestInfraOnce sync.Once
)

func getAccessTestInfra() *testenv.Environment {
	accessTestInfraOnce.Do(func() {
		res, _, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
		if err != nil {
			log.Fatalf("Failed to launch test infrastructure: %v", err)
		}

		accessTestInfra = res
	})

	return accessTestInfra
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := getAccessTestInfra().CloneTestDatabase(t, "testdb")
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

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope Scope, resource string) {
	t.Helper()

	_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Resource:       resource,
	})
	require.NoError(t, err)
}

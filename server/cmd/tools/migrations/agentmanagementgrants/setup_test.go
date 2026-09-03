package agentmanagementgrants

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var cloneTestDatabase testenv.PostgresDBCloneFunc

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, clone, err := testenv.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	cloneTestDatabase = clone

	code := m.Run()
	if err := container.Terminate(ctx); err != nil {
		log.Fatalf("terminate test postgres: %v", err)
	}
	os.Exit(code)
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := cloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return pool
}

func seedOrganization(t *testing.T, pool *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := orgrepo.New(pool).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Test Organization",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
	})
	require.NoError(t, err)
}

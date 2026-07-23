package policybypass

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testinfra"
)

var cloneTestDatabase testinfra.PostgresDBCloneFunc

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, cloneFunc, err := testinfra.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	cloneTestDatabase = cloneFunc

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate postgres container: %v", err)
	}

	os.Exit(code)
}

func newURLGrantTestDatabase(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()

	ctx := t.Context()
	conn, err := cloneTestDatabase(t, "policybypass")
	require.NoError(t, err)

	organizationID := "org_" + t.Name()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Policy bypass test organization",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
	})
	require.NoError(t, err)

	return ctx, conn, organizationID
}

package metering_test

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch metering test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup metering test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newMeteringPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "metering_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Metering Test Organization",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
	})
	require.NoError(t, err)
	return conn, organizationID
}

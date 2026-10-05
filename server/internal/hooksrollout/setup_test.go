package hooksrollout_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false})
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

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "hooks_rollout")
	require.NoError(t, err)
	return conn
}

// createOrganization inserts an organization whose slug is its id, so tests
// running in parallel never collide.
func createOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()
	organizationID := uuid.NewString()
	now := time.Now().UTC()
	err := testrepo.New(conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               "Hooks Rollout Target",
		Slug:               organizationID,
		GramAccountType:    "free",
		WorkosID:           conv.PtrToPGText(nil),
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(nil),
	})
	require.NoError(t, err)
	return organizationID
}

package admin

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
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

// newTestAdminService builds the minimum Service needed to exercise read-only
// handlers like ListOrganizations. Auth, sessions, and OIDC fields are left nil
// because the test invokes the handler directly without going through the HTTP
// transport layer.
func newTestAdminService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "admintestdb")
	require.NoError(t, err)

	svc := &Service{
		logger: logger,
		db:     conn,
	}

	return ctx, svc, conn
}

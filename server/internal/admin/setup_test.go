package admin

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

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

// newTestAdminService builds the minimum Service needed to exercise read-only
// handlers like ListOrganizations. Auth, sessions, and OIDC fields are left nil
// because the test invokes the handler directly without going through the HTTP
// transport layer. The WorkOS field is left nil too: a handler that needs it
// gets a service from newTestAdminServiceWithWorkOS instead.
func newTestAdminService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "admintestdb")
	require.NoError(t, err)

	svc := &Service{
		logger: logger,
		db:     conn,
		audit:  audit.NewLogger(),
	}

	return ctx, svc, conn
}

// newTestAdminServiceWithWorkOS is newTestAdminService with an identity provider
// attached, for the handlers that write to one.
func newTestAdminServiceWithWorkOS(t *testing.T, workos orgprovision.WorkOSOrganizationCreator) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	svc.workos = workos

	return ctx, svc, conn
}

package platformaiscantargets

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false})
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

// platformAdminReaderStub stands in for the session manager's users.admin lookup.
type platformAdminReaderStub struct {
	admin bool
}

func (s *platformAdminReaderStub) IsPlatformAdmin(_ context.Context, _ string) (bool, error) {
	return s.admin, nil
}

type testInstance struct {
	service *Service
	conn    *pgxpool.Pool
	catalog *aitargets.Catalog
	admins  *platformAdminReaderStub
}

// newTestService wires the service over a fresh database; the tracer and key
// authorizer are unused by handlers.
func newTestService(t *testing.T) *testInstance {
	t.Helper()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	catalog := aitargets.NewCatalog(logger, conn, aitargets.DefaultCacheTTL)
	admins := &platformAdminReaderStub{admin: true}
	return &testInstance{
		service: &Service{
			tracer:   nil,
			logger:   logger,
			db:       conn,
			auth:     nil,
			sessions: admins,
			catalog:  catalog,
		},
		conn:    conn,
		catalog: catalog,
		admins:  admins,
	}
}

// freshAdminContext is a validated dashboard session of a platform admin.
func freshAdminContext(t *testing.T) context.Context {
	t.Helper()
	sessionID := "session-1"
	email := "admin@example.com"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org",
		UserID:               "user-1",
		SessionID:            &sessionID,
		Email:                &email,
		IsAdmin:              true,
	}, false)
}

// readOnlyAdminContext carries users.admin without fresh-session provenance.
func readOnlyAdminContext(t *testing.T) context.Context {
	t.Helper()
	email := "admin@example.com"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org",
		UserID:               "user-1",
		Email:                &email,
		IsAdmin:              true,
	})
}

func memberContext(t *testing.T) context.Context {
	t.Helper()
	sessionID := "session-2"
	email := "member@example.com"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org",
		UserID:               "user-2",
		SessionID:            &sessionID,
		Email:                &email,
		IsAdmin:              false,
	}, false)
}

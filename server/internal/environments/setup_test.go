package environments_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *environments.Service
	entries        *environments.EnvironmentEntries
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager

	// newService builds another service over the same database and auth
	// setup with a different encryption client, so tests can read stored
	// entries through a key that cannot decrypt them.
	newService func(enc *encryption.Client) (*environments.Service, *environments.EnvironmentEntries)
}

func newTestEnvironmentService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	auditLogger := audit.NewLogger()

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	newService := func(enc *encryption.Client) (*environments.Service, *environments.EnvironmentEntries) {
		svc := environments.NewService(logger, tracerProvider, conn, sessionManager, enc, authzEngine, auditLogger)
		entries := environments.NewEnvironmentEntries(logger, conn, enc, mcpmetadata_repo.New(conn))
		return svc, entries
	}

	svc, entries := newService(testenv.NewEncryptionClient(t))

	return ctx, &testInstance{
		service:        svc,
		entries:        entries,
		conn:           conn,
		sessionManager: sessionManager,
		newService:     newService,
	}
}

// plaintextValues returns the decrypted value of each entry in env, keyed by
// entry name. Management reads mask secrets, so tests that must prove which
// plaintext was stored read it through here.
func (ti *testInstance) plaintextValues(t *testing.T, ctx context.Context, env *types.Environment) map[string]string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	entries, err := ti.entries.ListEnvironmentEntries(ctx, *authCtx.ProjectID, uuid.MustParse(env.ID))
	require.NoError(t, err)

	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		values[entry.Name] = entry.Value
	}

	return values
}

package googlechatconnections

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/provider"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	var cleanup func() error
	var err error
	infra, cleanup, err = testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

type fixture struct {
	service *Service
	db      *pgxpool.Pool
	auth    *contextvalues.AuthContext
	enc     *encryption.Client
}

func newService(t *testing.T) (context.Context, *fixture) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "google_connections")
	require.NoError(t, err)
	redis, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)
	sessions := testenv.NewTestManager(t, logger, tp, db, redis, cache.Suffix("google-connections"), billing.NewStubClient(logger, tp))
	ctx := authztest.InitAuthContext(t, t.Context(), db, sessions)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	site, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	service := NewService(logger, tp, db, sessions, authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(), cache.NewRedisCacheAdapter(redis), enc, &provider.Client{}, nil, site)
	return ctx, &fixture{service: service, db: db, auth: ac, enc: enc}
}

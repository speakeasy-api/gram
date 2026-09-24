package slackdirectoryconnections_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
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

type mockProvider struct{ mock.Mock }

func (p *mockProvider) AuthorizationURL(state, workspace string) string {
	return "https://slack.example/authorize?" + url.Values{"state": {state}, "team": {workspace}}.Encode()
}
func (p *mockProvider) Exchange(ctx context.Context, code string) (*slackdirectoryconnections.Authorization, error) {
	args := p.Called(ctx, code)
	result, _ := args.Get(0).(*slackdirectoryconnections.Authorization)
	return result, args.Error(1)
}

type fixture struct {
	build    func(slackdirectoryconnections.Provider) *slackdirectoryconnections.Service
	service  *slackdirectoryconnections.Service
	db       *pgxpool.Pool
	provider *mockProvider
	enc      *encryption.Client
	cache    cache.Cache
	auth     *contextvalues.AuthContext
}

func newService(t *testing.T) (context.Context, *fixture) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "slackdirectory")
	require.NoError(t, err)
	redis, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)
	sessions := testenv.NewTestManager(t, logger, tp, db, redis, cache.Suffix("slack-directory-test"), billing.NewStubClient(logger, tp))
	ctx := authztest.InitAuthContext(t, t.Context(), db, sessions)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = contextvalues.SetAuthContext(ctx, ac)
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	provider := &mockProvider{}
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	store := cache.NewRedisCacheAdapter(redis)
	site, err := url.Parse("https://dashboard.example")
	require.NoError(t, err)
	build := func(oauth slackdirectoryconnections.Provider) *slackdirectoryconnections.Service {
		return slackdirectoryconnections.NewService(logger, tp, db, sessions, engine, audit.NewLogger(), store, enc, oauth, site)
	}
	return ctx, &fixture{build: build, service: build(provider), db: db, provider: provider, enc: enc, cache: store, auth: ac}
}
func begin(t *testing.T, ctx context.Context, f *fixture, id *string) string {
	t.Helper()
	result, err := f.service.Begin(ctx, &gen.BeginPayload{SessionToken: nil, ConnectionID: id})
	require.NoError(t, err)
	u, err := url.Parse(result.AuthorizationURL)
	require.NoError(t, err)
	return u.Query().Get("state")
}
func authorize(t *testing.T, ctx context.Context, f *fixture, state, team string) *gen.SlackDirectoryConnection {
	t.Helper()
	f.provider.On("Exchange", mock.Anything, "synthetic-code").Return(&slackdirectoryconnections.Authorization{WorkspaceID: team, WorkspaceName: "Example workspace", Scopes: []string{"users:read", "users:read.email"}, Tokens: slackdirectoryconnections.TokenBundle{Version: 1, AccessToken: "synthetic-access-token", RefreshToken: "synthetic-refresh-token", ExpiresAt: nil, TokenType: "bot"}}, nil).Once()
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("synthetic-code"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "slack_result=connected")
	rows, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	for _, row := range rows.Connections {
		if row.WorkspaceID == team {
			return row
		}
	}
	t.Fatal("authorized workspace missing")
	return nil
}

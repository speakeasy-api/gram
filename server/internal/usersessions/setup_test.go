package usersessions_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service             *usersessions.Service
	conn                *pgxpool.Pool
	sessionManager      *sessions.Manager
	chatSessionsManager *chatsessions.Manager
	redis               *redis.Client
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := usersessions.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		chatSessionsManager,
		authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache),
	)

	return ctx, &testInstance{
		service:             svc,
		conn:                conn,
		sessionManager:      sessionManager,
		chatSessionsManager: chatSessionsManager,
		redis:               redisClient,
	}
}

// jtiRevoked reports whether the chat_session_revoked:{jti}: key exists in
// the test redis. The trailing colon comes from cache.fullKey with
// cache.SuffixNone. Bypasses chatsessions.Manager.IsTokenRevoked, which has
// a brittle error-string match that fires false negatives on missing keys.
func jtiRevoked(t *testing.T, ctx context.Context, r *redis.Client, jti string) bool {
	t.Helper()
	n, err := r.Exists(ctx, "chat_session_revoked:"+jti+":").Result()
	require.NoError(t, err)
	return n > 0
}

// withExactAuthzGrants flips the auth context to enterprise (so RBAC is
// enforced) and seeds the supplied grants on a freshly minted role principal,
// returning a context with those grants prepared. Mirrors the helper used by
// every other RBAC-tested service in this codebase.
func withExactAuthzGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "usersessions-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := authz.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return authz.GrantsToContext(ctx, loadedGrants)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// seedUserSessionClient inserts a user_session_clients row directly through
// the SQLc repo so cascade tests can exercise behaviour against rows the
// management API will not write itself (DCR / token issuance lands in
// milestone #2).
func seedUserSessionClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, issuerID uuid.UUID, clientID string) (repo.UserSessionClient, error) {
	t.Helper()

	r := repo.New(conn)
	row, err := r.CreateUserSessionClient(ctx, repo.CreateUserSessionClientParams{
		UserSessionIssuerID:   issuerID,
		ClientID:              clientID,
		ClientSecretHash:      pgtype.Text{String: "", Valid: false},
		ClientName:            "test-" + clientID,
		RedirectUris:          []string{"https://example.com/cb"},
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	})
	if err != nil {
		return repo.UserSessionClient{}, fmt.Errorf("seed user session client: %w", err)
	}
	return row, nil
}

// seedUserSession inserts a user_sessions row directly through the SQLc repo
// so revoke and cascade tests can exercise behaviour against rows the
// management API will not write itself.
func seedUserSession(t *testing.T, ctx context.Context, conn *pgxpool.Pool, issuerID uuid.UUID, principalURN urn.SessionSubject) (repo.UserSession, error) {
	t.Helper()

	now := time.Now()
	r := repo.New(conn)
	row, err := r.CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID,
		SubjectUrn:          principalURN,
		Jti:                 "jti-" + uuid.NewString(),
		RefreshTokenHash:    "hash-" + uuid.NewString(),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
	})
	if err != nil {
		return repo.UserSession{}, fmt.Errorf("seed user session: %w", err)
	}
	return row, nil
}

// seedUserSessionConsent inserts a user_session_consents row directly through
// the SQLc repo. The unique index on (principal_urn, user_session_client_id,
// remote_set_hash) means each call must vary at least one of those keys.
func seedUserSessionConsent(t *testing.T, ctx context.Context, conn *pgxpool.Pool, clientID uuid.UUID, principalURN urn.SessionSubject) (repo.UserSessionConsent, error) {
	t.Helper()

	r := repo.New(conn)
	row, err := r.CreateUserSessionConsent(ctx, repo.CreateUserSessionConsentParams{
		SubjectUrn:          principalURN,
		UserSessionClientID: clientID,
		RemoteSetHash:       "remote-set-" + uuid.NewString(),
	})
	if err != nil {
		return repo.UserSessionConsent{}, fmt.Errorf("seed user session consent: %w", err)
	}
	return row, nil
}

package cliauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/cliauth"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

type testInstance struct {
	service        *cliauth.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

// newTestService wires a cliauth.Service over test Postgres + Redis and returns
// a context carrying an authenticated member session for the mock user/org.
func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := authztest.InitAuthContext(t, t.Context(), conn, sessionManager)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	svc := cliauth.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authzEngine,
		redisClient,
		"local",
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

// pkcePair returns a fresh PKCE code_verifier and its S256 code_challenge.
func pkcePair(t *testing.T) (verifier, challenge string) {
	t.Helper()

	verifier = "test-verifier-" + uuid.NewString() + "-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// authenticateSession stores the session record and authenticates it,
// returning a context reflecting that session.
func authenticateSession(t *testing.T, ctx context.Context, ti *testInstance, session sessions.Session) context.Context {
	t.Helper()

	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))
	authedCtx, err := ti.sessionManager.Authenticate(ctx, session.SessionID)
	require.NoError(t, err)
	return authedCtx
}

// seedOrgMetadata inserts org metadata without any membership rows.
func seedOrgMetadata(t *testing.T, ctx context.Context, conn *pgxpool.Pool, id, name, slug string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          id,
		Name:        name,
		Slug:        slug,
		WorkosID:    pgtype.Text{String: id, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
}

// seedUser inserts a user row directly (no IDP round-trip), returning its id.
func seedUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, id, email string, admin bool) string {
	t.Helper()

	user, err := userrepo.New(conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          id,
		Email:       email,
		DisplayName: "Test User " + id,
		PhotoUrl:    pgtype.Text{String: "", Valid: false},
		Admin:       admin,
	})
	require.NoError(t, err)
	return user.ID
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

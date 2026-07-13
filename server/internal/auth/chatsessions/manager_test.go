package chatsessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testClaims() chatsessions.ChatSessionClaims {
	return chatsessions.ChatSessionClaims{
		OrgID:            "org-123",
		ProjectID:        uuid.NewString(),
		OrganizationSlug: "test-org",
		ProjectSlug:      "test-project",
		UserID:           "user-123",
	}
}

func requireUnauthorized(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeUnauthorized, se.Code)
}

func TestManagerAuthorize_EmptyToken(t *testing.T) {
	t.Parallel()

	mgr := newTestManager(t)

	_, err := mgr.Authorize(t.Context(), "")
	requireUnauthorized(t, err)
}

func TestManagerAuthorize_MalformedToken(t *testing.T) {
	t.Parallel()

	mgr := newTestManager(t)

	_, err := mgr.Authorize(t.Context(), "not-a-jwt")
	requireUnauthorized(t, err)
}

func TestManagerAuthorize_ExpiredToken(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mgr := newTestManager(t)

	token, _, err := mgr.GenerateToken(ctx, testClaims(), "https://example.com", -60)
	require.NoError(t, err)

	_, err = mgr.Authorize(ctx, token)
	requireUnauthorized(t, err)
}

func TestManagerAuthorize_RevokedToken(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mgr := newTestManager(t)

	token, jti, err := mgr.GenerateToken(ctx, testClaims(), "https://example.com", 3600)
	require.NoError(t, err)

	require.NoError(t, mgr.RevokeToken(ctx, jti))

	_, err = mgr.Authorize(ctx, token)
	requireUnauthorized(t, err)
}

func TestManagerAuthorize_RevocationCheckUnavailable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// Point the revocation cache at a dead address so the lookup fails with an
	// infrastructure error rather than a token-validity verdict.
	redisClient := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = redisClient.Close()
	})
	mgr := chatsessions.NewManager(testenv.NewLogger(t), redisClient, "test-jwt-secret")

	token, _, err := mgr.GenerateToken(ctx, testClaims(), "https://example.com", 3600)
	require.NoError(t, err)

	_, err = mgr.Authorize(ctx, token)
	require.Error(t, err)
	var se *oops.ShareableError
	require.NotErrorAs(t, err, &se, "revocation-check failures must surface as server faults, not unauthorized")
}

func TestManagerAuthorize_ValidToken(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mgr := newTestManager(t)

	claims := testClaims()
	token, _, err := mgr.GenerateToken(ctx, claims, "https://example.com", 3600)
	require.NoError(t, err)

	authorizedCtx, err := mgr.Authorize(ctx, token)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(authorizedCtx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.Equal(t, claims.OrgID, authCtx.ActiveOrganizationID)
	require.Equal(t, claims.ProjectID, authCtx.ProjectID.String())
	require.Equal(t, claims.UserID, authCtx.UserID)
}

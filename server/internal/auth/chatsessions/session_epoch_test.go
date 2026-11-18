package chatsessions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestManagerAuthorize_SessionEpoch(t *testing.T) {
	t.Parallel()

	cutoff := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	for _, tc := range []struct {
		name             string
		issuedAt         time.Time
		hasEpoch         bool
		sessionID        *string
		wantUnauthorized bool
	}{
		{name: "before cutoff", issuedAt: cutoff.Add(-time.Microsecond), hasEpoch: true, sessionID: new(uuid.NewString()), wantUnauthorized: true},
		{name: "at cutoff", issuedAt: cutoff, hasEpoch: true, sessionID: new(uuid.NewString()), wantUnauthorized: true},
		{name: "after cutoff", issuedAt: cutoff.Add(time.Microsecond), hasEpoch: true, sessionID: new(uuid.NewString())},
		{name: "unknown issuance with cutoff", hasEpoch: true, sessionID: new(uuid.NewString()), wantUnauthorized: true},
		{name: "unknown issuance without cutoff", sessionID: new(uuid.NewString())},
		{name: "missing session with cutoff", hasEpoch: true, wantUnauthorized: true},
		{name: "missing session without cutoff"},
		{name: "empty session with cutoff", hasEpoch: true, sessionID: new(""), wantUnauthorized: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			mgr, sessionManager := newTestManager(t)
			claims := testClaims()
			claims.UserID = uuid.NewString()
			claims.SessionID = tc.sessionID
			if !tc.issuedAt.IsZero() {
				require.NoError(t, sessionManager.StoreSessionIssuedAt(ctx, sessions.Session{UserID: claims.UserID, SessionID: *claims.SessionID}, tc.issuedAt))
			}
			if tc.hasEpoch {
				require.NoError(t, sessionManager.RaiseUserEpoch(ctx, claims.UserID, cutoff))
			}
			// A fresh JWT must not revive a dashboard session older than the cutoff.
			token, _, err := mgr.GenerateToken(ctx, claims, "https://example.com", 3600)
			require.NoError(t, err)

			authorized, err := mgr.Authorize(ctx, token)
			if tc.wantUnauthorized {
				requireUnauthorized(t, err)
				_, ok := contextvalues.GetAuthContext(authorized)
				require.False(t, ok)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestManagerAuthorize_SignOutEverywhere(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mgr, sessionManager := newTestManager(t)
	claims := testClaims()
	claims.UserID = uuid.NewString()
	claims.SessionID = new(uuid.NewString())
	cutoff := time.Now().Add(-time.Minute)
	require.NoError(t, sessionManager.StoreSessionIssuedAt(ctx, sessions.Session{UserID: claims.UserID, SessionID: *claims.SessionID}, cutoff.Add(-time.Second)))

	token, _, err := mgr.GenerateToken(ctx, claims, "https://example.com", 3600)
	require.NoError(t, err)

	_, err = mgr.Authorize(ctx, token)
	require.NoError(t, err)
	require.NoError(t, sessionManager.RaiseUserEpoch(ctx, claims.UserID, cutoff))

	_, err = mgr.Authorize(ctx, token)
	requireUnauthorized(t, err)
	// Validation remains available for CORS and explicit JTI revocation.
	_, invalid, err := mgr.ValidateToken(ctx, token)
	require.NoError(t, err)
	require.False(t, invalid)

	claims.SessionID = new(uuid.NewString())
	require.NoError(t, sessionManager.StoreSessionIssuedAt(ctx, sessions.Session{UserID: claims.UserID, SessionID: *claims.SessionID}, cutoff.Add(time.Second)))

	fresh, _, err := mgr.GenerateToken(ctx, claims, "https://example.com", 3600)
	require.NoError(t, err)

	_, err = mgr.Authorize(ctx, fresh)
	require.NoError(t, err)
}

type failingSessionEpochChecker struct {
	err error
}

func (c failingSessionEpochChecker) SessionPredatesEpoch(context.Context, string, string) (bool, error) {
	return false, c.err
}

func TestManagerAuthorize_SessionEpochUnavailable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	lookupErr := errors.New("session epoch unavailable")
	mgr := chatsessions.NewManager(testenv.NewLogger(t), redisClient, "test-jwt-secret", failingSessionEpochChecker{err: lookupErr})
	claims := testClaims()
	claims.SessionID = new(uuid.NewString())
	token, _, err := mgr.GenerateToken(ctx, claims, "https://example.com", 3600)
	require.NoError(t, err)

	_, invalid, err := mgr.ValidateToken(ctx, token)
	require.NoError(t, err, "individual token revocation lookup succeeds")
	require.False(t, invalid)

	authorized, err := mgr.Authorize(ctx, token)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.ErrorIs(t, err, lookupErr)

	_, ok := contextvalues.GetAuthContext(authorized)
	require.False(t, ok)
}

func TestManagerAuthorize_NonDashboardTokensSkipSessionEpoch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, userID, apiKeyID string
		sessionID              *string
	}{
		{name: "API key with creator", userID: "creator", apiKeyID: "key"},
		{name: "API key with mixed claims", userID: "creator", apiKeyID: "key", sessionID: new("session")},
		{name: "API key without creator", apiKeyID: "key"},
		{name: "anonymous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			redisClient, err := infra.NewRedisClient(t, 0)
			require.NoError(t, err)

			mgr := chatsessions.NewManager(testenv.NewLogger(t), redisClient, "test-jwt-secret", failingSessionEpochChecker{err: errors.New("must not query dashboard epoch")})
			claims := testClaims()
			claims.UserID, claims.APIKeyID, claims.SessionID = tc.userID, tc.apiKeyID, tc.sessionID
			token, _, err := mgr.GenerateToken(t.Context(), claims, "https://example.com", 3600)
			require.NoError(t, err)

			_, err = mgr.Authorize(t.Context(), token)
			require.NoError(t, err)
		})
	}
}

func TestManagerAuthorize_MissingSessionEpochChecker(t *testing.T) {
	t.Parallel()

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	mgr := chatsessions.NewManager(testenv.NewLogger(t), redisClient, "test-jwt-secret", nil)
	token, _, err := mgr.GenerateToken(t.Context(), testClaims(), "https://example.com", 3600)
	require.NoError(t, err)

	_, err = mgr.Authorize(t.Context(), token)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
}

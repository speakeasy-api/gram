package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
)

func enforceSSOSignIns(ti *testInstance) {
	flags := &feature.InMemory{}
	ssosignins.EnableForTests(flags)
	ti.sessionManager.SetFlags(flags)
}

// A session stored before issuance was recorded keeps authenticating and
// updating: the serialized session is unchanged, so compare-and-swap still
// matches it.
func TestSessionEpoch_PreDeploySessionKeepsWorking(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceSSOSignIns(ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	sessionID := *authCtx.SessionID
	require.NoError(t, ti.redis.Del(ctx, "sessions:issued_at:"+sessionID).Err())

	_, err := ti.sessionManager.Authenticate(t.Context(), sessionID)
	require.NoError(t, err, "no epoch, so an unknown issuance is fine")
	current, err := ti.sessionManager.GetSession(ctx, sessionID)
	require.NoError(t, err)
	replacement := current
	replacement.WorkOSSessionID = "session_cas"
	require.NoError(t, ti.sessionManager.UpdateSession(ctx, current, replacement), "compare-and-swap matches the stored bytes")

	// Epochs are per user; a fresh id keeps this off the shared fixture user.
	userID := "epoch-user-" + uuid.NewString()
	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, time.Now()))
	predates, err := ti.sessionManager.SessionPredatesEpoch(ctx, userID, sessionID)
	require.NoError(t, err)
	require.True(t, predates, "after signing out everywhere an unknown issuance is treated as older")
}

func TestSessionEpoch_OnlyMovesForwardAndRestoringKeepsIssuance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceSSOSignIns(ti)
	userID := "epoch-user-" + uuid.NewString()

	issued := time.Now().Add(-time.Minute)
	session := sessions.Session{
		SessionID:             "epoch-session-" + uuid.NewString(),
		UserID:                userID,
		ActiveOrganizationID:  "",
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, ti.sessionManager.StoreSessionIssuedAt(ctx, session, issued))
	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, time.Now()))
	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, issued.Add(-time.Hour)), "an older epoch write is ignored")

	predates, err := ti.sessionManager.SessionPredatesEpoch(ctx, userID, session.SessionID)
	require.NoError(t, err)
	require.True(t, predates, "a late, older epoch write never moves the epoch back")

	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))
	predates, err = ti.sessionManager.SessionPredatesEpoch(ctx, userID, session.SessionID)
	require.NoError(t, err)
	require.True(t, predates, "storing a session again keeps its original issuance")
}

func TestSessionEpoch_IgnoredWhileEnforcementIsOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userID := "epoch-user-" + uuid.NewString()
	sessionID := "epoch-session-" + uuid.NewString()

	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, time.Now()))
	predates, err := ti.sessionManager.SessionPredatesEpoch(ctx, userID, sessionID)
	require.NoError(t, err)
	require.False(t, predates)
}

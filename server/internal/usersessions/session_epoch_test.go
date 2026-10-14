package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

func enforceSignIns(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	flags := &feature.InMemory{}
	signins.EnableForTests(flags, authCtx.ActiveOrganizationID)
	ti.sessionManager.SetFlags(flags)
}

// A session stored before issuance was recorded keeps authenticating and
// updating: the serialized session is unchanged, so compare-and-swap still
// matches it.
func TestSessionEpoch_PreDeploySessionKeepsWorking(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceSignIns(t, ctx, ti)
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

	// Epochs are per user, so the epoch check uses an isolated pre-deploy
	// session of a fresh user rather than the shared fixture user.
	userID := "epoch-user-" + uuid.NewString()
	isolated := sessions.Session{
		SessionID:             "epoch-session-" + uuid.NewString(),
		UserID:                userID,
		ActiveOrganizationID:  "",
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, ti.sessionManager.StoreSessionIssuedAt(ctx, isolated, time.Now()))
	require.NoError(t, ti.redis.Del(ctx, "sessions:issued_at:"+isolated.SessionID).Err())
	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, time.Now()))
	predates, err := ti.sessionManager.SessionPredatesEpoch(ctx, userID, isolated.SessionID)
	require.NoError(t, err)
	require.True(t, predates, "after signing out everywhere an unknown issuance is treated as older")
}

func TestSessionEpoch_OnlyMovesForwardAndRestoringKeepsIssuance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceSignIns(t, ctx, ti)
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

func TestSessionEpoch_EnforcedWhateverTheFlagSays(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userID := "epoch-user-" + uuid.NewString()
	session := sessions.Session{
		SessionID:             "epoch-session-" + uuid.NewString(),
		UserID:                userID,
		ActiveOrganizationID:  "",
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, ti.sessionManager.StoreSessionIssuedAt(ctx, session, time.Now().Add(-time.Minute)))
	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, time.Now()))
	predates, err := ti.sessionManager.SessionPredatesEpoch(ctx, userID, session.SessionID)
	require.NoError(t, err)
	require.True(t, predates, "an existing epoch is enforced even with the flag off")

	unknown := "epoch-session-" + uuid.NewString()
	predates, err = ti.sessionManager.SessionPredatesEpoch(ctx, userID, unknown)
	require.NoError(t, err)
	require.True(t, predates, "an unknown issuance predates any epoch")

	predates, err = ti.sessionManager.SessionPredatesEpoch(ctx, "no-epoch-"+uuid.NewString(), unknown)
	require.NoError(t, err)
	require.False(t, predates, "without an epoch nothing is rejected")
}

func TestSessionEpoch_ClearingASessionDropsItsIssuance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	session := sessions.Session{
		SessionID:             "epoch-session-" + uuid.NewString(),
		UserID:                "epoch-user-" + uuid.NewString(),
		ActiveOrganizationID:  "",
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))
	require.NoError(t, ti.sessionManager.ClearSession(ctx, session))
	_, known, err := ti.sessionManager.IssuedAt(ctx, session.SessionID)
	require.NoError(t, err)
	require.False(t, known)
}

func TestSessionEpoch_ASessionStoredAfterSignOutEverywhereWithoutIssuanceIsRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userID := "epoch-user-" + uuid.NewString()
	require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, time.Now()))

	// A login that started before the sign-out everywhere and stores its
	// session only afterwards, without an issuance.
	late := sessions.Session{
		SessionID:             "epoch-session-" + uuid.NewString(),
		UserID:                userID,
		ActiveOrganizationID:  "",
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, ti.sessionManager.StoreSession(ctx, late))
	predates, err := ti.sessionManager.SessionPredatesEpoch(ctx, userID, late.SessionID)
	require.NoError(t, err)
	require.True(t, predates)

	started := time.Now().Add(-time.Minute)
	issued := late
	issued.SessionID = "epoch-session-" + uuid.NewString()
	require.NoError(t, ti.sessionManager.StoreSessionIssuedAt(ctx, issued, started))
	predates, err = ti.sessionManager.SessionPredatesEpoch(ctx, userID, issued.SessionID)
	require.NoError(t, err)
	require.True(t, predates, "a session whose authentication started before the epoch is rejected too")
}

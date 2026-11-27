package sessions_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newIssueTestManager(t *testing.T) (*sessions.Manager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return sessions.NewManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), nil, redisClient, cache.SuffixNone, nil, nil, nil), mr
}

func issueTestSession(id string) sessions.Session {
	return sessions.Session{
		SessionID:             id,
		UserID:                "user-1",
		ActiveOrganizationID:  "org-1",
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
}

func TestIssueStoresTheSessionThenWritesItsCookie(t *testing.T) {
	t.Parallel()
	manager, _ := newIssueTestManager(t)
	issuedAt := time.Now().Add(-time.Second).Truncate(time.Microsecond)

	rec := httptest.NewRecorder()
	require.NoError(t, manager.Issue(t.Context(), rec, issueTestSession("session-1"), issuedAt))

	stored, err := manager.GetSession(t.Context(), "session-1")
	require.NoError(t, err)
	require.Equal(t, issueTestSession("session-1"), stored)
	recorded, known, err := manager.IssuedAt(t.Context(), "session-1")
	require.NoError(t, err)
	require.True(t, known)
	require.True(t, issuedAt.Equal(recorded))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, constants.SessionCookie, cookies[0].Name)
	require.Equal(t, "session-1", cookies[0].Value)
	require.Equal(t, "/", cookies[0].Path)
	require.Empty(t, cookies[0].Domain, "host-only")
	require.True(t, cookies[0].HttpOnly)
	require.True(t, cookies[0].Secure)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
}

func TestIssueWritesNoCookieWhenTheSessionIsNotStored(t *testing.T) {
	t.Parallel()
	manager, mr := newIssueTestManager(t)
	mr.Close()

	rec := httptest.NewRecorder()
	require.Error(t, manager.Issue(t.Context(), rec, issueTestSession("session-1"), time.Now()))
	require.Empty(t, rec.Result().Cookies())
}

func TestConfinedUsersSessionCannotMoveToAnotherOrganization(t *testing.T) {
	t.Parallel()
	manager, _ := newIssueTestManager(t)
	manager.ConfineUser("user-1", "org-1")

	session := issueTestSession("session-1")
	require.NoError(t, manager.Issue(t.Context(), httptest.NewRecorder(), session, time.Now()))

	// auth.switchScopes to another organization, and auth.enterDemo.
	for _, organizationID := range []string{"org-2", constants.DemoOrganizationID, ""} {
		moved := session
		moved.ActiveOrganizationID = organizationID
		require.ErrorContains(t, manager.UpdateSession(t.Context(), session, moved), "confined", organizationID)
	}
	stored, err := manager.GetSession(t.Context(), "session-1")
	require.NoError(t, err)
	require.Equal(t, "org-1", stored.ActiveOrganizationID)

	// Selecting the organization it is already in changes nothing and succeeds.
	require.NoError(t, manager.UpdateSession(t.Context(), session, session))
}

func TestOtherUsersSessionsMoveFreely(t *testing.T) {
	t.Parallel()
	manager, _ := newIssueTestManager(t)
	manager.ConfineUser("user-1", "org-1")

	session := issueTestSession("session-2")
	session.UserID = "user-2"
	require.NoError(t, manager.Issue(t.Context(), httptest.NewRecorder(), session, time.Now()))

	moved := session
	moved.ActiveOrganizationID = constants.DemoOrganizationID
	require.NoError(t, manager.UpdateSession(t.Context(), session, moved))
}

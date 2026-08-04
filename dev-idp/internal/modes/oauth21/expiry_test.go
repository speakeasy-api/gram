package oauth21

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// seedExpiredToken writes a token of the given kind whose expires_at is
// already in the past.
func (h *dbHandler) seedExpiredToken(t *testing.T, user repo.User, kind string) string {
	t.Helper()
	value := kind + "-expired"
	_, err := h.queries.CreateToken(t.Context(), repo.CreateTokenParams{
		Token:     value,
		UserID:    user.ID,
		ClientID:  "test-login-client",
		Kind:      kind,
		Scope:     sql.NullString{String: "", Valid: false},
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	require.NoError(t, err, "seed expired %s", kind)
	return value
}

func TestUserinfoRejectsExpiredAccessToken(t *testing.T) {
	t.Parallel()

	h := newDBHandler(t)
	user := h.seedUser(t, "expiry@devidptest.local")
	expired := h.seedExpiredToken(t, user, "access_token")

	req := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+expired)
	rec := httptest.NewRecorder()
	h.Handler.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
}

func TestRefreshGrantRejectsExpiredRefreshToken(t *testing.T) {
	t.Parallel()

	h := newDBHandler(t)
	user := h.seedUser(t, "expiry@devidptest.local")
	expired := h.seedExpiredToken(t, user, "refresh_token")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", expired)

	rec := h.postForm(t, "/token", form)
	require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, "invalid_grant", decodeError(t, rec)["error"])
}

func TestAuthorizationCodeGrantRejectsExpiredCode(t *testing.T) {
	t.Parallel()

	h := newDBHandler(t)
	user := h.seedUser(t, "expiry@devidptest.local")

	_, err := h.queries.CreateAuthCode(t.Context(), repo.CreateAuthCodeParams{
		Code:                "expired-code",
		UserID:              user.ID,
		ClientID:            "test-login-client",
		RedirectUri:         "https://app.example/cb",
		CodeChallenge:       sql.NullString{String: "", Valid: false},
		CodeChallengeMethod: sql.NullString{String: "", Valid: false},
		Scope:               sql.NullString{String: "", Valid: false},
		ExpiresAt:           time.Now().Add(-time.Hour),
	})
	require.NoError(t, err, "seed expired auth code")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "expired-code")

	rec := h.postForm(t, "/token", form)
	require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, "invalid_grant", decodeError(t, rec)["error"])
}

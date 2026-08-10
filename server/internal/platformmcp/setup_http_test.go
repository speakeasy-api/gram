package platformmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestDashboardSetupHTTPRequiresDashboardAuthentication(t *testing.T) {
	t.Parallel()

	handler := NewDashboardSetupHTTP(&recordingDashboardSetupStarter{}, dashboardSessionAuthenticator{err: errors.New("invalid session")}).Handler()
	request := dashboardSetupRequest(http.MethodPost, `{"handoff":"handoff"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}

func TestDashboardSetupHTTPAuthenticatesCookieAndStartsProviderSetupWithoutCachingHandoff(t *testing.T) {
	t.Parallel()

	starter := &recordingDashboardSetupStarter{result: ProviderSetupResult{AuthorizationURL: "https://provider.test/authorize"}}
	handler := NewDashboardSetupHTTP(starter, dashboardSessionAuthenticator{userID: "user", organizationID: "organization"}).Handler()
	request := dashboardSetupRequest(http.MethodPost, `{"handoff":"handoff-value"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", response.Header().Get("Pragma"))
	require.JSONEq(t, `{"authorization_url":"https://provider.test/authorize"}`, response.Body.String())
	require.Equal(t, "user", starter.userID)
	require.Equal(t, "organization", starter.organizationID)
	require.Equal(t, "handoff-value", starter.handoff)
}

func TestDashboardSetupHTTPFailsClosedAndValidatesRequests(t *testing.T) {
	t.Parallel()

	unavailable := NewDashboardSetupHTTP(nil, dashboardSessionAuthenticator{userID: "user", organizationID: "organization"}).Handler()
	response := httptest.NewRecorder()
	unavailable.ServeHTTP(response, dashboardSetupRequest(http.MethodPost, `{"handoff":"handoff"}`))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)

	handler := NewDashboardSetupHTTP(&recordingDashboardSetupStarter{}, dashboardSessionAuthenticator{userID: "user", organizationID: "organization"}).Handler()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, dashboardSetupRequest(http.MethodGet, ""))
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	require.Equal(t, http.MethodPost, response.Header().Get("Allow"))

	request := httptest.NewRequest(http.MethodPost, providerSetupStartPath, strings.NewReader(`{"handoff":"handoff"}`))
	request.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, dashboardSetupRequest(http.MethodPost, `{"handoff":""}`))
	require.Equal(t, http.StatusBadRequest, response.Code)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, dashboardSetupRequest(http.MethodPost, `{"handoff":"`+strings.Repeat("x", 16<<10)+`"}`))
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestDashboardSetupHTTPRejectsOrganizationlessSession(t *testing.T) {
	t.Parallel()

	handler := NewDashboardSetupHTTP(&recordingDashboardSetupStarter{}, dashboardSessionAuthenticator{userID: "user"}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, dashboardSetupRequest(http.MethodPost, `{"handoff":"handoff"}`))
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestDashboardSetupHTTPMapsSetupErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		code int
	}{
		{name: "invalid handoff", err: ErrSetupHandoffInvalid, code: http.StatusForbidden},
		{name: "registration disabled", err: ErrRegistrationUnavailable, code: http.StatusForbidden},
		{name: "authorization denied", err: ErrForbidden, code: http.StatusForbidden},
		{name: "operation rate limited", err: ErrOperationRateLimited, code: http.StatusTooManyRequests},
		{name: "readiness rate limited", err: ErrReadinessRateLimited, code: http.StatusTooManyRequests},
		{name: "operation budget unavailable", err: ErrOperationBudgetUnavailable, code: http.StatusServiceUnavailable},
		{name: "adapter unavailable", err: ErrProviderAdapterUnavailable, code: http.StatusServiceUnavailable},
		{name: "dependency unavailable", err: ErrUnavailable, code: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			handler := NewDashboardSetupHTTP(&recordingDashboardSetupStarter{err: test.err}, dashboardSessionAuthenticator{userID: "user", organizationID: "organization"}).Handler()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, dashboardSetupRequest(http.MethodPost, `{"handoff":"handoff"}`))
			require.Equal(t, test.code, response.Code)
		})
	}
}

func TestDashboardSetupHTTPReturnsReissueRequired(t *testing.T) {
	t.Parallel()

	handler := NewDashboardSetupHTTP(&recordingDashboardSetupStarter{err: ErrSetupHandoffReissueRequired}, dashboardSessionAuthenticator{userID: "user", organizationID: "organization"}).Handler()
	request := dashboardSetupRequest(http.MethodPost, `{"handoff":"handoff"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusConflict, response.Code)
	require.JSONEq(t, `{"code":"setup_handoff_reissue_required"}`, response.Body.String())
}

func dashboardSetupRequest(method, body string) *http.Request {
	request := httptest.NewRequest(method, providerSetupStartPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

type dashboardSessionAuthenticator struct {
	userID         string
	organizationID string
	err            error
}

func (a dashboardSessionAuthenticator) AuthenticateWithCookie(ctx context.Context) (context.Context, error) {
	if a.err != nil {
		return ctx, a.err
	}
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: a.userID, ActiveOrganizationID: a.organizationID}), nil
}

type recordingDashboardSetupStarter struct {
	userID         string
	organizationID string
	handoff        string
	result         ProviderSetupResult
	err            error
}

func (s *recordingDashboardSetupStarter) StartDashboardSetup(_ context.Context, userID, organizationID, handoff string) (ProviderSetupResult, error) {
	s.userID = userID
	s.organizationID = organizationID
	s.handoff = handoff
	if s.err != nil {
		return ProviderSetupResult{}, s.err
	}
	return s.result, nil
}

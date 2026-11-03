package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// denyingHandler runs one authz check with no grants loaded and answers with
// status when the engine refuses it.
func denyingHandler(t *testing.T, status int, check authz.Check) http.Handler {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := "session_123"
		ctx := contextvalues.SetAuthContext(r.Context(), &contextvalues.AuthContext{
			ActiveOrganizationID:  "org_123",
			UserID:                "user_123",
			ExternalUserID:        "",
			APIKeyID:              "",
			SessionID:             &sessionID,
			ProjectID:             nil,
			OrganizationSlug:      "",
			Email:                 nil,
			AccountType:           "enterprise",
			HasActiveSubscription: false,
			Whitelisted:           false,
			ProjectSlug:           nil,
			APIKeyScopes:          nil,
		})
		ctx = authz.GrantsToContext(ctx, nil)
		if err := engine.Require(ctx, check); err != nil {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"name":"forbidden"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestDenialHeaders_NamesTheMissingScopeOn403(t *testing.T) {
	t.Parallel()

	handler := DenialHeaders()(denyingHandler(t, http.StatusForbidden, authz.Check{
		Scope: authz.ScopeLogRead, ResourceKind: "", ResourceID: "proj_1", Dimensions: nil,
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rpc/telemetry.logs", nil))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "log:read", rec.Header().Get(HeaderRequiredScope))
	require.Equal(t, "project", rec.Header().Get(HeaderRequiredResourceKind))
}

func TestDenialHeaders_StaysQuietWhenTheHandlerRecovers(t *testing.T) {
	t.Parallel()

	// The handler probed a scope, was refused, and answered 200 anyway.
	handler := DenialHeaders()(denyingHandler(t, http.StatusOK, authz.Check{
		Scope: authz.ScopeLogRead, ResourceKind: "", ResourceID: "proj_1", Dimensions: nil,
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rpc/telemetry.logs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get(HeaderRequiredScope))
	require.Empty(t, rec.Header().Get(HeaderRequiredResourceKind))
}

func TestDenialHeaders_StaysQuietOn403WithoutAnAuthzDenial(t *testing.T) {
	t.Parallel()

	handler := DenialHeaders()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rpc/anything", nil))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, rec.Header().Get(HeaderRequiredScope))
}

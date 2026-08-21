package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type fakeSupportHandoffIssuer struct {
	token          string
	organizationID string
	err            error
}

func (f *fakeSupportHandoffIssuer) Issue(_ context.Context, organizationID string) (string, error) {
	f.organizationID = organizationID
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

func TestHandleOpenOrganizationInDashboard(t *testing.T) {
	t.Parallel()

	t.Run("requires a valid admin session", func(t *testing.T) {
		t.Parallel()

		svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-unauthorized", "operator@example.com")))
		rec := callOpenOrganization(t, svc, "target-org", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("issues a target-bound handoff to the runtime dashboard origin", func(t *testing.T) {
		t.Parallel()

		svc, sessionID, issuer := newOpenOrganizationService(t)
		seedOrg(t, t.Context(), svc.db, orgFixture{id: "target-org", name: "Target Organization", slug: "target-slug"})

		rec := callOpenOrganization(t, svc, "target-org", sessionID)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "target-org", issuer.organizationID)

		destination, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "https://dashboard.example.com", destination.Scheme+"://"+destination.Host)
		require.Equal(t, "/rpc/auth.login", destination.Path)
		require.Equal(t, "opaque-token", destination.Query().Get("support_handoff"))
		require.Equal(t, "/target-slug", destination.Query().Get("redirect"))
	})

	t.Run("rejects missing and disabled organizations before issuing", func(t *testing.T) {
		t.Parallel()

		disabledAt := time.Now().UTC()
		svc, sessionID, issuer := newOpenOrganizationService(t)
		seedOrg(t, t.Context(), svc.db, orgFixture{id: "disabled-org", name: "Disabled Organization", slug: "disabled-slug", disabledAt: &disabledAt})

		for _, organizationID := range []string{"missing-org", "disabled-org"} {
			rec := callOpenOrganization(t, svc, organizationID, sessionID)
			require.Equal(t, http.StatusNotFound, rec.Code)
		}
		require.Empty(t, issuer.organizationID)
	})

	t.Run("fails closed when issuance fails", func(t *testing.T) {
		t.Parallel()

		svc, sessionID, issuer := newOpenOrganizationService(t)
		seedOrg(t, t.Context(), svc.db, orgFixture{id: "target-org", name: "Target Organization", slug: "target-slug"})
		issuer.err = errors.New("issuer unavailable")

		rec := callOpenOrganization(t, svc, "target-org", sessionID)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Empty(t, rec.Header().Get("Location"))
	})
}

func newOpenOrganizationService(t *testing.T) (*Service, string, *fakeSupportHandoffIssuer) {
	t.Helper()

	ctx := t.Context()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-support", "operator@example.com")))
	conn, err := infra.CloneTestDatabase(t, "adminsupporthandoff")
	require.NoError(t, err)
	svc.db = conn
	svc.dashboardURL = &url.URL{Scheme: "https", Host: "dashboard.example.com", Path: "/ignored", RawQuery: "ignored=true"}
	issuer := &fakeSupportHandoffIssuer{token: "opaque-token"}
	svc.supportHandoffIssuer = issuer

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email:        "operator@example.com",
		Name:         "Test Operator",
		OIDCSubject:  "sub-support",
		HD:           testAdminHD,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	return svc, sessionID, issuer
}

func callOpenOrganization(t *testing.T, svc *Service, organizationID, sessionID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/admin/organization.open-dashboard?organization_id="+url.QueryEscape(organizationID), nil)
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	}
	rec := httptest.NewRecorder()
	SessionMiddleware(oops.ErrHandle(svc.logger, svc.handleOpenOrganizationInDashboard)).ServeHTTP(rec, req)
	return rec
}

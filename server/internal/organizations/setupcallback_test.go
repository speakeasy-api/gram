package organizations_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestSetupCallbackPreservesValidatedOriginAndLegacyNavigation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, ac.SessionID)
	ctx = contextvalues.SetSessionTokenInContext(ctx, *ac.SessionID)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	organizations.Attach(mux, ti.service)
	for _, tc := range []struct {
		query    string
		location string
		active   bool
	}{
		{"intent=sso&task=connect-idp", "?task=connect-idp", true},
		{"intent=sso&task=connect-idp", "?task=connect-idp", false},
		{"intent=dsync&task=directory-sync", "?task=directory-sync", false},
		{"intent=sso&task=identity-provider", "?task=identity-provider", true},
		{"intent=dsync&task=identity-provider", "?task=identity-provider", false},
		{"intent=sso", "?step=identity-provider", true},
		{"intent=sso", "", false},
		{"intent=dsync", "?step=anthropic-observability", false},
	} {
		state := "inactive"
		if tc.active {
			state = "active"
		}
		call := ti.orgs.On("ListConnections", mock.Anything, org.WorkosID.String).Return([]workos.Connection{{State: state}}, nil).Maybe()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/setup/callback?"+tc.query, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusTemporaryRedirect, rec.Code, tc.query)
		require.Equal(t, "http://localhost:5173/"+org.Slug+"/setup"+tc.location, rec.Header().Get("Location"), tc.query)
		call.Unset()
	}
}

func TestSetupCallbackRejectsInvalidOrigin(t *testing.T) {
	t.Parallel()
	_, ti := newTestOrganizationsService(t)
	mux := goahttp.NewMuxer()
	organizations.Attach(mux, ti.service)
	for _, query := range []string{
		"intent=sso&task=directory-sync", "intent=dsync&task=connect-idp",
		"intent=dsync&task=anthropic-observability", "intent=sso&task=unknown",
		"intent=sso&task=https://example.test", "intent=unknown&task=connect-idp",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/setup/callback?"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
		require.Empty(t, rec.Header().Get("Location"), query)
	}
}

package organizations_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestSetupCallbackUsesVisibleConfiguredTask(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		query   string
		visible []string
		task    string
		active  bool
	}{
		{"intent=sso&task=connect-idp", []string{"connect-idp"}, "connect-idp", true},
		{"intent=sso&task=connect-idp", []string{"connect-idp"}, "connect-idp", false},
		{"intent=dsync&task=directory-sync", []string{"directory-sync"}, "directory-sync", false},
		{"intent=sso&task=identity-provider", []string{"identity-provider"}, "identity-provider", true},
		{"intent=dsync&task=identity-provider", []string{"identity-provider"}, "identity-provider", false},
		{"intent=sso", []string{"connect-idp", "directory-sync"}, "directory-sync", true},
		{"intent=sso", []string{"connect-idp", "directory-sync"}, "connect-idp", false},
		{"intent=sso", []string{"identity-provider"}, "identity-provider", true},
		{"intent=dsync", []string{"directory-sync"}, "directory-sync", false},
		{"intent=dsync", []string{"identity-provider"}, "identity-provider", false},
		{"intent=sso&task=identity-provider", []string{"connect-idp"}, "connect-idp", false},
		{"intent=dsync&task=directory-sync", []string{"identity-provider"}, "identity-provider", false},
		{"intent=sso&task=connect-idp", []string{}, "", true},
		{"intent=dsync", []string{}, "", false},
	} {
		t.Run(tc.query+"/"+tc.task, func(t *testing.T) {
			ctx, ti := newTestOrganizationsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, ac.SessionID)
			ctx = contextvalues.SetSessionTokenInContext(ctx, *ac.SessionID)
			org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
			require.NoError(t, err)
			_, err = organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), org.ID, tc.visible, nil, urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test"), nil)
			require.NoError(t, err)
			state := "inactive"
			if tc.active {
				state = "active"
			}
			ti.orgs.On("ListConnections", mock.Anything, org.WorkosID.String).Return([]workos.Connection{{State: state}}, nil).Maybe()
			mux := goahttp.NewMuxer()
			organizations.Attach(mux, ti.service)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/setup/callback?"+tc.query, nil))
			want := "http://localhost:5173/" + org.Slug + "/setup"
			if tc.task != "" {
				want += "?task=" + tc.task
			}
			require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
			require.Equal(t, want, rec.Header().Get("Location"))
		})
	}
}

func TestSetupCallbackDomainVerification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		visible   []string
		state     workos.OrganizationDomainState
		stored    bool
		lookupErr error
		task      string
	}{
		{name: "pending", visible: []string{"domain-verification", "connect-idp"}, state: workos.OrganizationDomainStatePending, task: "domain-verification"},
		{name: "verified split identity", visible: []string{"domain-verification", "connect-idp"}, state: workos.OrganizationDomainStateVerified, task: "connect-idp"},
		{name: "verified combined identity", visible: []string{"domain-verification", "identity-provider"}, state: workos.OrganizationDomainStateLegacyVerified, task: "identity-provider"},
		{name: "stored verification", visible: []string{"connect-idp"}, stored: true, task: "connect-idp"},
		{name: "no visible identity task", visible: []string{"domain-verification"}, state: workos.OrganizationDomainStateVerified},
		{name: "hidden pending task", visible: []string{}, state: workos.OrganizationDomainStatePending},
		{name: "lookup failure", visible: []string{"domain-verification", "connect-idp"}, lookupErr: errors.New("workos unavailable"), task: "domain-verification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, ti := newTestOrganizationsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, ac.SessionID)
			ctx = contextvalues.SetSessionTokenInContext(ctx, *ac.SessionID)
			repo := orgrepo.New(ti.conn)
			org, err := repo.GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
			require.NoError(t, err)
			_, err = organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), org.ID, tc.visible, nil, urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test"), nil)
			require.NoError(t, err)
			if tc.stored {
				require.NoError(t, repo.SetVerifiedDomains(ctx, orgrepo.SetVerifiedDomainsParams{ID: org.ID, VerifiedDomains: []string{"example.com"}}))
			} else {
				ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, org.WorkosID.String).Return(&workos.OrganizationDomainPolicy{Domains: []workos.OrganizationDomain{{Domain: "example.com", State: tc.state}}}, tc.lookupErr).Once()
			}
			mux := goahttp.NewMuxer()
			organizations.Attach(mux, ti.service)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/setup/callback?intent=domain_verification", nil))
			want := "http://localhost:5173/" + org.Slug + "/setup"
			if tc.task != "" {
				want += "?task=" + tc.task
			}
			require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
			require.Equal(t, want, rec.Header().Get("Location"))
			org, err = repo.GetOrganizationMetadata(ctx, org.ID)
			require.NoError(t, err)
			if tc.stored || tc.state == workos.OrganizationDomainStateVerified || tc.state == workos.OrganizationDomainStateLegacyVerified {
				require.Equal(t, []string{"example.com"}, org.VerifiedDomains)
			} else {
				require.Empty(t, org.VerifiedDomains)
			}
			ti.orgs.AssertExpectations(t)
		})
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

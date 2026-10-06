package organizations_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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
	}{
		{"intent=sso&task=identity-provider", []string{"identity-provider"}, "identity-provider"},
		{"intent=dsync&task=identity-provider", []string{"identity-provider"}, "identity-provider"},
		{"intent=sso", []string{"identity-provider"}, "identity-provider"},
		{"intent=dsync", []string{"identity-provider"}, "identity-provider"},
		{"intent=sso&task=identity-provider", []string{"instrument-agents"}, ""},
		{"intent=dsync&task=identity-provider", []string{}, ""},
		{"intent=sso", []string{}, ""},
		{"intent=dsync", []string{}, ""},
	} {
		t.Run(tc.query+"/"+tc.task, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestOrganizationsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, ac.SessionID)
			ctx = contextvalues.SetSessionTokenInContext(ctx, *ac.SessionID)
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
			org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
			require.NoError(t, err)
			setVisibleSetupTasks(t, ctx, ti, org.ID, tc.visible)
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
		{name: "pending", visible: []string{"identity-provider"}, state: workos.OrganizationDomainStatePending, task: "identity-provider"},
		{name: "verified domain", visible: []string{"identity-provider"}, state: workos.OrganizationDomainStateVerified, task: "identity-provider"},
		{name: "verified combined identity", visible: []string{"identity-provider"}, state: workos.OrganizationDomainStateLegacyVerified, task: "identity-provider"},
		{name: "stored verification", visible: []string{"identity-provider"}, stored: true, task: "identity-provider"},
		{name: "no visible identity task", visible: []string{}, state: workos.OrganizationDomainStateVerified},
		{name: "hidden pending task", visible: []string{}, state: workos.OrganizationDomainStatePending},
		{name: "lookup failure", visible: []string{"identity-provider"}, lookupErr: errors.New("workos unavailable"), task: "identity-provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestOrganizationsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, ac.SessionID)
			ctx = contextvalues.SetSessionTokenInContext(ctx, *ac.SessionID)
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
			repo := orgrepo.New(ti.conn)
			org, err := repo.GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
			require.NoError(t, err)
			setVisibleSetupTasks(t, ctx, ti, org.ID, tc.visible)
			if tc.stored {
				require.NoError(t, repo.SetVerifiedDomains(ctx, orgrepo.SetVerifiedDomainsParams{ID: org.ID, VerifiedDomains: []string{"example.com"}}))
			} else {
				ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, org.WorkosID.String).Return(&workos.OrganizationDomainPolicy{Domains: []workos.OrganizationDomain{{Domain: "example.com", State: tc.state}}}, tc.lookupErr).Once()
			}
			mux := goahttp.NewMuxer()
			organizations.Attach(mux, ti.service)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/setup/callback?intent=domain_verification&task=identity-provider", nil))
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
		"intent=sso&task=connect-idp", "intent=dsync&task=directory-sync",
		"intent=domain_verification&task=domain-verification",
		"intent=dsync&task=anthropic-observability", "intent=sso&task=unknown",
		"intent=sso&task=https://example.test", "intent=unknown&task=connect-idp",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/setup/callback?"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
		require.Empty(t, rec.Header().Get("Location"), query)
	}
}

// Prepared grants exercise the session callback path while selecting the
// caller's permissions independently of the default admin test fixture.
func TestSetupCallbackRequiresActiveOrganizationAdmin(t *testing.T) {
	t.Parallel()
	for _, intent := range []string{"domain_verification", "sso", "dsync"} {
		for _, origin := range []string{"", "&task=identity-provider"} {
			for _, role := range []string{"member", "other organization admin"} {
				t.Run(intent+origin+"/"+role, func(t *testing.T) {
					t.Parallel()
					ctx, ti := newTestOrganizationsService(t)
					ac, ok := contextvalues.GetAuthContext(ctx)
					require.True(t, ok)
					require.NotNil(t, ac.SessionID)
					ctx = contextvalues.SetSessionTokenInContext(ctx, *ac.SessionID)
					grants := []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)}
					if role == "other organization admin" {
						grants = append(grants, authz.NewGrant(authz.ScopeOrgAdmin, "org_other"))
					}
					ctx = authztest.WithExactGrants(t, ctx, grants...)
					repo := orgrepo.New(ti.conn)
					org, err := repo.GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
					require.NoError(t, err)
					setVisibleSetupTasks(t, ctx, ti, org.ID, []string{"identity-provider"})
					before := listAllSetupTasks(t, ctx, ti)
					// Make a regressed domain refresh observable without an unexpected mock call.
					ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, org.WorkosID.String).Return(&workos.OrganizationDomainPolicy{Domains: []workos.OrganizationDomain{{Domain: "example.com", State: workos.OrganizationDomainStateVerified}}}, nil).Maybe()
					mux := goahttp.NewMuxer()
					organizations.Attach(mux, ti.service)
					rec := httptest.NewRecorder()
					mux.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/setup/callback?intent="+intent+origin, nil))
					require.Equal(t, http.StatusForbidden, rec.Code)
					require.Empty(t, rec.Header().Get("Location"))
					require.Equal(t, "forbidden\n", rec.Body.String())
					require.Empty(t, ti.orgs.Calls, "denied callbacks must not call WorkOS")
					afterOrg, err := repo.GetOrganizationMetadata(ctx, org.ID)
					require.NoError(t, err)
					require.Equal(t, org.VerifiedDomains, afterOrg.VerifiedDomains)
					require.Equal(t, before, listAllSetupTasks(t, ctx, ti), "denied callbacks must not change the selection")
				})
			}
		}
	}
}

// setVisibleSetupTasks makes exactly the given cards visible, the way a staff
// selection would, so a test controls which task the callback may open.
func setVisibleSetupTasks(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, visible []string) {
	t.Helper()
	repo := orgrepo.New(ti.conn)
	for _, task := range listAllSetupTasks(t, ctx, ti) {
		if task.Group {
			continue
		}
		require.NoError(t, repo.SetOrganizationSetupTaskVisibility(ctx, orgrepo.SetOrganizationSetupTaskVisibilityParams{OrganizationID: organizationID, TaskKey: task.Key, Hidden: !slices.Contains(visible, task.Key)}))
	}
}

// listAllSetupTasks reads the whole catalog, hidden cards included, as a
// platform admin would.
func listAllSetupTasks(t *testing.T, ctx context.Context, ti *testInstance) []*gen.SetupTask {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *ac
	platformAuth.IsAdmin = true
	includeHidden := true
	result, err := ti.service.ListSetupTasks(contextvalues.SetAuthContext(ctx, &platformAuth), &gen.ListSetupTasksPayload{IncludeHidden: &includeHidden})
	require.NoError(t, err)
	return result.Tasks
}

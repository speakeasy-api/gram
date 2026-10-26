package workloadpolicy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestDashboardProject_WorkloadIssuerAndSubject(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"owner", "missing project read", "missing workload write", "foreign project", "missing project", "empty project"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, ac.SessionID)
			require.False(t, ac.IsAdmin)
			project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, ti.projectID)
			require.NoError(t, err)
			agentID := newAgent(t, ctx, ti, "dashboard-owner")
			grants := []authz.Grant{authz.NewGrant(authz.ScopeWorkloadRead, ti.orgID)}
			if mode != "missing workload write" {
				grants = append(grants, authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID))
			}
			if mode != "missing project read" {
				grants = append(grants, authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String()))
			}
			for _, grant := range grants {
				selectors, err := grant.Selector.MarshalJSON()
				require.NoError(t, err)
				_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ti.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ti.userID), Scope: string(grant.Scope), Selectors: selectors})
				require.NoError(t, err)
			}
			slug := project.Slug
			if mode == "foreign project" {
				orgID := "org_foreign_" + uuid.NewString()
				_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Foreign organization", Slug: "foreign-" + uuid.NewString()[:8], WorkosID: conv.ToPGText(orgID)})
				require.NoError(t, err)
				foreign, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: orgID, Name: "Foreign project", Slug: "foreign-" + uuid.NewString()[:8]})
				require.NoError(t, err)
				slug = foreign.Slug
			}
			if mode == "missing project" {
				slug = "missing-" + uuid.NewString()
			}
			if mode == "empty project" {
				slug = " "
			}
			mux := goahttp.NewMuxer()
			workloadpolicy.Attach(mux, ti.service)
			request := func(method, endpoint, body string, projectSlug string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, "/rpc/workloadIdentities."+endpoint, strings.NewReader(body))
				req.Header.Set("Gram-Session", *ac.SessionID)
				req.Header.Set("Gram-Project", projectSlug)
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, req)
				return response
			}
			registered := request(http.MethodPost, "registerIssuer", `{"name":"Dashboard project issuer","issuer":"https://token.actions.githubusercontent.com","jwks_uri":"https://token.actions.githubusercontent.com/.well-known/jwks","project_scoped":true}`, slug)
			if mode != "owner" {
				require.GreaterOrEqual(t, registered.Code, http.StatusBadRequest, registered.Body.String())
				require.Less(t, registered.Code, http.StatusInternalServerError, registered.Body.String())
				return
			}
			require.Equal(t, http.StatusOK, registered.Code, registered.Body.String())
			var created gen.RegisterIssuerResponseBody
			require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &created))
			require.Len(t, created.Issuers, 1)
			require.Equal(t, ti.projectID.String(), created.Issuers[0].ProjectID)
			body, err := json.Marshal(map[string]any{"issuer": githubIssuer, "subject": "repo:example/project:ref:refs/heads/main", "match_kind": "exact", "agent_id": agentID.String(), "project_scoped": true})
			require.NoError(t, err)
			admitted := request(http.MethodPost, "admitSubject", string(body), slug)
			require.Equal(t, http.StatusOK, admitted.Code, admitted.Body.String())
			var policy gen.RegisterIssuerResponseBody
			require.NoError(t, json.Unmarshal(admitted.Body.Bytes(), &policy))
			require.Len(t, policy.Admissions, 1)
			require.Equal(t, ti.projectID.String(), policy.Admissions[0].ProjectID)
			require.Equal(t, agentID.String(), policy.Admissions[0].AgentID)
			listed := request(http.MethodGet, "list", "", slug)
			require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
			require.Contains(t, listed.Body.String(), created.Issuers[0].ID)
			orgOnly := request(http.MethodGet, "list", "", "")
			require.Equal(t, http.StatusOK, orgOnly.Code, orgOnly.Body.String())
			require.NotContains(t, orgOnly.Body.String(), created.Issuers[0].ID)
		})
	}
}

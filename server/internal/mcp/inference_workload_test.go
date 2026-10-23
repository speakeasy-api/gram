package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func seedWorkloadInferenceGrant(t *testing.T, f workloadGrantFixture, principal urn.Principal, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	selectors, err := (authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindInference,
		authz.SelectorKeyResourceID:   authz.WildcardResource,
		authz.SelectorKeyProjectID:    projectID.String(),
	}).MarshalJSON()
	require.NoError(t, err)
	grant, err := accessrepo.New(f.ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.fx.orgID, PrincipalUrn: principal, Scope: string(authz.ScopeInferenceExecute), Selectors: selectors,
	})
	require.NoError(t, err)
	return grant.ID
}

func enableWorkloadInference(t *testing.T, f workloadGrantFixture) (uuid.UUID, uuid.UUID) {
	t.Helper()
	agent := seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), f.fx.target.ProjectID)
	owner := seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID), f.fx.target.ProjectID)
	return agent, owner
}

func exchangeWorkloadInference(t *testing.T, f workloadGrantFixture, assertion string) *httptest.ResponseRecorder {
	t.Helper()
	return sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), url.Values{
		"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion},
		"resource": {mcp.InferenceResource}, "project_id": {f.fx.target.ProjectID.String()},
	})
}

func requireWorkloadInferenceToken(t *testing.T, f workloadGrantFixture, issuer string) string {
	t.Helper()
	w := exchangeWorkloadInference(t, f, f.assertion(t, issuer))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp := decodeSharedTokenResponse(t, w)
	require.NotEmpty(t, resp.AccessToken)
	require.Empty(t, resp.RefreshToken)
	return resp.AccessToken
}

func TestWorkloadInference_ExchangeAndAuthorize(t *testing.T) {
	t.Parallel()
	for _, audience := range []string{"issuer", "token endpoint"} {
		t.Run(audience, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			assertionAudience := issuer
			if audience == "token endpoint" {
				assertionAudience += "/token"
			}
			token := requireWorkloadInferenceToken(t, f, assertionAudience)
			claims := unverifiedClaims(t, token)
			require.Equal(t, issuer, claims["iss"])
			require.Equal(t, []any{mcp.InferenceResource}, claims["aud"])
			require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), claims["sub"])
			ctx, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.NoError(t, err)
			require.True(t, handled)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, f.fx.orgID, authCtx.ActiveOrganizationID)
			require.NotNil(t, authCtx.ProjectID)
			require.Equal(t, f.fx.target.ProjectID, *authCtx.ProjectID)
			require.Len(t, f.workloadSessions(t), 1)
			_, _, _, err = f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
			require.Error(t, err, "inference credentials must not authorize MCP")
		})
	}
}

func TestWorkloadInference_RequiresExplicitAgentAndOwnerPermission(t *testing.T) {
	t.Parallel()
	for _, permission := range []string{"neither", "agent only", "owner only", "wrong agent project", "wrong owner project"} {
		t.Run(permission, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			agentProject, ownerProject := f.fx.target.ProjectID, f.fx.target.ProjectID
			if permission == "wrong agent project" {
				agentProject = uuid.New()
			}
			if permission == "wrong owner project" {
				ownerProject = uuid.New()
			}
			if permission != "neither" && permission != "owner only" {
				seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), agentProject)
			}
			if permission != "neither" && permission != "agent only" {
				seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID), ownerProject)
			}
			requireWorkloadGrantRefused(t, exchangeWorkloadInference(t, f, f.assertion(t, issuer)))
			require.Empty(t, f.workloadSessions(t))
		})
	}
}

func TestWorkloadInference_LiveRevocation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"admission", "assignment", "suspended agent", "revoked agent", "deleted issuer", "agent grant", "owner grant", "deleted session", "expired session"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			agentGrant, ownerGrant := enableWorkloadInference(t, f)
			token := requireWorkloadInferenceToken(t, f, issuer)
			_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.NoError(t, err)
			require.True(t, handled)
			switch change {
			case "suspended agent":
				_, err := agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
				require.NoError(t, err)
			case "revoked agent":
				_, err := agentsrepo.New(f.ti.conn).RevokeAgent(t.Context(), agentsrepo.RevokeAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
				require.NoError(t, err)
			case "deleted issuer":
				softDeleteWorkloadIssuer(t, t.Context(), f.ti, f.fx.orgID, f.issuerID)
			case "admission":
				rows, err := testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(t.Context(), testrepo.SoftDeleteWorkloadAdmissionFixtureParams{OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject})
				require.NoError(t, err)
				require.Equal(t, int64(1), rows)
			case "assignment":
				unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
			case "agent grant", "owner grant":
				id := agentGrant
				if change == "owner grant" {
					id = ownerGrant
				}
				rows, err := accessrepo.New(f.ti.conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: id, OrganizationID: f.fx.orgID})
				require.NoError(t, err)
				require.Equal(t, int64(1), rows)
			case "deleted session":
				sessions := f.workloadSessions(t)
				require.Len(t, sessions, 1)
				_, err := usersessionsrepo.New(f.ti.conn).RevokeUserSession(t.Context(), usersessionsrepo.RevokeUserSessionParams{
					ID: sessions[0].ID, ProjectID: f.fx.target.ProjectID, OrganizationID: f.fx.orgID,
				})
				require.NoError(t, err)
			case "expired session":
				sessions := f.workloadSessions(t)
				require.Len(t, sessions, 1)
				rows, err := testrepo.New(f.ti.conn).ExpireUserSessionFixture(t.Context(), testrepo.ExpireUserSessionFixtureParams{
					ID: sessions[0].ID, OrganizationID: f.fx.orgID, ProjectID: f.fx.target.ProjectID,
					UserSessionIssuerID: f.fx.target.UserSessionIssuerID,
				})
				require.NoError(t, err)
				require.Equal(t, int64(1), rows)
			}
			_, handled, err = f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.True(t, handled)
			require.Error(t, err, "an already-issued token must observe revocation on the next request")
		})
	}
}

func TestWorkloadInference_RejectsInvalidExchangeTargets(t *testing.T) {
	t.Parallel()
	for _, project := range []string{"", "not-a-project", uuid.NewString()} {
		t.Run(project, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			form := issuerWorkloadForm(f.assertion(t, issuer))
			form.Set("resource", mcp.InferenceResource)
			form.Set("project_id", project)
			w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Equal(t, "invalid_target", decodeSharedTokenResponse(t, w).Error)
			require.Empty(t, f.workloadSessions(t))
		})
	}
}

func TestWorkloadInference_RejectsExpiredAndMultiAudienceAssertions(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"expired", "multiple audiences"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			claims := oauthtest.WorkloadClaims(f.issuer, f.subject, issuer)
			if invalid == "expired" {
				claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
				claims.NotBefore = claims.IssuedAt
				claims.Expiry = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			} else {
				claims.Audience = jwt.Audience{issuer, mcp.InferenceResource}
			}
			assertion := oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims)
			requireWorkloadGrantRefused(t, exchangeWorkloadInference(t, f, assertion))
			require.Empty(t, f.workloadSessions(t))
		})
	}
}

func TestWorkloadInference_RejectsOtherCredentials(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	issuer := f.sharedIssuer(t)
	enableWorkloadInference(t, f)
	// A platform assertion is an exchange credential, not a completion credential.
	_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), f.assertion(t, issuer))
	require.NoError(t, err)
	require.False(t, handled)
	// A platform signature cannot become a session signature by claiming the inference audience.
	_, handled, err = f.ti.service.AuthorizeWorkloadInference(t.Context(), f.assertion(t, mcp.InferenceResource))
	require.True(t, handled)
	require.Error(t, err)
	for _, resource := range []string{"", f.resource} {
		form := issuerWorkloadForm(f.assertion(t, issuer))
		if resource != "" {
			form.Set("resource", resource)
		}
		w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		token := decodeSharedTokenResponse(t, w).AccessToken
		_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
		require.NoError(t, err)
		require.False(t, handled, "MCP-only credentials must not authenticate inference")
	}
}

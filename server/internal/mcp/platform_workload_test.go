package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func platformWorkloadToken(t *testing.T, f workloadGrantFixture, issuer, slug string) string {
	t.Helper()
	resource := platformtools.PlatformToolsetURL(f.ti.serverURL, slug)
	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), url.Values{
		"grant_type": {workloadGrantJWTBearer}, "assertion": {f.assertion(t, issuer)},
		"resource": {resource}, "project_id": {f.fx.target.ProjectID.String()},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w)
	require.Empty(t, token.RefreshToken)
	require.Equal(t, []any{resource}, unverifiedClaims(t, token.AccessToken)["aud"])
	return token.AccessToken
}

func TestPlatformWorkloadResourceSeparation(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	issuer := f.sharedIssuer(t)
	slug := platformtools.AssistantsPlatformToolsetSlug
	token := platformWorkloadToken(t, f, issuer, slug)
	// No assistant, thread, runtime credential, or project header is required.
	w, err := servePlatformHTTP(t, f.ti, slug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = servePlatformHTTP(t, f.ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "a token for one builtin cannot reach another")
	_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
	require.NoError(t, err)
	require.False(t, handled, "platform token is not an inference credential")
	_, _, _, err = f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
	require.Error(t, err, "builtin credentials cannot authorize customer MCP servers")
	enableWorkloadInference(t, f)
	inference := requireWorkloadInferenceToken(t, f, issuer)
	_, err = servePlatformHTTP(t, f.ti, slug, toolsListBody(), inference)
	require.Error(t, err, "inference credential cannot reach builtins")
}

func TestPlatformWorkloadLiveRevocation(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"agent suspended", "admission removed", "session revoked"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			slug := platformtools.AssistantsPlatformToolsetSlug
			token := platformWorkloadToken(t, f, issuer, slug)
			_, err := servePlatformHTTP(t, f.ti, slug, toolsListBody(), token)
			require.NoError(t, err)
			switch reason {
			case "agent suspended":
				_, err = agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
			case "admission removed":
				_, err = testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(t.Context(), testrepo.SoftDeleteWorkloadAdmissionFixtureParams{OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject})
			case "session revoked":
				sessions := f.workloadSessions(t)
				require.Len(t, sessions, 1)
				_, err = usersessionsrepo.New(f.ti.conn).RevokeUserSession(t.Context(), usersessionsrepo.RevokeUserSessionParams{ID: sessions[0].ID, ProjectID: f.fx.target.ProjectID, OrganizationID: f.fx.orgID})
			}
			require.NoError(t, err)
			_, err = servePlatformHTTP(t, f.ti, slug, toolsListBody(), token)
			require.Error(t, err, "live revocation must reject an already issued token")
		})
	}
}

// Missing or invalid assertions must not reveal whether a target project exists
// in the issuer's organization. Authenticated issuance still checks tenancy.
func TestPlatformWorkloadUnauthenticatedProjectRefusals(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	foreign := newWorkloadGrantFixture(t)
	f.sharedIssuer(t)
	organizationIssuer := createOrganizationIssuer(t, f.fixtureContext(t), f.ti, f.fx.orgID)
	setIssuerMode(t, f.fixtureContext(t), f.ti, organizationIssuer, "shared", "")
	resource := platformtools.PlatformToolsetURL(f.ti.serverURL, platformtools.AssistantsPlatformToolsetSlug)
	for _, issuerID := range []uuid.UUID{f.fx.target.UserSessionIssuerID, organizationIssuer} {
		for _, assertion := range []string{"", "invalid-assertion"} {
			var wantCode int
			var wantBody string
			for i, projectID := range []uuid.UUID{f.fx.target.ProjectID, uuid.New(), foreign.fx.target.ProjectID} {
				response := sharedToken(t, t.Context(), f.ti, issuerID.String(), url.Values{
					"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion}, "resource": {resource}, "project_id": {projectID.String()},
				})
				require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
				if i == 0 {
					wantCode, wantBody = response.Code, response.Body.String()
				}
				require.Equal(t, wantCode, response.Code)
				require.JSONEq(t, wantBody, response.Body.String(), "unauthenticated refusal must not disclose project tenancy")
			}
		}
	}
	require.Empty(t, f.workloadSessions(t))
}

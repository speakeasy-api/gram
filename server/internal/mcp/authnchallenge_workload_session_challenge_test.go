package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// A workload has no refresh token, so a rejected workload session must be
// challenged with invalid_token: clients that see a bare challenge, or a 403
// without that code, keep replaying the dead token instead of exchanging for a
// new one.

type workloadChallengeFixture struct {
	agentConsentFixture
	agent      agentsrepo.Agent
	assignment uuid.UUID
	session    usersessionsrepo.UserSession
	token      string
}

func newWorkloadChallengeFixture(t *testing.T, ctx context.Context, ti *testInstance) workloadChallengeFixture {
	t.Helper()

	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignment := assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)
	session := seedWorkloadSession(t, ctx, ti, fx, urn.NewWorkloadSubject(issuerID, workloadSessionSubject))

	return workloadChallengeFixture{
		agentConsentFixture: fx,
		agent:               agent,
		assignment:          assignment,
		session:             session,
		token:               mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time),
	}
}

// serveMCPResponse runs a request through the same error middleware the
// server mounts, so the recorder holds the status and headers a client sees.
func serveMCPResponse(t *testing.T, ti *testInstance, slug string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	oops.ErrHandle(ti.logger, ti.service.ServePublic).ServeHTTP(w, req)
	return w
}

func requireAdmitted(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, "the workload must be admitted first, or the refusal that follows proves nothing: body=%s", w.Body.String())
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
}

func requireInvalidTokenChallenge(t *testing.T, ti *testInstance, slug string, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())
	require.Equal(t,
		`Bearer resource_metadata="`+ti.serverURL.String()+`/.well-known/oauth-protected-resource/mcp/`+slug+`", error="invalid_token"`,
		w.Header().Get("WWW-Authenticate"),
	)
}

func TestServePublic_RevokedWorkloadSessionIsChallengedWithInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))

	_, err := usersessionsrepo.New(ti.conn).RevokeUserSession(ctx, usersessionsrepo.RevokeUserSessionParams{
		ID:             fx.session.ID,
		ProjectID:      fx.target.ProjectID,
		OrganizationID: fx.orgID,
	})
	require.NoError(t, err)

	requireInvalidTokenChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))
}

// The revocation cache rejects a token before its session row is read, which
// is a different branch from the row lookup above.
func TestServePublic_WorkloadSessionWithRevokedTokenIsChallengedWithInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))

	require.NoError(t, ti.chatSessionsManager.RevokeToken(ctx, fx.session.Jti))

	requireInvalidTokenChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))
}

func TestServePublic_ExpiredWorkloadSessionIsChallengedWithInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))

	expired := mintSessionBearerExpiringAt(t, ti, fx.agentConsentFixture, fx.session, time.Now().Add(-time.Minute))
	requireInvalidTokenChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), expired))
}

func TestServePublic_WorkloadSessionWithWithdrawnAssignmentIsChallengedWithInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))

	unassignWorkloadAgent(t, ctx, ti, fx.assignment)

	requireInvalidTokenChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))
}

func TestServePublic_WorkloadSessionWithSuspendedAgentIsChallengedWithInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))

	_, err := agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: fx.agent.ID})
	require.NoError(t, err)

	requireInvalidTokenChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))
}

// A valid token asking for something its agent may not do is a permission
// denial, not a dead credential: exchanging for a new token would not help.
func TestServePublic_WorkloadToolDeniedByAgentPolicyIsNotInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	addHTTPTools(t, ctx, ti, fx.toolset.ID, fx.target.ProjectID, fx.orgID, "deploy", "rollback")

	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	// The agent may connect, but only to use rollback.
	selector := authz.NewSelector(authz.ScopeMCPConnect, fx.target.MCPResourceID.String())
	selector[authz.SelectorKeyTool] = "rollback"
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: fx.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)
	session := seedWorkloadSession(t, ctx, ti, fx, urn.NewWorkloadSubject(issuerID, workloadSessionSubject))
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), token))

	w := serveMCPResponse(t, ti, slug, makeToolsCallBody("deploy"), token)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.JSONEq(t,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32003,"message":`+strconv.Quote(mcpaccess.ToolPermissionDeniedMessage)+`}}`,
		w.Body.String(),
	)
}

// Human sessions keep the bare challenge: they recover through their refresh
// token on any 401.
func TestServePublic_ExpiredHumanSessionChallengeIsUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	session := seedWorkloadSession(t, ctx, ti, fx, urn.NewUserSubject(fx.userID))
	expired := mintSessionBearerExpiringAt(t, ti, fx, session, time.Now().Add(-time.Minute))
	slug := fx.toolset.McpSlug.String

	w := serveMCPResponse(t, ti, slug, makeInitializeBody(), expired)
	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())
	require.Equal(t,
		`Bearer resource_metadata="`+ti.serverURL.String()+`/.well-known/oauth-protected-resource/mcp/`+slug+`"`,
		w.Header().Get("WWW-Authenticate"),
	)
}

// A token Gram did not sign names nobody, so it cannot claim the workload
// challenge by asserting a workload subject.
func requireBareChallenge(t *testing.T, ti *testInstance, slug string, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())
	require.Equal(t,
		`Bearer resource_metadata="`+ti.serverURL.String()+`/.well-known/oauth-protected-resource/mcp/`+slug+`"`,
		w.Header().Get("WWW-Authenticate"),
	)
}

// The rollout gate hides the endpoint from a workload whose token is perfectly
// good. Naming that invalid_token would send the workload to the token endpoint
// for a credential that meets the same gate, forever.
func TestServePublic_WorkloadSessionHiddenByRolloutGetsBareChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))

	ti.features.SetFlag(feature.FlagAgentIdentityCredentials, fx.orgID, false)

	requireBareChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token))
}

func TestServePublic_ForgedWorkloadBearerGetsBareChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	slug := fx.toolset.McpSlug.String

	requireBareChallenge(t, ti, slug, serveMCPResponse(t, ti, slug, makeInitializeBody(), fx.token+"tampered"))
}

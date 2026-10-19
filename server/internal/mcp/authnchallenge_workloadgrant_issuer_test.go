package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// issuerWorkloadForm is a clientless workload grant naming no resource.
func issuerWorkloadForm(assertion string) url.Values {
	return url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {assertion},
	}
}

// fixtureContext is a context carrying the workload fixture's project, for
// seeding rows in it.
func (f workloadGrantFixture) fixtureContext(t *testing.T) context.Context {
	t.Helper()

	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ProjectID:            &f.fx.target.ProjectID,
		ActiveOrganizationID: f.fx.orgID,
	})
}

// seedToolsetOnIssuer creates another MCP server in the fixture's project,
// attached to the fixture's user session issuer, and returns the endpoint the
// MCP side resolves for it.
func (f workloadGrantFixture) seedToolsetOnIssuer(t *testing.T) *mcp.ResolvedMcpEndpoint {
	t.Helper()

	return f.seedToolsetOnUserSessionIssuer(t, f.fx.target.UserSessionIssuerID)
}

// seedToolsetOnUserSessionIssuer creates an MCP server in the fixture's
// project attached to issuerID, and returns the endpoint the MCP side
// resolves for it.
func (f workloadGrantFixture) seedToolsetOnUserSessionIssuer(t *testing.T, issuerID uuid.UUID) *mcp.ResolvedMcpEndpoint {
	t.Helper()

	ctx := f.fixtureContext(t)
	slug := "issuer-workload-" + uuid.NewString()[:8]
	toolset, err := toolsets_repo.New(f.ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         f.fx.orgID,
		ProjectID:              f.fx.target.ProjectID,
		Name:                   "Issuer workload MCP " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("another MCP server of the workload fixture's issuer"),
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)
	toolset, err = toolsets_repo.New(f.ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           f.fx.target.ProjectID,
	})
	require.NoError(t, err)

	return &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewToolset(toolset.ID).String(),
		OrganizationID:      f.fx.orgID,
		ProjectID:           f.fx.target.ProjectID,
		RouteBase:           "mcp",
		Slug:                slug,
		ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UserSessionIssuerID: issuerID,
	}
}

// applyIssuerGate presents token to endpoint as an MCP request would.
func (f workloadGrantFixture) applyIssuerGate(t *testing.T, token string, endpoint *mcp.ResolvedMcpEndpoint) (*httptest.ResponseRecorder, error) {
	t.Helper()

	w := httptest.NewRecorder()
	if _, _, _, err := f.ti.service.ApplyIssuerGate(t.Context(), w, token, f.ti.serverURL.String(), endpoint); err != nil {
		return w, fmt.Errorf("apply issuer gate: %w", err)
	}
	return w, nil
}

func requireShareableCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

// A workload grant naming no resource mints one session for the whole issuer:
// its audience names all of the issuer's MCP servers, it carries no refresh
// token and no resource, and each server the assigned agent may reach admits
// it.
func TestSharedAuthorizationServer_WorkloadWithoutResourceReachesIssuerServers(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	second := f.seedToolsetOnIssuer(t)
	seedPrincipalMCPConnectGrant(t, f.fixtureContext(t), f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), second.ToolsetID.UUID)

	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp := decodeSharedTokenResponse(t, w)
	require.Empty(t, resp.RefreshToken)

	claims := unverifiedClaims(t, resp.AccessToken)
	require.Equal(t, issuerURL, claims["iss"])
	require.Equal(t, []any{urn.NewUserSessionIssuerMCPServers(f.fx.target.UserSessionIssuerID).String()}, claims["aud"])
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), claims["sub"])

	jti, ok := claims["jti"].(string)
	require.True(t, ok)
	policy, err := usersessionsrepo.New(f.ti.conn).GetUserSessionPolicyByJTI(t.Context(), usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: f.fx.target.UserSessionIssuerID, Jti: jti})
	require.NoError(t, err)
	require.False(t, policy.Refreshable)
	require.Len(t, f.workloadSessions(t), 1)

	for _, endpoint := range []*mcp.ResolvedMcpEndpoint{workloadSessionEndpoint(f.fx), second} {
		_, err := f.applyIssuerGate(t, resp.AccessToken, endpoint)
		require.NoError(t, err, "every server the agent may reach admits the session: %s", endpoint.Slug)
	}
}

// An organization issuer's MCP servers can span projects. One session minted
// without a resource reaches each of them, and every request acts in the
// project of the server it reaches.
func TestSharedAuthorizationServer_OrganizationWorkloadWithoutResourceReachesMultipleProjects(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	ctx := f.fixtureContext(t)
	issuer, err := usersessionsrepo.New(f.ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(f.fx.orgID),
		Slug:               "organization-workload-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	setIssuerMode(t, ctx, f.ti, issuer.ID, "shared", "")
	issuerURL := f.ti.serverURL.String() + "/oauth/usi/" + issuer.ID.String()

	projectIDs := []uuid.UUID{f.fx.target.ProjectID, newLiveWorkloadProject(t, f.ti.conn, f.fx.orgID)}
	endpoints := make([]*mcp.ResolvedMcpEndpoint, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		projectCtx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
			ActiveOrganizationID: f.fx.orgID,
			ProjectID:            &projectID,
		})
		slug := seedPublicServerOnIssuer(t, projectCtx, f.ti, issuer.ID)
		endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(projectCtx, testenv.NewLogger(t), slug, "mcp")
		require.NoError(t, err)
		seedPrincipalMCPConnectGrant(t, projectCtx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), endpoint.McpServerID.UUID)
		endpoints = append(endpoints, endpoint)
	}

	w := sharedToken(t, t.Context(), f.ti, issuer.ID.String(), issuerWorkloadForm(f.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	response := decodeSharedTokenResponse(t, w)
	require.Empty(t, response.RefreshToken)

	for _, endpoint := range endpoints {
		admitted, _, _, err := f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), response.AccessToken, f.ti.serverURL.String(), endpoint)
		require.NoError(t, err)
		authCtx := requireProjectAuthContext(t, admitted)
		require.Equal(t, endpoint.ProjectID, *authCtx.ProjectID, "each request acts in the target server's project")
		require.Equal(t, f.fx.orgID, authCtx.ActiveOrganizationID)
	}
}

// A server of the issuer the assigned agent may not connect to answers 403
// insufficient_scope, not invalid_token: the token still works at the other
// servers, and a fresh one would be refused here too.
func TestSharedAuthorizationServer_WorkloadWithoutResourceIsForbiddenWhereAgentMayNotConnect(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	unreachable := f.seedToolsetOnIssuer(t)

	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken

	gate, err := f.applyIssuerGate(t, token, unreachable)
	requireShareableCode(t, err, oops.CodeForbidden)
	require.Contains(t, gate.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`)

	_, err = f.applyIssuerGate(t, token, workloadSessionEndpoint(f.fx))
	require.NoError(t, err, "a refusal at one server leaves the token valid at the others")
}

// Taking the issuer out of shared mode retires the sessions its shared
// authorization server minted for all of its servers.
func TestSharedAuthorizationServer_WorkloadWithoutResourceRetiredWithSharedMode(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)

	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken

	setIssuerMode(t, f.fixtureContext(t), f.ti, f.fx.target.UserSessionIssuerID, "endpoint", "")

	gate, err := f.applyIssuerGate(t, token, workloadSessionEndpoint(f.fx))
	requireShareableCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, gate.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
}

// The audience naming all of an issuer's MCP servers is only ever minted for
// a workload by the issuer's shared authorization server. A token carrying it
// for any other subject, or naming another authorization server, is refused.
func TestSharedAuthorizationServer_IssuerWideAudienceRequiresWorkloadFromSharedServer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	audience := urn.NewUserSessionIssuerMCPServers(f.fx.target.UserSessionIssuerID).String()
	expiresAt := time.Now().Add(time.Hour)

	cases := map[string]struct {
		subject urn.SessionSubject
		issuer  string
	}{
		"user subject":               {subject: urn.NewUserSubject(f.fx.userID), issuer: issuerURL},
		"per-endpoint issuer":        {subject: urn.NewWorkloadSubject(f.issuerID, f.subject), issuer: f.advertisedIssuer},
		"another shared issuer":      {subject: urn.NewWorkloadSubject(f.issuerID, f.subject), issuer: f.ti.serverURL.String() + "/oauth/usi/" + uuid.NewString()},
		"shared issuer, unknown jti": {subject: urn.NewWorkloadSubject(f.issuerID, f.subject), issuer: issuerURL},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
				Subject:   tc.subject,
				Audience:  audience,
				Issuer:    tc.issuer,
				ExpiresAt: &expiresAt,
				ClientID:  "",
				JTI:       "",
			})
			require.NoError(t, err)

			_, err = f.applyIssuerGate(t, token, workloadSessionEndpoint(f.fx))
			requireShareableCode(t, err, oops.CodeUnauthorized)
		})
	}
}

// The workload must still have a live assigned agent: admission runs before
// anything is minted, as it does for a grant naming a resource.
func TestSharedAuthorizationServer_WorkloadWithoutResourceRequiresAssignedAgent(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	unassigned := "repo:acme/payments-api:ref:refs/heads/unassigned"
	require.NoError(t, testrepo.New(f.ti.conn).CreateWorkloadIdentityAdmissionFixture(t.Context(), testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   f.fx.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: f.issuerID,
		Subject:          unassigned,
	}))

	assertion := oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", oauthtest.WorkloadClaims(f.issuer, unassigned, issuerURL))
	requireWorkloadGrantRefused(t, sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(assertion)))
	require.Empty(t, f.workloadSessions(t))
}

// Only the clientless workload grant may omit the resource. A request
// presenting client authentication is the ID-JAG exchange, which must still
// name the one MCP server it is for.
func TestSharedAuthorizationServer_ClientAuthenticatedAssertionStillRequiresResource(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	form := issuerWorkloadForm(f.assertion(t, issuerURL))
	form.Set(oauthwire.ParamClientID, "unknown-client")

	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid_request", decodeSharedTokenResponse(t, w).Error)
	require.Empty(t, f.workloadSessions(t))
}

// The token names its issuer's MCP servers, not every server the agent may
// reach: a server of another issuer refuses it as a token minted for someone
// else, even in the same project, with that issuer also in shared mode, and
// with the agent allowed to connect there.
func TestSharedAuthorizationServer_WorkloadWithoutResourceRefusedAtAnotherIssuersServer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	ctx := f.fixtureContext(t)

	other, err := usersessionsrepo.New(f.ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          f.fx.target.ProjectID,
		OrganizationID:     conv.ToPGText(f.fx.orgID),
		Slug:               "other-shared-usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	setIssuerMode(t, ctx, f.ti, other.ID, "shared", "")
	elsewhere := f.seedToolsetOnUserSessionIssuer(t, other.ID)
	seedPrincipalMCPConnectGrant(t, ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), elsewhere.ToolsetID.UUID)

	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken

	gate, err := f.applyIssuerGate(t, token, elsewhere)
	requireShareableCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, gate.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	_, err = f.applyIssuerGate(t, token, workloadSessionEndpoint(f.fx))
	require.NoError(t, err, "the token still works at its own issuer's servers")
}

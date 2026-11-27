package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeProjectGateway_WorkloadMachineAccess(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)
	fixture := seedOrganizationGateway(t, f.ctx, f.ti)
	grantedID := mcpServerBySlug(t, f.ctx, f.ti, *authCtx.ProjectID, fixture.granted)
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedID.String())
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), grantedID)
	root := strings.TrimSuffix(f.ti.serverURL.String(), "/") + "/mcp/gateway/" + fixture.organizationSlug + "/projects/" + fixture.projectSlug

	_, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.False(t, ok)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, f.fx.orgID, true)
	target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, root+"/token", target.TokenEndpoint)
	require.Equal(t, root, target.Resource)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, target.Audience)},
		oauthwire.ParamResource:  {target.Resource},
	}
	router := goahttp.NewMuxer()
	mcp.Attach(router, f.ti.service.Service, nil)
	request := httptest.NewRequest(http.MethodPost, target.TokenEndpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	bearer := decodeSharedTokenResponse(t, response).AccessToken
	require.NotEmpty(t, bearer)

	listed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), fixture.open)
	require.Contains(t, listed.Body.String(), fixture.granted)

	_, err = serveOrganizationGatewayHTTP(t, f.ctx, f.ti, fixture.organizationSlug, bearer, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServeProjectGateway_AgentMachineAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	fixture := seedOrganizationGateway(t, ctx, ti)
	grantedID := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fixture.granted)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Project gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), grantedID)
	token := mintAgentGatewayKey(t, ctx, ti, agent.ID, grantedID)

	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	anonymousToken := mintProjectGatewayBearerForSubject(t, ti, fixture, fixture.projectSlug, urn.NewAnonymousSubject(uuid.NewString()))
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, anonymousToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	assistantToken := mintForgedAssistantRuntimeJWT(t, authCtx, createAssistant(t, ti, authCtx, "Project gateway"))
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, assistantToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), fixture.granted)
	require.NotContains(t, listed.Body.String(), fixture.denied)
	require.Contains(t, listed.Body.String(), fixture.open)

	_, err = serveOrganizationGatewayHTTP(t, ctx, ti, fixture.organizationSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

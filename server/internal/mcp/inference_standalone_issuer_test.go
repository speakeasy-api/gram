package mcp_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

func TestWorkloadInference_Standalone(t *testing.T) {
	t.Parallel()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	jwksURI := oauthtest.DiscoverWorkloadJWKSURI(t, issuer)
	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardian.WithTLSRootCAs(issuer.RootCAs()))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := guardian.NewUnsafePolicy(ti.tracerProvider, []string{}, guardian.WithTLSRootCAs(issuer.RootCAs()))
	require.NoError(t, err)
	service := usersessions.NewService(ti.logger, ti.tracerProvider, testenv.NewMeterProvider(t), ti.conn,
		ti.sessionManager, ti.chatSessionsManager, ti.authzEngine, ti.audit, policy, nil, ti.enc,
		usersessions.NewSigner("test-jwt-secret"), ti.serverURL.String(),
		authserver.Hosts{ServerURL: ti.serverURL.String()}, nil)
	created, err := service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		Slug: "standalone-inference", AuthorizationServerMode: "shared",
		AuthnChallengeMode: "interactive", SessionDurationHours: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "shared", created.AuthorizationServerMode)

	// Construct only the identity fixture: no MCP server, toolset, or MCP grant.
	fx := agentConsentFixture{orgID: orgID, userID: authCtx.UserID}
	agent := createConsentAgent(t, ctx, ti, fx, "Standalone inference agent")
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: orgID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name: "standalone-workload", Issuer: issuer.OAuth21URL, JwksUri: jwksURI,
	})
	require.NoError(t, err)
	const subject = "repo:example/inference:ref:refs/heads/main"
	require.NoError(t, fixtures.CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID: orgID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID, Subject: subject,
	}))
	assignmentID := assignAgentToWorkload(t, ctx, ti, orgID, issuerID, subject, agent.ID)
	f := workloadGrantFixture{ctx: ctx, ti: ti, fx: fx, issuer: issuer, issuerID: issuerID,
		agentID: agent.ID, subject: subject, assignmentID: assignmentID}
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), projectID)
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), projectID)

	issuerURL := ti.serverURL.String() + "/oauth/usi/" + created.ID
	w := sharedToken(t, t.Context(), ti, created.ID, url.Values{
		"grant_type": {workloadGrantJWTBearer}, "assertion": {f.assertion(t, issuerURL)},
		"resource": {mcp.InferenceResource}, "project_id": {projectID.String()},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	response := decodeSharedTokenResponse(t, w)
	require.NotEmpty(t, response.AccessToken)
	require.Empty(t, response.RefreshToken)
	claims := unverifiedClaims(t, response.AccessToken)
	require.Equal(t, issuerURL, claims["iss"])
	require.Equal(t, []any{mcp.InferenceResource}, claims["aud"])
	require.Equal(t, urn.NewWorkloadSubject(issuerID, subject).String(), claims["sub"])

	authorized, handled, err := ti.service.AuthorizeWorkloadInference(t.Context(), response.AccessToken)
	require.NoError(t, err)
	require.True(t, handled)
	inferenceAuth, ok := contextvalues.GetAuthContext(authorized)
	require.True(t, ok)
	require.Equal(t, orgID, inferenceAuth.ActiveOrganizationID)
	require.NotNil(t, inferenceAuth.ProjectID)
	require.Equal(t, projectID, *inferenceAuth.ProjectID)

	toolsets, err := toolsetsrepo.New(ti.conn).ListToolsetsByProject(ctx, projectID)
	require.NoError(t, err)
	servers, err := mcpserversrepo.New(ti.conn).ListMCPServersByProjectID(ctx, mcpserversrepo.ListMCPServersByProjectIDParams{ProjectID: projectID})
	require.NoError(t, err)
	require.Empty(t, toolsets, "standalone inference must not configure a toolset")
	require.Empty(t, servers, "standalone inference must not configure an MCP server")
}

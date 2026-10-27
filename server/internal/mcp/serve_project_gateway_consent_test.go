package mcp_test

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProjectGateway_ConsentRejectsAnotherProjectsClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	// A public server in that project authenticates through the client, so
	// the person reaches it and the client is theirs to connect.
	otherMeta := createMetaMcpEndpoint(t, ctx, ti.conn, otherProject.ID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	otherSlug := "other-public-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, otherProject.ID, otherMeta.ID, "other public", otherSlug, 0, mcpservers.VisibilityPublic)
	otherServer, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(otherSlug), ProjectID: otherProject.ID})
	require.NoError(t, err)
	client := createConsentRemoteClient(t, ctx, ti.conn, otherProject.ID, authCtx.ActiveOrganizationID, "orggw-"+uuid.NewString()[:8], "", []uuid.UUID{otherServer.UserSessionIssuerID.UUID, fx.issuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, authCtx.ActiveOrganizationID, otherProject.ID, []uuid.UUID{otherServer.UserSessionIssuerID.UUID}))

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug+"/projects/"+fx.projectSlug, "mcp/gateway")
	require.NoError(t, err)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(t.Context(), mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:    "org-gateway-mcp-client",
		RedirectURI: "http://example.com/cb",
		CSRFToken:   "csrf-token",
		Subject:     &fx.subject,
		CreatedAt:   time.Now(),
	}))

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", client.String())
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	err = ti.service.ServeConsentAction(w, req, endpoint)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Contains(t, err.Error(), "unknown remote session client")
	require.Empty(t, w.Header().Get("Location"), "must not start a login for a client outside this project")
}

package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestHostedConsentSetupPresentation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		stored        pgtype.Bool
		placements    int
		invalidClient bool
		incomplete    bool
	}{
		{name: "required without placement", stored: pgtype.Bool{Bool: true, Valid: true}, incomplete: true},
		{name: "undecided without placement", incomplete: true},
		{name: "undecided with one placement", placements: 1},
		{name: "undecided with a deleted registration", placements: 1, invalidClient: true, incomplete: true},
		{name: "undecided with ambiguous placements", placements: 2, incomplete: true},
		{name: "downstream sign-in only", stored: pgtype.Bool{Bool: false, Valid: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			project, org := consentTestTenant(t, ctx)
			issuer := createUserSessionIssuer(t, ctx, ti.conn, project)
			slug := "hosted-setup"
			toolset := createPublicMCPToolset(t, ctx, ti, requireProjectAuthContext(t, ctx), slug)
			hosted := setToolsetIssuer(t, ctx, ti.conn, project, toolset, issuer)
			endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, project, org, issuer, hosted.McpSlug.String)
			endpoint.McpServerID, endpoint.ToolsetID = conv.ToNullUUID(toolset.ID), conv.ToNullUUID(toolset.ID)
			fx := consentActionFixture{ti: ti, endpoint: endpoint, stateID: stateID, projectID: project, orgID: org, shared: issuer, subject: subject}
			registerPageClient(t, ctx, fx)
			q := authclientbackfill.New(ti.conn)
			const assignedIssuerURL = "https://hosted-exact-consent.example.com"
			for i := range tc.placements {
				baseURL := ""
				if i == 0 {
					baseURL = assignedIssuerURL
				}
				client := createConsentRemoteClient(t, ctx, ti.conn, project, org, "hosted-"+uuid.NewString()[:8], baseURL, nil)
				assignHostedOAuth(t, ctx, ti.conn, org, project, toolset.ID, client)
				if tc.invalidClient {
					require.NoError(t, q.SoftDeleteIssuerOfClientFixture(ctx, client))
				}
			}
			require.NoError(t, q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: toolset.ID, RequiresUpstreamAuth: tc.stored}))
			bearer := mintUserSessionBearerForSubject(t, ti, hosted, subject)
			request := func() (*httptest.ResponseRecorder, error) {
				return servePublicHTTP(t, context.Background(), ti, hosted.McpSlug.String, makeInitializeBody(), bearer, nil)
			}
			before, beforeErr := request()
			code, page, location := render(t, fx)
			after, afterErr := request()
			require.Equal(t, before.Code, after.Code)
			require.Equal(t, before.Header().Get("WWW-Authenticate"), after.Header().Get("WWW-Authenticate"))
			if beforeErr != nil {
				require.ErrorContains(t, afterErr, beforeErr.Error())
			} else {
				require.NoError(t, afterErr)
			}
			if tc.incomplete {
				require.Contains(t, page, "An administrator must finish upstream authentication setup")
				require.NotContains(t, page, "consent-approve-button")
				w, err := postApproveResult(t, ctx, fx)
				require.ErrorContains(t, err, "An administrator must finish upstream authentication setup")
				require.Empty(t, w.Header().Get("Location"))
			} else if tc.placements == 1 {
				require.NotContains(t, page, "An administrator must finish upstream authentication setup")
				if code == http.StatusSeeOther {
					require.NotNil(t, location)
					require.Equal(t, assignedIssuerURL+"/authorize", location.Scheme+"://"+location.Host+location.Path)
				} else {
					require.Contains(t, page, "Connect")
				}
			} else {
				_ = approveConsent(t, ctx, fx)
			}
			stored, err := testrepo.New(ti.conn).GetMCPServerRequiresUpstreamAuthFixture(ctx, toolset.ID)
			require.NoError(t, err)
			require.Equal(t, tc.stored, stored, "consent does not persist an upstream decision")
		})
	}
}

func TestGatewayHostedSetupNoticeDoesNotAdmitMissingPlacement(t *testing.T) {
	t.Parallel()
	ctx, fx, gateway := seedMetaConsentEndpoint(t, "hosted-setup-gateway")
	registerPageClient(t, ctx, fx)
	member := seedHostedMetaMember(t, ctx, fx.ti, gateway, "Hosted missing registration", 0, mcpservers.VisibilityPublic)
	require.NoError(t, authclientbackfill.New(fx.ti.conn).SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: member.serverID, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
	query := url.Values{"state": {fx.stateID}, "server": {member.slug}}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.endpoint.Slug+"/connect?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, req.WithContext(ctx), fx.endpoint))
	require.Contains(t, w.Body.String(), "An administrator must finish upstream authentication setup")
	require.NotContains(t, w.Body.String(), "consent-approve-button")

	approval, err := postApproveResult(t, ctx, fx)
	require.ErrorContains(t, err, "An administrator must finish upstream authentication setup")
	require.Empty(t, approval.Header().Get("Location"))
}

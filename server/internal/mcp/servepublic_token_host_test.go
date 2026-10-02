package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// TestServePublic_IssuerGated_TokenBoundToMintHost drives the issuer gate with
// per-endpoint user-session tokens minted on one host and presented on
// another (GRW-26). The audience is the issuer URN on every host, so only the
// `iss` origin tells the hosts apart.
func TestServePublic_IssuerGated_TokenBoundToMintHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := newStatelessRemoteMCPUpstream(t, "ping", nil)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	platformSlug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, platformSlug, "public", issuerID)

	customDomain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "mcp-" + uuid.NewString()[:8] + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	_, err = customdomainsrepo.New(ti.conn).UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		ID:             customDomain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	customSlug := "endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: customDomain.ID, Valid: true},
		McpServerID:    uuid.NullUUID{UUID: mcpServer.ID, Valid: true},
		Slug:           customSlug,
	})
	require.NoError(t, err)

	canonicalBase := ti.serverURL.String()
	const platformBase = "https://ai.gram.example.test"
	customBase := "https://" + customDomain.Domain
	subject := urn.NewUserSubject(mockidp.MockUserID)

	mintToken := func(t *testing.T, issuer, clientID string) string {
		t.Helper()
		token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
			Subject:  subject,
			Audience: urn.NewUserSessionIssuer(issuerID).String(),
			Issuer:   issuer,
			Lifetime: time.Hour,
			ClientID: clientID,
		})
		require.NoError(t, err)
		persistTestUserSession(t, ti, issuerID, subject, jti)
		return token
	}
	withOrigin := func(surface requestorigin.Surface, baseURL, organizationID string) context.Context {
		return requestorigin.WithContext(context.Background(), requestorigin.Origin{
			Surface:          surface,
			BaseURL:          baseURL,
			OrganizationID:   organizationID,
			NetworkIngressID: uuid.Nil,
			NetworkIdentity:  nil,
		})
	}

	// The origins the custom-domains middleware stamps for each host.
	contexts := map[string]context.Context{
		"canonical": withOrigin(requestorigin.SurfacePlatform, canonicalBase, ""),
		"platform":  withOrigin(requestorigin.SurfacePlatform, platformBase, ""),
		"custom": customdomains.WithContext(withOrigin(requestorigin.SurfaceCustomDomain, customBase, authCtx.ActiveOrganizationID), &customdomains.Context{
			OrganizationID: authCtx.ActiveOrganizationID,
			Domain:         customDomain.Domain,
			DomainID:       customDomain.ID,
		}),
		"internal": context.Background(),
	}

	canonicalToken := mintToken(t, canonicalBase+"/mcp/"+platformSlug, "client-a")
	platformToken := mintToken(t, platformBase+"/mcp/"+platformSlug, "client-a")
	customToken := mintToken(t, customBase+"/mcp/"+customSlug, "client-a")
	noIssuerToken := mintToken(t, "", "client-a")
	dashboardToken := mintToken(t, canonicalBase+"/mcp/"+platformSlug, sessiontokens.FirstPartyClientID)

	cases := []struct {
		name     string
		origin   string
		slug     string
		token    string
		accepted bool
	}{
		{name: "platform host token on platform host", origin: "platform", slug: platformSlug, token: platformToken, accepted: true},
		{name: "platform host token refused on canonical host", origin: "canonical", slug: platformSlug, token: platformToken, accepted: false},
		{name: "canonical token on canonical host", origin: "canonical", slug: platformSlug, token: canonicalToken, accepted: true},
		{name: "canonical token refused on platform host", origin: "platform", slug: platformSlug, token: canonicalToken, accepted: false},
		{name: "custom domain token on its custom domain", origin: "custom", slug: customSlug, token: customToken, accepted: true},
		{name: "custom domain token refused on canonical host", origin: "canonical", slug: platformSlug, token: customToken, accepted: false},
		{name: "custom domain token refused on platform host", origin: "platform", slug: platformSlug, token: customToken, accepted: false},
		{name: "canonical token refused on custom domain", origin: "custom", slug: customSlug, token: canonicalToken, accepted: false},
		{name: "token without iss keeps working on custom domain", origin: "custom", slug: customSlug, token: noIssuerToken, accepted: true},
		{name: "token without iss keeps working on platform host", origin: "platform", slug: platformSlug, token: noIssuerToken, accepted: true},
		{name: "dashboard token keeps working on platform host", origin: "platform", slug: platformSlug, token: dashboardToken, accepted: true},
		{name: "dashboard token keeps working on custom domain", origin: "custom", slug: customSlug, token: dashboardToken, accepted: true},
		{name: "internal caller keeps accepting any host", origin: "internal", slug: platformSlug, token: platformToken, accepted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, err := servePublicHTTP(t, contexts[tc.origin], ti, tc.slug, makeInitializeBody(), tc.token, nil)
			if tc.accepted {
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
				return
			}
			require.Error(t, err)
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
			require.Contains(t, w.Header().Get("WWW-Authenticate"), `Bearer resource_metadata="`)
		})
	}
}

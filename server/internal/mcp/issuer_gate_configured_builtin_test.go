package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestConfiguredGatewayBuiltinAgentKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                                    string
		gatewayGrant, memberGrant, liveExact, credential, human bool
	}{
		{name: "explicit machine", gatewayGrant: true, memberGrant: true, liveExact: true, credential: true},
		{name: "no gateway grant", memberGrant: true, liveExact: true, credential: true},
		{name: "gateway without member ceiling", gatewayGrant: true, liveExact: true, credential: true},
		{name: "broad live member", gatewayGrant: true, memberGrant: true, credential: true},
		{name: "missing upstream credential", gatewayGrant: true, memberGrant: true, liveExact: true},
		{name: "human cannot see builtin", gatewayGrant: true, memberGrant: true, liveExact: true, credential: true, human: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			issuerID := createUserSessionIssuer(t, ctx, ti.conn, *ac.ProjectID)
			slug := "builtin-gateway-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *ac.ProjectID, ac.ActiveOrganizationID, slug, issuerID)
			setGatewayMode(t, ctx, ti, gateway.ID, "direct")
			_, err := metamcprepo.New(ti.conn).SetBuiltinMetaMCPMember(ctx, metamcprepo.SetBuiltinMetaMCPMemberParams{ProjectID: conv.ToNullUUID(*ac.ProjectID), MetaMcpServerID: conv.ToNullUUID(gateway.ID), BuiltinKey: builtinmcp.Slack})
			require.NoError(t, err)
			agentID, _ := seedBuiltinAgent(t, ctx, ti, tc.liveExact)
			memberID := builtinmcp.ID(*ac.ProjectID, builtinmcp.Slack)
			if tc.credential {
				environment, err := envrepo.New(ti.conn).GetEnvironmentBySlug(ctx, envrepo.GetEnvironmentBySlugParams{Slug: "default", ProjectID: *ac.ProjectID})
				require.NoError(t, err)
				_, err = envrepo.New(ti.conn).UpsertEnvironmentEntry(ctx, envrepo.UpsertEnvironmentEntryParams{Name: "SLACK_BOT_TOKEN", Value: "fixture-token", EnvironmentID: environment.ID, ProjectID: *ac.ProjectID})
				require.NoError(t, err)
			}
			var grants []authz.Grant
			if tc.gatewayGrant {
				seedPrincipalMCPConnectGrant(t, ctx, ti, ac.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()), gateway.ID)
				seedUserMCPConnectGrant(t, ctx, ti.conn, ac.ActiveOrganizationID, ac.UserID, gateway.ID.String())
				grants = append(grants, authz.Grant{Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, gateway.ID.String())})
			}
			if tc.memberGrant {
				resourceID := memberID.String()
				grants = append(grants, authz.Grant{Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, resourceID)})
			}
			for i := range grants {
				grants[i].Selector[authz.SelectorKeyProjectID] = ac.ProjectID.String()
			}
			policy, err := runtimepolicy.NewDelegatedPolicyV1(grants)
			require.NoError(t, err)
			delegated, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
			require.NoError(t, err)
			token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
			require.NoError(t, err)
			_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{OrganizationID: ac.ActiveOrganizationID, CreatedByUserID: ac.UserID, Name: "Configured builtin key", KeyPrefix: prefix, KeyHash: hash, SubjectUrn: conv.ToPGText(urn.NewAgentSubject(agentID).String()), DelegatedGrants: delegated, DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
			require.NoError(t, err)
			if tc.human {
				subject := urn.NewUserSubject(ac.UserID)
				var jti string
				token, jti, err = usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour})
				require.NoError(t, err)
				_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
				require.NoError(t, err)
			}
			visible := tc.gatewayGrant && tc.memberGrant && tc.liveExact && tc.credential && !tc.human
			for _, method := range []string{"tools/list", mcpversions.MethodServerDiscover} {
				params := map[string]any{}
				headers := map[string]string{}
				if method == mcpversions.MethodServerDiscover {
					params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": mcpversions.Version20260728, "io.modelcontextprotocol/clientInfo": map[string]any{"name": "fixture", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
					headers[mcpversions.HTTPHeader] = mcpversions.Version20260728
					headers["Mcp-Method"] = method
				}
				response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, method, params), token, headers)
				if !tc.gatewayGrant {
					assertAgentKeyUnauthorized(t, response, err)
					continue
				}
				require.NoError(t, err, "%s: %s", method, response.Body.String())
				require.Equal(t, http.StatusOK, response.Code)
				require.Empty(t, response.Header().Get("WWW-Authenticate"))
				require.Contains(t, decodeRPCResponse(t, response), "result", method)
				if method == "tools/list" {
					require.Equal(t, visible, strings.Contains(response.Body.String(), "platform_slack_send_message"), method)
				} else {
					// server/discover describes the gateway, not member tools.
					require.Contains(t, response.Body.String(), "Speakeasy Gateway")
				}
			}
			if tc.gatewayGrant && !visible {
				response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "builtin_slack--platform_slack_send_message", "arguments": map[string]any{}}), token, nil)
				require.NoError(t, err)
				require.Contains(t, decodeRPCResponse(t, response), "error")
				require.Contains(t, response.Body.String(), "unknown server")
			}
			if visible {
				endpoint := mcp.ResolvedMcpEndpoint{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, RouteBase: mcproute.MCP, Slug: slug, UserSessionIssuerID: issuerID, MetaMcpServerID: conv.ToNullUUID(gateway.ID)}
				for _, mismatch := range []string{"organization", "project", "gateway"} {
					other := endpoint
					switch mismatch {
					case "organization":
						other.OrganizationID = "org-other"
					case "project":
						other.ProjectID = uuid.New()
					case "gateway":
						other.MetaMcpServerID = conv.ToNullUUID(uuid.New())
					}
					response := httptest.NewRecorder()
					_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), response, token, ti.serverURL.String(), &other)
					assertAgentKeyUnauthorized(t, response, err)
				}
			}
		})
	}
}

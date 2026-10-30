package mcp_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// workloadGatewayFixture is a stored gateway on the workload fixture's issuer
// with two private members: the agent may connect to one, its owner to both.
type workloadGatewayFixture struct {
	workloadGrantFixture

	slug     string
	resource string
	metaID   uuid.UUID

	// granted is the member the agent may connect to, by grantID.
	granted         string
	grantedServerID uuid.UUID
	grantID         uuid.UUID

	// denied is the member the agent has no grant on.
	denied string
}

func newWorkloadGatewayFixture(t *testing.T) workloadGatewayFixture {
	t.Helper()

	return newWorkloadGatewayFixtureFrom(t, newWorkloadGrantFixture(t))
}

func newWorkloadGatewayFixtureFrom(t *testing.T, f workloadGrantFixture) workloadGatewayFixture {
	t.Helper()

	ctx := f.fixtureContext(t)
	slug := "agent-gateway-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.fx.target.ProjectID, f.fx.orgID, slug, f.fx.target.UserSessionIssuerID)
	granted := "granted-" + uuid.NewString()[:8]
	denied := "denied-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, f.ti.conn, f.fx.target.ProjectID, meta.ID, "granted member", granted, 0, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, f.ti.conn, f.fx.target.ProjectID, meta.ID, "denied member", denied, 1, mcpservers.VisibilityPrivate)
	grantedServerID := mcpServerBySlug(t, ctx, f.ti, f.fx.target.ProjectID, granted)
	grantID := seedPrincipalMCPConnectGrant(t, ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), grantedServerID)

	return workloadGatewayFixture{
		workloadGrantFixture: f,
		slug:                 slug,
		resource:             strings.TrimSuffix(f.ti.serverURL.String(), "/") + "/mcp/" + slug,
		metaID:               meta.ID,
		granted:              granted,
		grantedServerID:      grantedServerID,
		grantID:              grantID,
		denied:               denied,
	}
}

// anotherGateway is a second, empty gateway on the same issuer and project.
func (g workloadGatewayFixture) anotherGateway(t *testing.T) workloadGatewayFixture {
	t.Helper()

	other := g
	other.slug = "agent-gateway-" + uuid.NewString()[:8]
	other.resource = strings.TrimSuffix(g.ti.serverURL.String(), "/") + "/mcp/" + other.slug
	other.metaID = createMetaMcpEndpoint(t, g.fixtureContext(t), g.ti.conn, g.fx.target.ProjectID, g.fx.orgID, other.slug, g.fx.target.UserSessionIssuerID).ID
	other.granted, other.grantedServerID, other.grantID, other.denied = "", uuid.Nil, uuid.Nil, ""
	return other
}

// seedPublicMember attaches a public member the agent has no grant on.
func (g workloadGatewayFixture) seedPublicMember(t *testing.T) (string, uuid.UUID) {
	t.Helper()

	ctx := g.fixtureContext(t)
	slug := "public-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, g.ti.conn, g.fx.target.ProjectID, g.metaID, "public member", slug, 2, mcpservers.VisibilityPublic)
	return slug, mcpServerBySlug(t, ctx, g.ti, g.fx.target.ProjectID, slug)
}

func (g workloadGatewayFixture) grantAgent(t *testing.T, resourceID uuid.UUID) uuid.UUID {
	t.Helper()

	return seedPrincipalMCPConnectGrant(t, g.fixtureContext(t), g.ti, g.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, g.agentID.String()), resourceID)
}

// projectConnectGrant is a ceiling that bounds nothing in the project.
func (g workloadGatewayFixture) projectConnectGrant() authz.Grant {
	return authz.Grant{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    g.fx.target.ProjectID.String(),
		},
		Inferred: false,
	}
}

func (g workloadGatewayFixture) agentKey(t *testing.T, ceiling ...authz.Grant) string {
	t.Helper()

	policy, err := runtimepolicy.NewDelegatedPolicyV1(ceiling)
	require.NoError(t, err)
	delegated, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(g.ti.conn).CreateAgentAPIKey(g.ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         g.fx.orgID,
		CreatedByUserID:        g.fx.userID,
		Name:                   "gateway agent key " + uuid.NewString()[:8],
		KeyPrefix:              prefix,
		KeyHash:                hash,
		SubjectUrn:             pgtype.Text{String: urn.NewAgentSubject(g.agentID).String(), Valid: true},
		DelegatedGrants:        delegated,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return token
}

// principalCredential mints a credential for the agent, authorized by its
// owner, who needs project read to authorize one.
func (g workloadGatewayFixture) principalCredential(t *testing.T) string {
	t.Helper()

	seedPrincipalGrant(t, g.fixtureContext(t), g.ti, g.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, g.fx.userID), authz.ScopeProjectRead, g.fx.target.ProjectID.String())
	token, _, err := g.ti.principalCredentials.Mint(principalcredential.Credential{
		OrganizationID:   g.fx.orgID,
		ProjectID:        g.fx.target.ProjectID,
		Principal:        urn.NewPrincipal(urn.PrincipalTypeAgent, g.agentID.String()),
		AuthorizerUserID: g.fx.userID,
	})
	require.NoError(t, err)
	return token
}

func (g workloadGatewayFixture) callTool(t *testing.T, token, tool string, arguments map[string]any) map[string]json.RawMessage {
	t.Helper()

	w, err := servePublicHTTP(t, t.Context(), g.ti, g.slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      tool,
		"arguments": arguments,
	}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return decodeRPCResponse(t, w)
}

func (g workloadGatewayFixture) seedToolsetMember(t *testing.T, toolsetID uuid.UUID) string {
	t.Helper()

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	slug := "toolset-member-" + uuid.NewString()[:8]
	_, err = mcpserversrepo.New(g.ti.conn).CreateMCPServer(g.fixtureContext(t), mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           g.fx.target.ProjectID,
		Name:                conv.ToPGText("toolset member"),
		Slug:                conv.ToPGText(slug),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:          mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	g.attachMember(t, serverID)
	return slug
}

func (g workloadGatewayFixture) endpoint() *mcp.ResolvedMcpEndpoint {
	return &mcp.ResolvedMcpEndpoint{
		OrganizationID:      g.fx.orgID,
		ProjectID:           g.fx.target.ProjectID,
		RouteBase:           mcproute.MCP,
		Slug:                g.slug,
		MetaMcpServerID:     uuid.NullUUID{UUID: g.metaID, Valid: true},
		UserSessionIssuerID: g.fx.target.UserSessionIssuerID,
	}
}

func (g workloadGatewayFixture) attachMember(t *testing.T, serverID uuid.UUID) {
	t.Helper()

	_, err := metamcprepo.New(g.ti.conn).CreateMetaMCPMember(g.fixtureContext(t), metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       g.fx.target.ProjectID,
		MetaMcpServerID: g.metaID,
		McpServerID:     serverID,
		SortOrder:       0,
	})
	require.NoError(t, err)
}

// recreate deletes the gateway and creates another at its slug, issuer and
// project, serving the granted member.
func (g workloadGatewayFixture) recreate(t *testing.T) workloadGatewayFixture {
	t.Helper()

	ctx := g.fixtureContext(t)
	project := g.fx.target.ProjectID
	_, err := metamcprepo.New(g.ti.conn).DeleteMetaMCPMembersByMetaMCPServerID(ctx, metamcprepo.DeleteMetaMCPMembersByMetaMCPServerIDParams{MetaMcpServerID: g.metaID, ProjectID: project})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(g.ti.conn).SoftDeleteMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMetaMCPServerIDParams{MetaMcpServerID: g.metaID, ProjectID: project})
	require.NoError(t, err)
	_, err = metamcprepo.New(g.ti.conn).DeleteMetaMCPServer(ctx, metamcprepo.DeleteMetaMCPServerParams{ID: g.metaID, OrganizationID: g.fx.orgID, ProjectID: project})
	require.NoError(t, err)

	recreated := g
	recreated.metaID = createMetaMcpEndpoint(t, ctx, g.ti.conn, project, g.fx.orgID, g.slug, g.fx.target.UserSessionIssuerID).ID
	recreated.attachMember(t, g.grantedServerID)
	return recreated
}

// requireInvalidToken asserts invalid_token, which tells a workload to
// exchange again.
func (g workloadGatewayFixture) requireInvalidToken(t *testing.T, token string) {
	t.Helper()

	w, err := servePublicHTTP(t, t.Context(), g.ti, g.slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolListServers,
		"arguments": map[string]any{},
	}), token, nil)
	requireShareableCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
}

// requireUnknownServer asserts the member reads as nonexistent to token.
func (g workloadGatewayFixture) requireUnknownServer(t *testing.T, token, slug string) {
	t.Helper()

	envelope := g.callTool(t, token, metamcp.ToolDescribeServer, map[string]any{"server": slug})
	require.Contains(t, string(envelope["error"]), "unknown server")
}

// exchangeAtGateway posts a workload grant naming the gateway as its resource.
func (g workloadGatewayFixture) exchangeAtGateway(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	issuer, _ := fetchAdvertisedIssuer(t, g.ctx, g.ti, g.slug)
	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {g.assertion(t, issuer)},
		oauthwire.ParamResource:  {g.resource},
	}
	return postForm(t, g.ti, g.slug, "token", form)
}

func (g workloadGatewayFixture) listServers(t *testing.T, token string) []string {
	t.Helper()

	w, err := servePublicHTTP(t, t.Context(), g.ti, g.slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolListServers,
		"arguments": map[string]any{},
	}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var envelope struct {
		Result struct {
			StructuredContent metamcp.ListServersResult `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())
	slugs := make([]string, 0, len(envelope.Result.StructuredContent.Servers))
	for _, server := range envelope.Result.StructuredContent.Servers {
		slugs = append(slugs, server.Slug)
	}
	return slugs
}

func TestWorkloadGateway_ResourceGrantServesReachableMembers(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)

	grants, ok := fetchASMetadata(t, g.ti, g.slug)["grant_types_supported"].([]any)
	require.True(t, ok)
	require.Contains(t, grants, workloadGrantJWTBearer, "a stored gateway advertises the workload grant")

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, []any{g.resource}, claims["aud"])
	require.Equal(t, urn.NewWorkloadSubject(g.issuerID, g.subject).String(), claims["sub"])

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotContains(t, resp, "refresh_token")
	token, ok := resp["access_token"].(string)
	require.True(t, ok)

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
}

func TestWorkloadGateway_TokenTargetResolvesTheGateway(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)

	target, ok, err := g.ti.service.WorkloadTokenTargetFor(t.Context(), g.resource)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, g.resource+"/token", target.TokenEndpoint)
	require.Equal(t, g.resource, target.Resource)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {g.assertion(t, target.Audience)},
		oauthwire.ParamResource:  {target.Resource},
	}
	w := postForm(t, g.ti, g.slug, "token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{g.granted}, g.listServers(t, decodeSharedTokenResponse(t, w).AccessToken))
}

// The session reaches the fixture's server through the gateway, but not at
// that server's own endpoint.
func TestWorkloadGateway_ResourceGrantSessionIsBoundToTheGateway(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	member := g.seedToolsetMember(t, g.fx.toolset.ID)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.ElementsMatch(t, []string{g.granted, member}, g.listServers(t, token))

	refused, err := g.applyIssuerGate(t, token, workloadSessionEndpoint(g.fx))
	requireShareableCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, refused.Header().Get("WWW-Authenticate"), `error="invalid_token"`, "a gateway session must not admit at a member's own server")
}

func TestWorkloadGateway_SessionIsRefusedAtAnotherGateway(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	other := g.anotherGateway(t)
	other.attachMember(t, g.grantedServerID)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.Equal(t, []string{g.granted}, g.listServers(t, token))

	other.requireInvalidToken(t, token)
}

// The refusal answers like an untrusted identity, but logs its own reason.
func TestWorkloadGateway_ResourceGrantRefusedWhenNoMemberIsReachable(t *testing.T) {
	t.Parallel()

	var logs syncBuffer
	g := newWorkloadGatewayFixtureFrom(t, newWorkloadGrantFixtureWithLogger(t, slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: false, ReplaceAttr: nil}))))
	rows, err := accessrepo.New(g.ti.conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: g.grantID, OrganizationID: g.fx.orgID})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	requireWorkloadGrantRefused(t, g.exchangeAtGateway(t))
	require.Contains(t, logs.String(), "workload assertion grant refused")
	require.Contains(t, logs.String(), "gateway_member_unreachable")
	require.NotContains(t, logs.String(), "agent_admission_denied")
}

func (g workloadGatewayFixture) deleteToolset(t *testing.T, toolsetID uuid.UUID) {
	t.Helper()

	ctx := g.fixtureContext(t)
	toolset, err := toolsets_repo.New(g.ti.conn).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: g.fx.target.ProjectID})
	require.NoError(t, err)
	_, err = toolsets_repo.New(g.ti.conn).DeleteToolset(ctx, toolsets_repo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: g.fx.target.ProjectID})
	require.NoError(t, err)
}

func TestWorkloadGateway_IssuanceRereadsMembership(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	ctx := g.fixtureContext(t)
	hosted := seedHostedMetaMember(t, ctx, g.ti, g.metaID, "hosted member", 2, mcpservers.VisibilityPublic, "alpha_tool")
	deniedID := mcpServerBySlug(t, ctx, g.ti, g.fx.target.ProjectID, g.denied)

	resources, err := mcp.FreshGatewayMemberResources(ctx, g.ti.conn, g.endpoint(), g.grantedServerID, deniedID, hosted.serverID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{g.grantedServerID.String(), deniedID.String(), hosted.toolsetID.String()}, resources)

	resources, err = mcp.FreshGatewayMemberResources(ctx, g.ti.conn, g.endpoint(), g.grantedServerID)
	require.NoError(t, err)
	require.Equal(t, []string{g.grantedServerID.String()}, resources, "a member the preflight did not reach is not counted")

	_, err = metamcprepo.New(g.ti.conn).DeleteMetaMCPMembersByMCPServerID(ctx, metamcprepo.DeleteMetaMCPMembersByMCPServerIDParams{McpServerID: deniedID, ProjectID: g.fx.target.ProjectID})
	require.NoError(t, err)
	resources, err = mcp.FreshGatewayMemberResources(ctx, g.ti.conn, g.endpoint(), g.grantedServerID, deniedID, hosted.serverID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{g.grantedServerID.String(), hosted.toolsetID.String()}, resources, "a member removed since the preflight is not counted")

	g.deleteToolset(t, hosted.toolsetID)
	resources, err = mcp.FreshGatewayMemberResources(ctx, g.ti.conn, g.endpoint(), g.grantedServerID, hosted.serverID)
	require.NoError(t, err)
	require.Equal(t, []string{g.grantedServerID.String()}, resources, "a hosted member whose toolset stopped serving is not counted")
}

func TestWorkloadGateway_UnservableHostedMemberDoesNotMakeASessionIssuable(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t).anotherGateway(t)
	ctx := g.fixtureContext(t)
	hosted := seedHostedMetaMember(t, ctx, g.ti, g.metaID, "hosted member", 0, mcpservers.VisibilityPublic, "alpha_tool")
	g.grantAgent(t, hosted.toolsetID)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{hosted.slug}, g.listServers(t, decodeSharedTokenResponse(t, w).AccessToken))

	g.deleteToolset(t, hosted.toolsetID)
	requireWorkloadGrantRefused(t, g.exchangeAtGateway(t))
}

// A revoked grant drops its member on the next request; the session stays
// valid.
func TestWorkloadGateway_ResourceGrantRechecksMembersOnEveryRequest(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.Equal(t, []string{g.granted}, g.listServers(t, token))

	_, err := accessrepo.New(g.ti.conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: g.grantID, OrganizationID: g.fx.orgID})
	require.NoError(t, err)

	require.Empty(t, g.listServers(t, token))
}

func TestWorkloadGateway_IssuerWideSessionServesReachableMembers(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	issuerURL := g.sharedIssuer(t)

	w := sharedToken(t, t.Context(), g.ti, g.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(g.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken

	require.Equal(t, []string{g.granted}, g.listServers(t, token))

	_, err := g.applyIssuerGate(t, token, workloadSessionEndpoint(g.fx))
	require.NoError(t, err, "the same session still reaches the issuer's other servers")
}

func TestWorkloadGateway_OwnerPolicyCapsMembers(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	issuerURL := g.sharedIssuer(t)
	deniedServerID := mcpServerBySlug(t, g.fixtureContext(t), g.ti, g.fx.target.ProjectID, g.denied)
	seedPrincipalMCPConnectGrant(t, g.fixtureContext(t), g.ti, g.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, g.agentID.String()), deniedServerID)

	w := sharedToken(t, t.Context(), g.ti, g.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(g.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.ElementsMatch(t, []string{g.granted, g.denied}, g.listServers(t, token))

	_, err := accessrepo.New(g.ti.conn).DeletePrincipalGrantsByPrincipal(t.Context(), accessrepo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: g.fx.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, g.fx.userID),
	})
	require.NoError(t, err)
	seedPrincipalMCPConnectGrant(t, g.fixtureContext(t), g.ti, g.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, g.fx.userID), g.grantedServerID)

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
}

func TestWorkloadGateway_AgentKeyServesReachableMembers(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	deniedServerID := mcpServerBySlug(t, g.fixtureContext(t), g.ti, g.fx.target.ProjectID, g.denied)
	// The agent may connect to both; the key's ceiling names one.
	g.grantAgent(t, deniedServerID)
	token := g.agentKey(t, authz.Grant{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   g.grantedServerID.String(),
			authz.SelectorKeyProjectID:    g.fx.target.ProjectID.String(),
		},
		Inferred: false,
	})

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
}

// As at a public single server, a workload needs its agent's mcp:connect on a
// public member.
func TestWorkloadGateway_PublicMemberNeedsTheAgentsGrant(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	public, publicID := g.seedPublicMember(t)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.Equal(t, []string{g.granted}, g.listServers(t, token))
	g.requireUnknownServer(t, token, public)

	g.grantAgent(t, publicID)
	require.ElementsMatch(t, []string{g.granted, public}, g.listServers(t, token))
}

func TestWorkloadGateway_PublicMemberAloneDoesNotMakeASessionIssuable(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t).anotherGateway(t)
	public, publicID := g.seedPublicMember(t)

	requireWorkloadGrantRefused(t, g.exchangeAtGateway(t))

	g.grantAgent(t, publicID)
	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{public}, g.listServers(t, decodeSharedTokenResponse(t, w).AccessToken))
}

func TestWorkloadGateway_AgentKeyPublicMemberNeedsTheAgentsGrant(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	public, publicID := g.seedPublicMember(t)
	token := g.agentKey(t, g.projectConnectGrant())

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
	g.requireUnknownServer(t, token, public)

	g.grantAgent(t, publicID)
	require.ElementsMatch(t, []string{g.granted, public}, g.listServers(t, token))
}

func TestWorkloadGateway_PrincipalCredentialPublicMemberNeedsTheAgentsGrant(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	public, publicID := g.seedPublicMember(t)
	token := g.principalCredential(t)

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
	g.requireUnknownServer(t, token, public)

	g.grantAgent(t, publicID)
	require.ElementsMatch(t, []string{g.granted, public}, g.listServers(t, token))
}

func TestWorkloadGateway_HostedPublicMemberNeedsTheAgentsGrant(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	hosted := seedHostedMetaMember(t, g.fixtureContext(t), g.ti, g.metaID, "hosted member", 2, mcpservers.VisibilityPublic, "alpha_tool")
	issuerURL := g.sharedIssuer(t)
	w := sharedToken(t, t.Context(), g.ti, g.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(g.assertion(t, issuerURL)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
	g.requireUnknownServer(t, token, hosted.slug)

	g.grantAgent(t, hosted.toolsetID)
	require.ElementsMatch(t, []string{g.granted, hosted.slug}, g.listServers(t, token))
	result := decodeMetaToolResult(t, g.callTool(t, token, metamcp.ToolDescribeServer, map[string]any{"server": hosted.slug}))
	require.False(t, result.IsError, string(result.StructuredContent))
	require.Contains(t, string(result.StructuredContent), hosted.slug+"--alpha_tool")
}

func TestWorkloadGateway_PersonReachesPublicMemberWithoutAGrant(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	public, _ := g.seedPublicMember(t)
	person := seedConsentMember(t, g.ctx, g.ti, g.fx.orgID)
	token := mintMetaIssuerBearer(t, g.ti, g.slug, g.fx.target.UserSessionIssuerID, urn.NewUserSubject(person))

	require.Equal(t, []string{public}, g.listServers(t, token))
}

func TestWorkloadGateway_SessionIsRefusedAtARecreatedGateway(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.Equal(t, []string{g.granted}, g.listServers(t, token))

	recreated := g.recreate(t)
	recreated.requireInvalidToken(t, token)

	w = recreated.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{g.granted}, recreated.listServers(t, decodeSharedTokenResponse(t, w).AccessToken))
}

func TestWorkloadGateway_OrganizationWideAgentKeyAtAnotherProjectsGateway(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	ctx := g.fixtureContext(t)
	project := createSiblingProject(t, ctx, g.ti.conn, g.fx.orgID)
	other := g
	other.slug = "agent-gateway-" + uuid.NewString()[:8]
	other.metaID = createMetaMcpEndpoint(t, ctx, g.ti.conn, project, g.fx.orgID, other.slug, createUserSessionIssuer(t, ctx, g.ti.conn, project)).ID
	member := "other-project-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, g.ti.conn, project, other.metaID, "other project member", member, 0, mcpservers.VisibilityPrivate)
	g.grantAgent(t, mcpServerBySlug(t, ctx, g.ti, project, member))

	projectScoped := g.agentKey(t, g.projectConnectGrant())
	organizationWide := g.agentKey(t, authz.Grant{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector:     authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: authz.WildcardResource},
		Inferred:     false,
	})

	require.Empty(t, other.listServers(t, projectScoped), "the key's ceiling bounds it in every project")
	require.Equal(t, []string{member}, other.listServers(t, organizationWide))
}

// A workload calls an OAuth member with its agent owner's own connection, as
// on direct servers, and never calls an ungranted one.
func TestWorkloadGateway_OAuthMemberUsesTheOwnersConnection(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t).anotherGateway(t)
	ctx := g.fixtureContext(t)
	project, issuer := g.fx.target.ProjectID, g.fx.target.UserSessionIssuerID
	suffix := uuid.NewString()[:8]
	grantedUpstream := newRecordingUpstream(t, "ping")
	deniedUpstream := newRecordingUpstream(t, "ping")
	granted, denied := "granted-upstream-"+suffix, "denied-upstream-"+suffix
	g.grantAgent(t, seedMetaMemberWithUpstream(t, ctx, g.ti.conn, project, g.metaID, "Granted upstream", granted, 0, grantedUpstream.url))
	seedMetaMemberWithUpstream(t, ctx, g.ti.conn, project, g.metaID, "Denied upstream", denied, 1, deniedUpstream.url)

	// A workload's owner credential needs project read.
	owner := urn.NewUserSubject(g.fx.userID)
	seedPrincipalGrant(t, ctx, g.ti, g.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, g.fx.userID), authz.ScopeProjectRead, project.String())
	grantedClient := createConsentRemoteClient(t, ctx, g.ti.conn, project, g.fx.orgID, "owner-granted-"+suffix, "", []uuid.UUID{issuer})
	deniedClient := createConsentRemoteClient(t, ctx, g.ti.conn, project, g.fx.orgID, "owner-denied-"+suffix, "", []uuid.UUID{issuer})
	insertQualifiedRemoteSessionToken(t, ctx, g.ti, issuer, grantedClient, owner, "owner-token-granted", grantedUpstream.url)
	insertQualifiedRemoteSessionToken(t, ctx, g.ti, issuer, deniedClient, owner, "owner-token-denied", deniedUpstream.url)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken
	require.Equal(t, []string{granted}, g.listServers(t, token))

	text, isError := metaToolResultText(t, executeMetaTool(t, g.ti, g.slug, token, granted+"--ping"))
	require.False(t, isError, text)
	require.Contains(t, text, "pong from ping")
	grantedUpstream.requireToolCallAuth(t, "Bearer owner-token-granted", "the granted member is called with the owner's own connection")

	envelope := executeMetaTool(t, g.ti, g.slug, token, denied+"--ping")
	require.Contains(t, string(envelope["error"]), "unknown server")
	require.Zero(t, deniedUpstream.requestCount(), "a member the agent is not granted is never called")
	grantedUpstream.requireNeverCarried(t, "Bearer owner-token-denied")
}

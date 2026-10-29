package mcp_test

import (
	"encoding/json"
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
	"github.com/speakeasy-api/gram/server/internal/authz"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// workloadGatewayFixture is a stored gateway on the workload fixture's user
// session issuer with two private members. The fixture's agent may connect to
// one of them; its owner may connect to both.
type workloadGatewayFixture struct {
	workloadGrantFixture

	// slug is the gateway's MCP slug.
	slug string

	// resource is the gateway's canonical URL on the MCP host.
	resource string

	// granted is the slug of the member the agent may connect to.
	granted string

	// grantedServerID is the mcp_servers id the agent's grant names.
	grantedServerID uuid.UUID

	// grantID is the agent's mcp:connect grant on the granted member.
	grantID uuid.UUID

	// denied is the slug of the member the agent has no grant on.
	denied string
}

func newWorkloadGatewayFixture(t *testing.T) workloadGatewayFixture {
	t.Helper()

	f := newWorkloadGrantFixture(t)
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
		granted:              granted,
		grantedServerID:      grantedServerID,
		grantID:              grantID,
		denied:               denied,
	}
}

// exchangeAtGateway posts the clientless workload grant, naming the gateway as
// its resource, to the gateway's own token endpoint.
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

// listServers calls the gateway's list_servers tool with token and returns
// the slugs it lists.
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

// A workload naming a stored gateway as its resource receives a session bound
// to the gateway, and the gateway lists exactly the members its agent and the
// agent's owner may both connect to.
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

// A headless device resolves a gateway URL to the gateway's own token
// endpoint, naming the gateway as the resource, and the exchange it is told to
// make succeeds.
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

// A gateway session's audience is the gateway: it is refused at the member
// servers it reaches through the gateway, and at the fixture's own server.
func TestWorkloadGateway_ResourceGrantSessionIsBoundToTheGateway(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)

	w := g.exchangeAtGateway(t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w).AccessToken

	_, err := g.applyIssuerGate(t, token, workloadSessionEndpoint(g.fx))
	require.Error(t, err, "a gateway session must not admit at another server of its issuer")
}

// A workload whose agent reaches no member of the gateway is refused at the
// token endpoint rather than handed a session that serves nothing.
func TestWorkloadGateway_ResourceGrantRefusedWhenNoMemberIsReachable(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	rows, err := accessrepo.New(g.ti.conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: g.grantID, OrganizationID: g.fx.orgID})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	requireWorkloadGrantRefused(t, g.exchangeAtGateway(t))
}

// Each request re-admits the gateway's members against the agent's live
// policy, so a grant revoked after issuance drops its member on the next
// request while the session itself stays valid.
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

// The session a shared authorization server mints for all of an issuer's MCP
// servers enters a stored gateway on that issuer and is served the members
// its agent reaches, where it used to be refused as out of reach.
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

// The owner's live policy caps the agent at every member: an agent granted a
// member its owner may no longer connect to does not reach it.
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

// An agent key enters a stored gateway on its project's issuer, and the
// gateway lists the members its key ceiling, its agent, and its owner all
// reach.
func TestWorkloadGateway_AgentKeyServesReachableMembers(t *testing.T) {
	t.Parallel()

	g := newWorkloadGatewayFixture(t)
	deniedServerID := mcpServerBySlug(t, g.fixtureContext(t), g.ti, g.fx.target.ProjectID, g.denied)
	// The agent may connect to both members, but the key's own ceiling names
	// only one.
	seedPrincipalMCPConnectGrant(t, g.fixtureContext(t), g.ti, g.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, g.agentID.String()), deniedServerID)

	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   g.grantedServerID.String(),
			authz.SelectorKeyProjectID:    g.fx.target.ProjectID.String(),
		},
		Inferred: false,
	}})
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

	require.Equal(t, []string{g.granted}, g.listServers(t, token))
}

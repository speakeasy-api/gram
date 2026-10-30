package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// gatewaySkillsFixture is an ungated stored gateway in the skills fixture's
// project with one served member, one disabled member, and one server outside
// the gateway.
type gatewaySkillsFixture struct {
	*skillsFixture
	gatewayID   uuid.UUID
	gatewaySlug string
	memberID    uuid.UUID
	disabledID  uuid.UUID
	// outsideID is the skills fixture's own server, which is not a member.
	outsideID uuid.UUID
}

func newGatewaySkillsFixture(t *testing.T, ctx context.Context, ti *testInstance, f *skillsFixture) *gatewaySkillsFixture {
	t.Helper()

	slug := "gateway-skills-" + uuid.NewString()[:8]
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, f.projectID, f.organizationID, slug, uuid.Nil)
	memberID := seedMetaMember(t, ctx, ti.conn, f.projectID, gateway.ID, "member live", "member-live-"+uuid.NewString()[:8], 1, mcpservers.VisibilityPrivate)
	disabledID := seedMetaMember(t, ctx, ti.conn, f.projectID, gateway.ID, "member dark", "member-dark-"+uuid.NewString()[:8], 2, mcpservers.VisibilityDisabled)

	return &gatewaySkillsFixture{
		skillsFixture: f,
		gatewayID:     gateway.ID,
		gatewaySlug:   slug,
		memberID:      memberID,
		disabledID:    disabledID,
		outsideID:     f.serverID,
	}
}

func listedSkillURIs(t *testing.T, result map[string]json.RawMessage) []string {
	t.Helper()

	var skills []wireSkill
	require.NoError(t, json.Unmarshal(result["skills"], &skills))
	uris := make([]string, 0, len(skills))
	for _, skill := range skills {
		uris = append(uris, skill.URI)
	}
	return uris
}

func TestServePublic_GatewaySkills_InitializeAdvertisesExtensionWhenFlagOn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	g := newGatewaySkillsFixture(t, ctx, ti, newSkillsFixture(t, ctx, ti))

	w, err := servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	requireSkillsCapability(t, requireRPCResult(t, w)["capabilities"])

	ti.features.SetFlag(feature.FlagSkillsOverMCP, g.organizationID, false)
	w, err = servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	var capabilities map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(requireRPCResult(t, w)["capabilities"], &capabilities))
	require.Contains(t, capabilities, "resources")
	require.NotContains(t, capabilities, "extensions")
}

func TestServePublic_GatewaySkills_ListServesOwnAndInheritedSkills(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	g := newGatewaySkillsFixture(t, ctx, ti, newSkillsFixture(t, ctx, ti))
	servedSkillAt(t, ctx, ti, g.skillsFixture, "gateway-skill", mcpskills.MetaMCPServerTarget(g.gatewayID))
	servedSkillOn(t, ctx, ti, g.skillsFixture, "member-skill", g.memberID)
	servedSkillOn(t, ctx, ti, g.skillsFixture, "disabled-member-skill", g.disabledID)
	servedSkillOn(t, ctx, ti, g.skillsFixture, "outside-skill", g.outsideID)

	w, err := servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	requireCacheHints(t, decodeRPCResponse(t, w)["result"], "private")
	require.ElementsMatch(t, []string{g.uri("gateway-skill"), g.uri("member-skill")}, listedSkillURIs(t, result),
		"the gateway serves its own links and its served members' links, not a disabled member's or an outside server's")
}

func TestServePublic_GatewaySkills_GetAndReadInheritedSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	g := newGatewaySkillsFixture(t, ctx, ti, newSkillsFixture(t, ctx, ti))
	skillID, content := servedSkillOn(t, ctx, ti, g.skillsFixture, "inherited-skill", g.memberID)
	servedSkillOn(t, ctx, ti, g.skillsFixture, "outside-skill", g.outsideID)
	uri := g.uri("inherited-skill")

	w, err := servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": uri}), "", nil)
	require.NoError(t, err)
	var skill wireSkill
	require.NoError(t, json.Unmarshal(requireRPCResult(t, w)["skill"], &skill))
	requireWireSkill(t, g.skillsFixture, skill, "inherited-skill", content)

	session := "gateway-skills-" + uuid.NewString()[:8]
	w, err = servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), "", map[string]string{"Mcp-Session-Id": session})
	require.NoError(t, err)
	var contents []struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType"`
		Text     string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(requireRPCResult(t, w)["contents"], &contents))
	require.Len(t, contents, 1)
	require.Equal(t, uri, contents[0].URI)
	require.Equal(t, "text/markdown", contents[0].MimeType)
	require.Equal(t, content, contents[0].Text)

	observations, err := testrepo.New(ti.conn).ListMCPSkillObservations(ctx, testrepo.ListMCPSkillObservationsParams{
		ProjectID: g.projectID, Provider: mcpskills.ObservationProvider,
	})
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.Equal(t, skillID, observations[0].SkillID.UUID)
	require.Equal(t, session, observations[0].SessionID.String)

	for _, method := range []string{mcpversions.MethodSkillsGet, mcpversions.MethodResourcesRead} {
		w, err = servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeMetaRPCBody(t, method, map[string]any{"uri": g.uri("outside-skill")}), "", nil)
		require.NoError(t, err)
		requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
	}
}

func TestServePublic_GatewaySkills_APIKeyCallerIsServedNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	g := newGatewaySkillsFixture(t, ctx, ti, newSkillsFixture(t, ctx, ti))
	servedSkillOn(t, ctx, ti, g.skillsFixture, "member-skill", g.memberID)
	apiKey := ti.createTestAPIKey(ctx, t)

	w, err := servePublicHTTP(t, t.Context(), ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), apiKey, nil)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(requireRPCResult(t, w)["skills"]), "an API key is not served its creator's skills")
}

func TestServePublic_GatewaySkills_FlagOffAnswersMethodNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	g := newGatewaySkillsFixture(t, ctx, ti, newSkillsFixture(t, ctx, ti))
	servedSkillOn(t, ctx, ti, g.skillsFixture, "member-skill", g.memberID)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, g.organizationID, false)
	uri := g.uri("member-skill")

	for _, method := range []string{mcpversions.MethodSkillsList, mcpversions.MethodSkillsGet} {
		w, err := servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeMetaRPCBody(t, method, map[string]any{"uri": uri}), "", nil)
		require.NoError(t, err)
		requireRPCErrorCode(t, w, oops.MCPCodeMethodNotFound)
	}

	w, err := servePublicHTTP(t, ctx, ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), "", nil)
	require.NoError(t, err)
	_, hasError := decodeRPCResponse(t, w)["error"]
	require.True(t, hasError, "with the extension off the gateway serves no resource at a skill URI; body=%s", w.Body.String())
}

// A consented connection inherits skills only from the members it approved
// with at least one selected tool: not from a member left unselected, and not
// from a member added after consent.
func TestServePublic_GatewaySkills_ConsentedConnectionInheritsOnlySelectedMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	f := newSkillsFixture(t, ctx, ti)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-skills-consent-" + uuid.NewString()[:8]
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, f.projectID, f.organizationID, slug, issuer.ID)
	chosenID := seedMetaMemberWithUpstream(t, ctx, ti.conn, f.projectID, gateway.ID, "Chosen", "chosen", 1, newCatalogUpstream(t, 1).url)
	otherID := seedMetaMemberWithUpstream(t, ctx, ti.conn, f.projectID, gateway.ID, "Other", "other", 2, newCatalogUpstream(t, 1).url)
	specific := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, []string{"chosen--tool_000"})
	all := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, nil)
	addedID := seedMetaMemberWithUpstream(t, ctx, ti.conn, f.projectID, gateway.ID, "Added", "added", 3, newCatalogUpstream(t, 1).url)

	servedSkillOn(t, ctx, ti, f, "chosen-skill", chosenID)
	servedSkillOn(t, ctx, ti, f, "other-skill", otherID)
	servedSkillOn(t, ctx, ti, f, "added-skill", addedID)

	list := func(token string) []string {
		t.Helper()
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), token, nil)
		require.NoError(t, err)
		return listedSkillURIs(t, requireRPCResult(t, w))
	}
	require.ElementsMatch(t, []string{f.uri("chosen-skill")}, list(specific.token),
		"a member with no selected tool and a member added after consent contribute no skills")
	require.ElementsMatch(t, []string{f.uri("chosen-skill"), f.uri("other-skill")}, list(all.token),
		"an all-tools consent inherits from every approved member, still not from one added after consent")

	for _, name := range []string{"other-skill", "added-skill"} {
		for _, method := range []string{mcpversions.MethodSkillsGet, mcpversions.MethodResourcesRead} {
			w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, method, map[string]any{"uri": f.uri(name)}), specific.token, nil)
			require.NoError(t, err)
			requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
		}
	}
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": f.uri("chosen-skill")}), specific.token, nil)
	require.NoError(t, err)
	require.Contains(t, string(requireRPCResult(t, w)["contents"]), "chosen-skill")
}

// The playground reaches a gateway with a chat-session token minted from the
// signed-in user's dashboard session, which proves the user and so is served
// the gateway's inherited skills. A key-minted chat session proves no user.
func TestServePublic_GatewaySkills_DashboardChatSessionIsServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	g := newGatewaySkillsFixture(t, ctx, ti, newSkillsFixture(t, ctx, ti))
	servedSkillOn(t, ctx, ti, g.skillsFixture, "member-skill", g.memberID)

	sessionID := "dashboard-session-" + uuid.NewString()[:8]
	dashboard := chatSessionToken(t, ti, g.skillsFixture, g.authCtx.UserID, "", &sessionID, nil)
	w, err := servePublicHTTP(t, t.Context(), ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), dashboard, nil)
	require.NoError(t, err)
	require.Equal(t, []string{g.uri("member-skill")}, listedSkillURIs(t, requireRPCResult(t, w)))

	keyMinted := chatSessionToken(t, ti, g.skillsFixture, g.authCtx.UserID, uuid.NewString(), &sessionID, nil)
	w, err = servePublicHTTP(t, t.Context(), ti, g.gatewaySlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), keyMinted, nil)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(requireRPCResult(t, w)["skills"]), "a key-minted chat session is not served its key creator's skills")
}

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

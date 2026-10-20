package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

const (
	testLinkServerID  = "55555555-5555-4555-8555-555555555555"
	testLinkGatewayID = "66666666-6666-4666-8666-666666666666"
	testTwinServerA   = "77777777-7777-4777-8777-777777777777"
	testTwinServerB   = "88888888-8888-4888-8888-888888888888"
)

// stubSkillAndMCPTargets is a target inventory that also lists the MCP
// servers and gateways a skill can be linked to.
type stubSkillAndMCPTargets struct {
	stubSkillTargets
	mcp []SkillMCPTarget
}

func (s stubSkillAndMCPTargets) SkillMCPTargets(_ context.Context, _ string, _ uuid.UUID, _ int) ([]SkillMCPTarget, error) {
	return s.mcp, nil
}

func testMCPTargets() []SkillMCPTarget {
	return []SkillMCPTarget{
		{Kind: SkillMCPTargetServer, ID: testLinkServerID, Name: "Docs Server", Slug: "docs-server"},
		{Kind: SkillMCPTargetServer, ID: testTwinServerA, Name: "Twin", Slug: "twin-a"},
		{Kind: SkillMCPTargetServer, ID: testTwinServerB, Name: "Twin", Slug: "twin-b"},
		{Kind: SkillMCPTargetGateway, ID: testLinkGatewayID, Name: "Team Gateway"},
	}
}

func testLinkSkillsService(t *testing.T, management *recordingSkillsManagement) *SkillsService {
	t.Helper()
	return NewSkillsService(
		management,
		stubSkillAndMCPTargets{stubSkillTargets: stubSkillTargets{targets: testTargets()}, mcp: testMCPTargets()},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)
}

func linkInput(kind, server, gateway, pinned string) LinkSkillToMCPInput {
	return LinkSkillToMCPInput{
		ProjectSlug: testSkillProjectSlug, SkillID: testSkillID, TargetKind: kind,
		MCPServer: server, Gateway: gateway, PinnedVersionID: pinned, IdempotencyKey: "link-1",
	}
}

func unlinkInput(kind, server, gateway string) UnlinkSkillFromMCPInput {
	return UnlinkSkillFromMCPInput{
		ProjectSlug: testSkillProjectSlug, SkillID: testSkillID, TargetKind: kind,
		MCPServer: server, Gateway: gateway, IdempotencyKey: "unlink-1",
	}
}

func TestLinkSkillToMCPLinksExactTargetAndReadsItBack(t *testing.T) {
	t.Parallel()

	management := &recordingSkillsManagement{skill: testSkill()}
	service := testLinkSkillsService(t, management)

	out, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("mcp_server", "docs-server", "", ""))
	require.NoError(t, err)
	require.Len(t, management.linked, 1)
	require.Equal(t, "mcp_server", management.linked[0].TargetKind)
	require.Equal(t, testLinkServerID, *management.linked[0].McpServerID, "the slug resolves to exactly that server's id")
	require.Nil(t, management.linked[0].MetaMcpServerID)
	require.Equal(t, testSkillProjectSlug, out.ProjectSlug)
	require.Equal(t, SkillMCPTargetServer, out.Link.Target.Kind)
	require.Equal(t, testLinkServerID, out.Link.Target.ID)
	require.True(t, out.Link.FollowsLatest)
	require.Contains(t, out.Message, "Docs Server")

	pinned, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("mcp_server", testLinkServerID, "", testSkillVersionID))
	require.NoError(t, err)
	require.Equal(t, out.Link.LinkID, pinned.Link.LinkID, "a repeat settles on the same link")
	require.Equal(t, testSkillVersionID, pinned.Link.PinnedVersionID)
	require.False(t, pinned.Link.FollowsLatest)

	gateway, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("meta_mcp_server", "", "team gateway", ""))
	require.NoError(t, err)
	require.Equal(t, testLinkGatewayID, gateway.Link.Target.ID)

	org, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("organization_gateway", "", "", ""))
	require.NoError(t, err)
	require.Equal(t, SkillMCPTargetOrganizationGateway, org.Link.Target.Kind)
	require.Empty(t, org.Link.Target.ID)
}

func TestLinkSkillToMCPRefusesInexactOrInvalidTargets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		input LinkSkillToMCPInput
		want  error
	}{
		{name: "unknown server", input: linkInput("mcp_server", "nowhere", "", ""), want: ErrSkillMCPTargetNotFound},
		{name: "gateway named as server", input: linkInput("mcp_server", "Team Gateway", "", ""), want: ErrSkillMCPTargetNotFound},
		{name: "ambiguous name", input: linkInput("mcp_server", "twin", "", ""), want: ErrSkillMCPTargetAmbiguous},
		{name: "missing server", input: linkInput("mcp_server", "", "", ""), want: ErrRegistrationInvalid},
		{name: "both targets", input: linkInput("mcp_server", "docs-server", "Team Gateway", ""), want: ErrRegistrationInvalid},
		{name: "organization gateway with a name", input: linkInput("organization_gateway", "docs-server", "", ""), want: ErrRegistrationInvalid},
		{name: "unknown kind", input: linkInput("project_gateway", "", "", ""), want: ErrRegistrationInvalid},
		{name: "invalid pin", input: linkInput("mcp_server", "docs-server", "", "not-a-uuid"), want: ErrRegistrationInvalid},
		{name: "missing idempotency key", input: func() LinkSkillToMCPInput {
			input := linkInput("mcp_server", "docs-server", "", "")
			input.IdempotencyKey = ""
			return input
		}(), want: ErrRegistrationInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			management := &recordingSkillsManagement{skill: testSkill()}
			_, err := testLinkSkillsService(t, management).LinkSkillToMCP(t.Context(), testPrincipal(), test.input)
			require.ErrorIs(t, err, test.want)
			require.Empty(t, management.linked, "nothing is linked when the target is not exact")
		})
	}
}

func TestLinkSkillToMCPChecksSkillReadBeforeResolvingTheTarget(t *testing.T) {
	t.Parallel()

	targets := &countingSkillTargets{targets: testTargets()}
	management := &recordingSkillsManagement{skill: testSkill()}
	service := NewSkillsService(management, targets, stubSkillProjects{}, denyingGrants{}, stubSkillsGate{enabled: true}, OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
	})
	service.mcpTargets = stubSkillAndMCPTargets{mcp: testMCPTargets()}

	_, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("mcp_server", "nowhere", "", ""))
	code, _, ok := skillsRefusalCode(err)
	require.True(t, ok)
	require.Equal(t, "forbidden", code, "a caller without skill read learns nothing about the target")
	require.Empty(t, management.linked)
}

func TestLinkSkillToMCPUnavailableWithoutMCPInventory(t *testing.T) {
	t.Parallel()

	management := &recordingSkillsManagement{skill: testSkill()}
	_, err := testSkillsService(t, management).LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("mcp_server", "docs-server", "", ""))
	require.ErrorIs(t, err, ErrSkillsUnavailable)
	_, err = testSkillsService(t, management).UnlinkSkillFromMCP(t.Context(), testPrincipal(), unlinkInput("mcp_server", "docs-server", ""))
	require.ErrorIs(t, err, ErrSkillsUnavailable)
}

func TestUnlinkSkillFromMCPIsIdempotentAndConfirmsRemoval(t *testing.T) {
	t.Parallel()

	management := &recordingSkillsManagement{skill: testSkill()}
	service := testLinkSkillsService(t, management)
	linked, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("mcp_server", "docs-server", "", ""))
	require.NoError(t, err)

	out, err := service.UnlinkSkillFromMCP(t.Context(), testPrincipal(), unlinkInput("mcp_server", "Docs Server", ""))
	require.NoError(t, err)
	require.True(t, out.Removed)
	require.Equal(t, []string{linked.Link.LinkID}, management.unlinked)
	require.Equal(t, testLinkServerID, out.Target.ID)
	require.Equal(t, testSkillID, out.SkillID)
	require.Empty(t, management.links)

	again, err := service.UnlinkSkillFromMCP(t.Context(), testPrincipal(), unlinkInput("mcp_server", "Docs Server", ""))
	require.NoError(t, err)
	require.False(t, again.Removed, "a surface that no longer serves the skill is a no-op")
	require.Len(t, management.unlinked, 1)

	_, err = service.UnlinkSkillFromMCP(t.Context(), testPrincipal(), unlinkInput("mcp_server", "twin", ""))
	require.ErrorIs(t, err, ErrSkillMCPTargetAmbiguous)
}

func TestListSkillDistributionsReportsServedBy(t *testing.T) {
	t.Parallel()

	management := &recordingSkillsManagement{skill: testSkill()}
	service := testLinkSkillsService(t, management)
	_, err := service.LinkSkillToMCP(t.Context(), testPrincipal(), linkInput("mcp_server", "docs-server", "", testSkillVersionID))
	require.NoError(t, err)

	out, err := service.ListSkillDistributions(t.Context(), testPrincipal(), ListSkillDistributionsInput{ProjectSlug: testSkillProjectSlug, SkillID: testSkillID})
	require.NoError(t, err)
	require.Len(t, out.ServedBy, 1)
	require.Equal(t, SkillMCPTargetServer, out.ServedBy[0].Target.Kind)
	require.Equal(t, testLinkServerID, out.ServedBy[0].Target.ID)
	require.Equal(t, "Docs Server", out.ServedBy[0].Target.Name)
	require.Equal(t, testSkillVersionID, out.ServedBy[0].PinnedVersionID)

	later, err := service.ListSkillDistributions(t.Context(), testPrincipal(), ListSkillDistributionsInput{ProjectSlug: testSkillProjectSlug, SkillID: testSkillID, Cursor: "next"})
	require.NoError(t, err)
	require.NotNil(t, later.ServedBy)
	require.Empty(t, later.ServedBy, "served_by is read on the first page only")

	management.linksErr = oops.E(oops.CodeForbidden, skills.ErrSkillsOverMCPDisabled, "serving skills over MCP is not enabled for this organization")
	off, err := service.ListSkillDistributions(t.Context(), testPrincipal(), ListSkillDistributionsInput{ProjectSlug: testSkillProjectSlug, SkillID: testSkillID})
	require.NoError(t, err, "the rollout being off does not break the distribution listing")
	require.NotNil(t, off.ServedBy)
	require.Empty(t, off.ServedBy)

	encoded, err := json.Marshal(off)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"served_by":[]`)
}

func TestSkillLinkToolsRequireConfirmationAndMapTheRollout(t *testing.T) {
	t.Parallel()

	management := &recordingSkillsManagement{skill: testSkill()}
	server := mcp.NewServer(&mcp.Implementation{Name: "skill-links", Version: "0.0.1"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerSkillsTools(reg, testLinkSkillsService(t, management))
	session := connectTestClient(t, server)

	for _, name := range []string{"link_skill_to_mcp", "unlink_skill_from_mcp"} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{
			"project_slug": testSkillProjectSlug, "skill_id": testSkillID, "target_kind": "mcp_server",
			"mcp_server": "docs-server", "confirmed": false, "idempotency_key": "k",
		}})
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Equal(t, "confirmation_required", skillRefusalCode(t, result), name)
	}
	require.Empty(t, management.linked)
	require.Empty(t, management.unlinked)

	refusal, ok := skillsToolResult(oops.E(oops.CodeForbidden, skills.ErrSkillsOverMCPDisabled, "serving skills over MCP is not enabled for this organization"))
	require.True(t, ok)
	require.Equal(t, unavailableCode, skillRefusalCode(t, refusal))
	for _, err := range []error{ErrSkillMCPTargetNotFound, ErrSkillMCPTargetAmbiguous, ErrSkillLinkNotConfirmed} {
		refusal, ok := skillsToolResult(err)
		require.True(t, ok, err)
		require.NotEqual(t, unavailableCode, skillRefusalCode(t, refusal))
	}
}

func TestBuildSkillMCPLinkSummaryNamesTheTarget(t *testing.T) {
	t.Parallel()

	gatewayID := testLinkGatewayID
	summary := buildSkillMCPLinkSummary(&types.SkillLink{
		ID: "link", SkillID: testSkillID, SkillName: "add-mcp", SkillDisplayName: "Add MCP",
		TargetKind: "meta_mcp_server", McpServerID: nil, MetaMcpServerID: &gatewayID,
		TargetName: new("Team Gateway"), PinnedVersionID: nil, CreatedAt: "2026-10-20T00:00:00Z",
	})
	require.Equal(t, SkillMCPTarget{Kind: SkillMCPTargetGateway, ID: testLinkGatewayID, Name: "Team Gateway"}, summary.Target)
	require.True(t, summary.FollowsLatest)
}

func skillRefusalCode(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var body skillsRefusalResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &body))
	return body.Code
}

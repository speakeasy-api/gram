package skills_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// newLinkTestService returns a test service in the skills-over-mcp rollout,
// with a caller who holds skill write, mcp write on every server and project
// write, which together cover every link target.
func newLinkTestService(t *testing.T) (context.Context, *testInstance, *feature.InMemory) {
	t.Helper()

	ctx, ti := newTestService(t)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagSkillsOverMCP, ti.authContext.ActiveOrganizationID, true)
	ti.service.WithFeatureProvider(flags)
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, "*"),
		authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()),
	)
	return ctx, ti, flags
}

type linkTestServer struct {
	id        uuid.UUID
	toolsetID uuid.UUID
}

// createLinkServer creates an MCP server fronting a new toolset in projectID.
func createLinkServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) linkTestServer {
	t.Helper()

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: ti.authContext.ActiveOrganizationID, ProjectID: projectID, Name: slug + "-toolset", Slug: slug + "-toolset",
		Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: conv.ToPGText(slug + "-toolset"), McpEnabled: true,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		ToolsetID: conv.ToNullUUID(toolset.ID), Visibility: visibility.Private,
	})
	require.NoError(t, err)
	return linkTestServer{id: server.ID, toolsetID: toolset.ID}
}

func createLinkGateway(t *testing.T, ctx context.Context, ti *testInstance, name string) uuid.UUID {
	t.Helper()

	server, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: ti.authContext.ActiveOrganizationID, ProjectID: ti.projectID, Name: name,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Visibility: "private", NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
	return server.ID
}

func linkToServer(skillID string, serverID uuid.UUID, pinned *string) *gen.LinkPayload {
	return &gen.LinkPayload{
		ID: skillID, TargetKind: "mcp_server", McpServerID: new(serverID.String()), MetaMcpServerID: nil,
		PinnedVersionID: pinned, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	}
}

func listLinks(filter gen.ListLinksPayload) *gen.ListLinksPayload {
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	return &filter
}

func auditCount(t *testing.T, ctx context.Context, ti *testInstance, action audit.Action) int64 {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
	require.NoError(t, err)
	return count
}

func TestSkillLinkLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "linked-skill", "Linked over MCP.")
	server := createLinkServer(t, ctx, ti, ti.projectID, "linked-server")
	linksBefore := auditCount(t, ctx, ti, audit.ActionSkillLink)
	updatesBefore := auditCount(t, ctx, ti, audit.ActionSkillUpdateLink)
	unlinksBefore := auditCount(t, ctx, ti, audit.ActionSkillUnlink)

	link, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, nil))
	require.NoError(t, err)
	require.Equal(t, created.Skill.ID, link.SkillID)
	require.Equal(t, "linked-skill", link.SkillName)
	require.Equal(t, created.Skill.DisplayName, link.SkillDisplayName)
	require.Equal(t, "mcp_server", link.TargetKind)
	require.Equal(t, server.id.String(), *link.McpServerID)
	require.Nil(t, link.MetaMcpServerID)
	require.Equal(t, "linked-server", *link.TargetName)
	require.Nil(t, link.PinnedVersionID)
	require.NotEmpty(t, link.CreatedAt)
	require.Equal(t, linksBefore+1, auditCount(t, ctx, ti, audit.ActionSkillLink))

	repeated, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, nil))
	require.NoError(t, err)
	require.Equal(t, link.ID, repeated.ID, "a repeat returns the live link")
	require.Equal(t, linksBefore+1, auditCount(t, ctx, ti, audit.ActionSkillLink), "a no-op repeat records nothing")
	require.Equal(t, updatesBefore, auditCount(t, ctx, ti, audit.ActionSkillUpdateLink))

	pinned, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, &created.Version.ID))
	require.NoError(t, err)
	require.Equal(t, link.ID, pinned.ID)
	require.Equal(t, created.Version.ID, *pinned.PinnedVersionID)
	require.Equal(t, updatesBefore+1, auditCount(t, ctx, ti, audit.ActionSkillUpdateLink))
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUpdateLink)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, before["PinnedVersionID"])
	require.Equal(t, created.Version.ID, after["PinnedVersionID"])

	listed, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	require.Len(t, listed.Links, 1)
	require.Equal(t, link.ID, listed.Links[0].ID)
	require.Equal(t, "linked-server", *listed.Links[0].TargetName)
	require.Nil(t, listed.NextCursor)

	require.NoError(t, ti.service.Unlink(ctx, &gen.UnlinkPayload{LinkID: link.ID}))
	require.Equal(t, unlinksBefore+1, auditCount(t, ctx, ti, audit.ActionSkillUnlink))
	require.NoError(t, ti.service.Unlink(ctx, &gen.UnlinkPayload{LinkID: link.ID}), "a repeat unlink is a no-op")
	require.NoError(t, ti.service.Unlink(ctx, &gen.UnlinkPayload{LinkID: uuid.NewString()}), "an unknown link is a no-op")
	require.Equal(t, unlinksBefore+1, auditCount(t, ctx, ti, audit.ActionSkillUnlink), "a no-op unlink records nothing")

	listed, err = ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	require.NotNil(t, listed.Links)
	require.Empty(t, listed.Links)

	relinked, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, nil))
	require.NoError(t, err)
	require.NotEqual(t, link.ID, relinked.ID, "relinking after an unlink creates a new link")
}

func TestSkillLinkGatewayTargetsAndFilters(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	first := createSkill(t, ctx, ti, "first-linked", "First.")
	second := createSkill(t, ctx, ti, "second-linked", "Second.")
	server := createLinkServer(t, ctx, ti, ti.projectID, "filter-server")
	gatewayID := createLinkGateway(t, ctx, ti, "filter-gateway")

	serverLink, err := ti.service.Link(ctx, linkToServer(first.Skill.ID, server.id, nil))
	require.NoError(t, err)
	gatewayLink, err := ti.service.Link(ctx, &gen.LinkPayload{ID: first.Skill.ID, TargetKind: "meta_mcp_server", MetaMcpServerID: new(gatewayID.String())})
	require.NoError(t, err)
	require.Equal(t, "filter-gateway", *gatewayLink.TargetName)
	require.Equal(t, gatewayID.String(), *gatewayLink.MetaMcpServerID)
	orgLink, err := ti.service.Link(ctx, &gen.LinkPayload{ID: second.Skill.ID, TargetKind: "organization_gateway"})
	require.NoError(t, err)
	require.Nil(t, orgLink.TargetName)
	require.Nil(t, orgLink.McpServerID)
	require.Nil(t, orgLink.MetaMcpServerID)

	ids := func(result *gen.ListSkillLinksResult) []string {
		out := make([]string, 0, len(result.Links))
		for _, link := range result.Links {
			out = append(out, link.ID)
		}
		return out
	}

	all, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	require.Equal(t, []string{serverLink.ID, gatewayLink.ID, orgLink.ID}, ids(all))

	bySkill, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{SkillID: &first.Skill.ID}))
	require.NoError(t, err)
	require.Equal(t, []string{serverLink.ID, gatewayLink.ID}, ids(bySkill))

	byServer, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{McpServerID: new(server.id.String())}))
	require.NoError(t, err)
	require.Equal(t, []string{serverLink.ID}, ids(byServer))

	byGateway, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{MetaMcpServerID: new(gatewayID.String())}))
	require.NoError(t, err)
	require.Equal(t, []string{gatewayLink.ID}, ids(byGateway))

	byKind, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{TargetKind: new("organization_gateway")}))
	require.NoError(t, err)
	require.Equal(t, []string{orgLink.ID}, ids(byKind))
}

func TestSkillLinkListPaginates(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "paginated-link", "Valid.")
	for _, slug := range []string{"page-server-a", "page-server-b", "page-server-c"} {
		server := createLinkServer(t, ctx, ti, ti.projectID, slug)
		_, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, nil))
		require.NoError(t, err)
	}

	first, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{Limit: 2}))
	require.NoError(t, err)
	require.Len(t, first.Links, 2)
	require.NotNil(t, first.NextCursor)

	second, err := ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{Cursor: first.NextCursor, Limit: 2}))
	require.NoError(t, err)
	require.Len(t, second.Links, 1)
	require.Nil(t, second.NextCursor)
	require.Equal(t, "page-server-c", *second.Links[0].TargetName)

	_, err = ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{Cursor: new("not-a-cursor"), Limit: 2}))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillLinkRefusedWhenFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti, flags := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "flagged-link", "Valid.")
	server := createLinkServer(t, ctx, ti, ti.projectID, "flagged-server")
	link, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, nil))
	require.NoError(t, err)

	flags.SetFlag(feature.FlagSkillsOverMCP, ti.authContext.ActiveOrganizationID, false)
	_, err = ti.service.Link(ctx, linkToServer(created.Skill.ID, server.id, nil))
	requireOopsCode(t, err, oops.CodeForbidden)
	requireOopsCode(t, ti.service.Unlink(ctx, &gen.UnlinkPayload{LinkID: link.ID}), oops.CodeForbidden)
	_, err = ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{}))
	requireOopsCode(t, err, oops.CodeForbidden)

	ti.service.WithFeatureProvider(nil)
	_, err = ti.service.ListLinks(ctx, listLinks(gen.ListLinksPayload{}))
	requireOopsCode(t, err, oops.CodeForbidden) // without a flag provider the methods fail closed
}

func TestSkillLinkRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "rbac-link", "Valid.")
	visible := createLinkServer(t, ctx, ti, ti.projectID, "visible-server")
	hidden := createLinkServer(t, ctx, ti, ti.projectID, "hidden-server")
	visibleLink, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, visible.id, nil))
	require.NoError(t, err)
	hiddenLink, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, hidden.id, nil))
	require.NoError(t, err)
	orgLink, err := ti.service.Link(ctx, &gen.LinkPayload{ID: created.Skill.ID, TargetKind: "organization_gateway"})
	require.NoError(t, err)

	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err = ti.service.Link(readOnly, linkToServer(created.Skill.ID, visible.id, nil))
	requireOopsCode(t, err, oops.CodeForbidden)
	requireOopsCode(t, ti.service.Unlink(readOnly, &gen.UnlinkPayload{LinkID: visibleLink.ID}), oops.CodeForbidden)
	listed, err := ti.service.ListLinks(readOnly, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	require.Len(t, listed.Links, 1, "without mcp read only the organization gateway link is visible")
	require.Equal(t, orgLink.ID, listed.Links[0].ID)

	noSkill := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPWrite, "*"), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
	_, err = ti.service.Link(noSkill, linkToServer(created.Skill.ID, visible.id, nil))
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListLinks(noSkill, listLinks(gen.ListLinksPayload{}))
	requireOopsCode(t, err, oops.CodeForbidden)

	serverWriter := authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, visible.toolsetID.String()),
	)
	listed, err = ti.service.ListLinks(serverWriter, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	listedIDs := []string{}
	for _, link := range listed.Links {
		listedIDs = append(listedIDs, link.ID)
	}
	require.Equal(t, []string{visibleLink.ID, orgLink.ID}, listedIDs, "links to servers the caller cannot read are left out")
	_, err = ti.service.Link(serverWriter, linkToServer(created.Skill.ID, visible.id, &created.Version.ID))
	require.NoError(t, err, "mcp write on the server is enough to change its links")
	_, err = ti.service.Link(serverWriter, linkToServer(created.Skill.ID, hidden.id, &created.Version.ID))
	requireOopsCode(t, err, oops.CodeForbidden)
	requireOopsCode(t, ti.service.Unlink(serverWriter, &gen.UnlinkPayload{LinkID: hiddenLink.ID}), oops.CodeForbidden)
	_, err = ti.service.Link(serverWriter, &gen.LinkPayload{ID: created.Skill.ID, TargetKind: "organization_gateway", PinnedVersionID: &created.Version.ID})
	requireOopsCode(t, err, oops.CodeForbidden) // the organization gateway needs project write
	requireOopsCode(t, ti.service.Unlink(serverWriter, &gen.UnlinkPayload{LinkID: orgLink.ID}), oops.CodeForbidden)
	require.NoError(t, ti.service.Unlink(serverWriter, &gen.UnlinkPayload{LinkID: visibleLink.ID}))
}

func TestSkillLinkGatewayVisibleWithProjectMCPRead(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "gateway-read", "Valid.")
	gatewayID := createLinkGateway(t, ctx, ti, "read-gateway")
	gatewayLink, err := ti.service.Link(ctx, &gen.LinkPayload{ID: created.Skill.ID, TargetKind: "meta_mcp_server", MetaMcpServerID: new(gatewayID.String())})
	require.NoError(t, err)

	reader := authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeMCPRead, ti.projectID.String()),
	)
	listed, err := ti.service.ListLinks(reader, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	require.Len(t, listed.Links, 1)
	require.Equal(t, gatewayLink.ID, listed.Links[0].ID, "project mcp read, which the gateway read APIs check, shows gateway links")

	skillOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	listed, err = ti.service.ListLinks(skillOnly, listLinks(gen.ListLinksPayload{}))
	require.NoError(t, err)
	require.Empty(t, listed.Links, "without mcp read the gateway link stays hidden")
}

func TestSkillLinkValidation(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "validated-link", "Valid.")
	other := createSkill(t, ctx, ti, "other-skill", "Other.")
	server := createLinkServer(t, ctx, ti, ti.projectID, "validated-server")
	otherProjectID := createProjectContextID(t, ctx, ti)
	foreign := createLinkServer(t, ctx, ti, otherProjectID, "foreign-server")

	_, err := ti.service.Link(ctx, linkToServer(created.Skill.ID, foreign.id, nil))
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.Link(ctx, linkToServer(created.Skill.ID, uuid.New(), nil))
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.Link(ctx, &gen.LinkPayload{ID: created.Skill.ID, TargetKind: "meta_mcp_server", MetaMcpServerID: new(uuid.NewString())})
	requireOopsCode(t, err, oops.CodeNotFound)

	for _, payload := range []*gen.LinkPayload{
		{ID: created.Skill.ID, TargetKind: "mcp_server"},
		{ID: created.Skill.ID, TargetKind: "mcp_server", McpServerID: new(server.id.String()), MetaMcpServerID: new(uuid.NewString())},
		{ID: created.Skill.ID, TargetKind: "meta_mcp_server"},
		{ID: created.Skill.ID, TargetKind: "organization_gateway", McpServerID: new(server.id.String())},
		{ID: created.Skill.ID, TargetKind: "project_gateway"},
		{ID: created.Skill.ID, TargetKind: "mcp_server", McpServerID: new("not-a-uuid")},
		{ID: "not-a-uuid", TargetKind: "organization_gateway"},
		{ID: created.Skill.ID, TargetKind: "mcp_server", McpServerID: new(server.id.String()), PinnedVersionID: &other.Version.ID},
	} {
		_, err := ti.service.Link(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err = ti.service.Link(ctx, linkToServer(uuid.NewString(), server.id, nil))
	requireOopsCode(t, err, oops.CodeNotFound)
	requireOopsCode(t, ti.service.Unlink(ctx, &gen.UnlinkPayload{LinkID: "not-a-uuid"}), oops.CodeBadRequest)
}

// createProjectContextID creates another project in the test organization.
func createProjectContextID(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	_, projectID := createProjectContext(t, ctx, ti)
	return projectID
}

func TestSkillLinkGatewayWritableWithProjectMCPWrite(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newLinkTestService(t)
	created := createSkill(t, ctx, ti, "gateway-write", "Valid.")
	gatewayID := createLinkGateway(t, ctx, ti, "write-gateway")

	gatewayWriter := authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, ti.projectID.String()),
	)
	gatewayLink, err := ti.service.Link(gatewayWriter, &gen.LinkPayload{ID: created.Skill.ID, TargetKind: "meta_mcp_server", MetaMcpServerID: new(gatewayID.String())})
	require.NoError(t, err, "project mcp write, which the gateway write APIs check, is enough to link a gateway")
	require.NoError(t, ti.service.Unlink(gatewayWriter, &gen.UnlinkPayload{LinkID: gatewayLink.ID}))

	skillOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err = ti.service.Link(skillOnly, &gen.LinkPayload{ID: created.Skill.ID, TargetKind: "meta_mcp_server", MetaMcpServerID: new(gatewayID.String())})
	requireOopsCode(t, err, oops.CodeForbidden)
}

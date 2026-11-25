package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// attachIssuerToolset creates a toolset in projectID hosted by a canonical MCP server attached to issuerID.
func attachIssuerToolset(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) toolsetsrepo.Toolset {
	t.Helper()

	toolset := createPlainToolset(t, ctx, ti, projectID, slug)
	hostToolset(t, ctx, ti, toolset, uuid.NullUUID{UUID: issuerID, Valid: true})
	return toolset
}

// createPlainToolset creates a toolset nothing hosts.
func createPlainToolset(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) toolsetsrepo.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID,
		Name: slug, Slug: slug, Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{},
	})
	require.NoError(t, err)
	return toolset
}

// hostToolset gives a toolset a private canonical MCP server, without endpoints, on issuerID.
func hostToolset(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsetsrepo.Toolset, issuerID uuid.NullUUID) {
	t.Helper()

	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID,
		Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: pgtype.Text{String: toolset.Slug, Valid: true},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: issuerID, RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
}

// attachIssuerMCPServer creates an MCP server in projectID, backed by toolset
// and attached to issuerID.
func attachIssuerMCPServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, toolset toolsetsrepo.Toolset, slug string) uuid.UUID {
	t.Helper()

	serverID := uuid.New()
	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: serverID, ProjectID: projectID,
		Name: pgtype.Text{String: slug, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}, RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	return serverID
}

func TestOrganizationUserSessionIssuerListMcpServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	issuerID := createOrganizationIssuer(t, ctx, ti, "attached-issuer")
	otherIssuerID := createOrganizationIssuer(t, ctx, ti, "other-issuer")
	sibling := seedSiblingProject(t, ctx, ti, "attached-sibling")

	// A toolset backing an MCP server is listed through that server, and a hosted toolset as its canonical server.
	wrapped := createPlainToolset(t, ctx, ti, projectID, "wrapped-toolset")
	serverID := attachIssuerMCPServer(t, ctx, ti, projectID, issuerID, wrapped, "attached-server")
	toolset := attachIssuerToolset(t, ctx, ti, sibling.projectID, issuerID, "sibling-toolset")
	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           projectID,
		Name:                "attached-meta",
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
	})
	require.NoError(t, err)
	attachIssuerToolset(t, ctx, ti, projectID, otherIssuerID, "unrelated-toolset")

	listed, err := ti.service.ListMcpServers(ctx, &orggen.ListMcpServersPayload{UserSessionIssuerID: issuerID.String()})
	require.NoError(t, err)
	require.Nil(t, listed.NextCursor)

	project := listedProjectName(t, listed.Items, serverID.String())
	require.ElementsMatch(t, []*orggen.UserSessionIssuerMcpServer{
		{Kind: "mcp_server", ID: serverID.String(), Name: "attached-server", Slug: new("attached-server"), ProjectID: projectID.String(), ProjectName: project},
		{Kind: "mcp_server", ID: toolset.ID.String(), Name: "sibling-toolset", Slug: new("sibling-toolset"), ProjectID: sibling.projectID.String(), ProjectName: "attached-sibling"},
		{Kind: "meta_mcp_server", ID: meta.ID.String(), Name: "attached-meta", Slug: nil, ProjectID: projectID.String(), ProjectName: project},
	}, listed.Items)
}

func listedProjectName(t *testing.T, items []*orggen.UserSessionIssuerMcpServer, id string) string {
	t.Helper()

	for _, item := range items {
		if item.ID == id {
			return item.ProjectName
		}
	}
	require.Failf(t, "MCP server not listed", "id %s", id)
	return ""
}

func TestOrganizationUserSessionIssuerListMcpServersPages(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "paged-issuer")
	first := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "paged-first")
	second := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "paged-second")

	firstPage, err := ti.service.ListMcpServers(ctx, &orggen.ListMcpServersPayload{UserSessionIssuerID: issuerID.String(), Limit: new(1)})
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 1)
	require.NotNil(t, firstPage.NextCursor)

	secondPage, err := ti.service.ListMcpServers(ctx, &orggen.ListMcpServersPayload{UserSessionIssuerID: issuerID.String(), Limit: new(1), Cursor: firstPage.NextCursor})
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 1)

	// UUIDv7 ids created in the same millisecond order arbitrarily, so only the
	// pages' union is fixed.
	require.ElementsMatch(t, []string{first.ID.String(), second.ID.String()}, []string{firstPage.Items[0].ID, secondPage.Items[0].ID})
}

func TestOrganizationUserSessionIssuerListMcpServersRefusesUnknownIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListMcpServers(ctx, &orggen.ListMcpServersPayload{UserSessionIssuerID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuerListMcpServersRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "rbac-issuer")
	payload := &orggen.ListMcpServersPayload{UserSessionIssuerID: issuerID.String()}

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err := ti.service.ListMcpServers(readCtx, payload)
	require.NoError(t, err)

	projectReadCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.ListMcpServers(projectReadCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestOrganizationUserSessionIssuerListMcpServersHidesDeletedAttachments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "deleted-attachments-issuer")
	sibling := seedSiblingProject(t, ctx, ti, "deleted-attachments-sibling")

	deleted := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "deleted-toolset")
	_, err := toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: deleted.Slug, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	attachIssuerToolset(t, ctx, ti, sibling.projectID, issuerID, "deleted-project-toolset")
	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, sibling.projectID)
	require.NoError(t, err)

	listed, err := ti.service.ListMcpServers(ctx, &orggen.ListMcpServersPayload{UserSessionIssuerID: issuerID.String()})
	require.NoError(t, err)
	require.Empty(t, listed.Items)
}

func TestOrganizationUserSessionIssuerListMcpServersRejectsAPIKeyContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "api-key-issuer")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	apiKeyAuthCtx := *authCtx
	apiKeyAuthCtx.APIKeyID = uuid.NewString()
	apiKeyCtx := contextvalues.SetAuthContext(ctx, &apiKeyAuthCtx)

	_, err := ti.service.ListMcpServers(apiKeyCtx, &orggen.ListMcpServersPayload{UserSessionIssuerID: issuerID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
}

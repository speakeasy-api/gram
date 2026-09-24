package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func seedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.UUID, slug string, mcpEnabled bool) uuid.UUID {
	t.Helper()

	ts, err := toolsetsRepo.New(conn).CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
		McpSlug:        pgtype.Text{String: slug, Valid: true},
		McpEnabled:     mcpEnabled,
	})
	require.NoError(t, err)

	return ts.ID
}

// Every mcp_servers row needs exactly one backend, and a toolset-backed one is
// the only backend that needs no other row seeded alongside it.
func seedMCPServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, toolsetID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	srv, err := mcpserversRepo.New(conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID:         id,
		ProjectID:  projectID,
		Name:       pgtype.Text{String: slug, Valid: true},
		Slug:       pgtype.Text{String: slug, Valid: true},
		ToolsetID:  uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility: "disabled",
	})
	require.NoError(t, err)

	return srv.ID
}

func deleteToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, slug string) {
	t.Helper()

	_, err := toolsetsRepo.New(conn).DeleteToolset(ctx, toolsetsRepo.DeleteToolsetParams{
		Slug:      slug,
		ProjectID: projectID,
	})
	require.NoError(t, err)
}

func deleteMCPServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, id uuid.UUID) {
	t.Helper()

	_, err := mcpserversRepo.New(conn).DeleteMCPServer(ctx, mcpserversRepo.DeleteMCPServerParams{
		ID:        id,
		ProjectID: projectID,
	})
	require.NoError(t, err)
}

func listProjects(t *testing.T, ctx context.Context, svc *Service, orgID string) map[string]int {
	t.Helper()

	res, err := svc.ListOrganizationProjects(ctx, &gen.ListOrganizationProjectsPayload{OrganizationID: orgID})
	require.NoError(t, err)

	counts := make(map[string]int, len(res.Projects))
	for _, p := range res.Projects {
		counts[p.Slug] = p.McpServerCount
	}

	return counts
}

func TestListOrganizationProjects_CountIsZeroWithoutServers(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedProject(t, ctx, conn, "org_one", "empty")

	require.Equal(t, map[string]int{"empty": 0}, listProjects(t, ctx, svc, "org_one"))
}

// A toolset is only an MCP server when mcp_enabled says so.
func TestListOrganizationProjects_CountsOnlyMCPEnabledToolsets(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "mixed")
	seedToolset(t, ctx, conn, "org_one", projectID, "served", true)
	seedToolset(t, ctx, conn, "org_one", projectID, "plain", false)

	require.Equal(t, map[string]int{"mixed": 1}, listProjects(t, ctx, svc, "org_one"))
}

func TestListOrganizationProjects_CountsMCPServerRows(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "new-model")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	backingTwo := seedToolset(t, ctx, conn, "org_one", projectID, "backing-two", false)
	seedMCPServer(t, ctx, conn, projectID, backing, "one")
	seedMCPServer(t, ctx, conn, projectID, backingTwo, "two")

	require.Equal(t, map[string]int{"new-model": 2}, listProjects(t, ctx, svc, "org_one"))
}

// The shape AGE-1880's data copy produces: one server described twice, by an
// mcp_enabled toolset and by the mcp_servers row that points at it. A plain sum
// of the two halves would report 2.
func TestListOrganizationProjects_DoesNotDoubleCountAMigratedServer(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "migrated")
	toolsetID := seedToolset(t, ctx, conn, "org_one", projectID, "served", true)
	seedMCPServer(t, ctx, conn, projectID, toolsetID, "served")

	require.Equal(t, map[string]int{"migrated": 1}, listProjects(t, ctx, svc, "org_one"))
}

// And the dedupe is per toolset, not a blanket "this project has both models".
func TestListOrganizationProjects_DedupesPerToolsetNotPerProject(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "half-migrated")
	migrated := seedToolset(t, ctx, conn, "org_one", projectID, "migrated", true)
	seedToolset(t, ctx, conn, "org_one", projectID, "legacy", true)
	seedMCPServer(t, ctx, conn, projectID, migrated, "migrated")

	require.Equal(t, map[string]int{"half-migrated": 2}, listProjects(t, ctx, svc, "org_one"))
}

// A deleted mcp_servers row stops hiding the toolset it pointed at, so the
// count has to stay 1 rather than fall to 0.
func TestListOrganizationProjects_IgnoresDeletedMCPServerRows(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "deleted-server")
	toolsetID := seedToolset(t, ctx, conn, "org_one", projectID, "served", true)
	serverID := seedMCPServer(t, ctx, conn, projectID, toolsetID, "served")
	deleteMCPServer(t, ctx, conn, projectID, serverID)

	require.Equal(t, map[string]int{"deleted-server": 1}, listProjects(t, ctx, svc, "org_one"))
}

func TestListOrganizationProjects_IgnoresDeletedToolsets(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "deleted-toolset")
	seedToolset(t, ctx, conn, "org_one", projectID, "served", true)
	deleteToolset(t, ctx, conn, projectID, "served")

	require.Equal(t, map[string]int{"deleted-toolset": 0}, listProjects(t, ctx, svc, "org_one"))
}

// A deleted mcp_servers row is not counted on its own half either.
func TestListOrganizationProjects_IgnoresDeletedServersWithoutAnMCPEnabledToolset(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "deleted-only")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, backing, "gone")
	deleteMCPServer(t, ctx, conn, projectID, serverID)

	require.Equal(t, map[string]int{"deleted-only": 0}, listProjects(t, ctx, svc, "org_one"))
}

// Each row carries its own count. Transposing the two would still add up.
func TestListOrganizationProjects_CountsAreNotTransposedAcrossRows(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})

	one := seedProject(t, ctx, conn, "org_one", "alpha")
	seedToolset(t, ctx, conn, "org_one", one, "a1", true)

	three := seedProject(t, ctx, conn, "org_one", "beta")
	seedToolset(t, ctx, conn, "org_one", three, "b1", true)
	seedToolset(t, ctx, conn, "org_one", three, "b2", true)
	seedToolset(t, ctx, conn, "org_one", three, "b3", true)

	require.Equal(t, map[string]int{"alpha": 1, "beta": 3}, listProjects(t, ctx, svc, "org_one"))
}

// The count is per project, so a sibling project's servers must not leak in
// even though they share an organization.
func TestListOrganizationProjects_CountIsScopedToTheProject(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_two", name: "Two", slug: "two"})

	// Both halves of the count are scoped, so both halves are seeded in every
	// project here: a bare mcp_servers row as well as an mcp_enabled toolset.
	mine := seedProject(t, ctx, conn, "org_one", "mine")
	seedMCPServer(t, ctx, conn, mine, seedToolset(t, ctx, conn, "org_one", mine, "m1", false), "m1")

	sibling := seedProject(t, ctx, conn, "org_one", "sibling")
	seedToolset(t, ctx, conn, "org_one", sibling, "s1", true)
	seedMCPServer(t, ctx, conn, sibling, seedToolset(t, ctx, conn, "org_one", sibling, "s2", false), "s2")

	theirs := seedProject(t, ctx, conn, "org_two", "theirs")
	seedMCPServer(t, ctx, conn, theirs, seedToolset(t, ctx, conn, "org_two", theirs, "t1", false), "t1")

	require.Equal(t, map[string]int{"mine": 1, "sibling": 2}, listProjects(t, ctx, svc, "org_one"))
	require.Equal(t, map[string]int{"theirs": 1}, listProjects(t, ctx, svc, "org_two"))
}

// A soft-deleted project drops out of the list entirely, count or no count.
func TestListOrganizationProjects_SkipsDeletedProjects(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	live := seedProject(t, ctx, conn, "org_one", "live")
	seedToolset(t, ctx, conn, "org_one", live, "l1", true)

	gone := seedProject(t, ctx, conn, "org_one", "gone")
	seedToolset(t, ctx, conn, "org_one", gone, "g1", true)
	_, err := projectsRepo.New(conn).DeleteProject(ctx, gone)
	require.NoError(t, err)

	require.Equal(t, map[string]int{"live": 1}, listProjects(t, ctx, svc, "org_one"))
}

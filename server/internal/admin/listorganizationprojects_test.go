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

// seedToolset creates a toolset; hosted gives it a private canonical wrapper without endpoints.
func seedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.UUID, slug string, hosted bool) uuid.UUID {
	t.Helper()

	ts, err := toolsetsRepo.New(conn).CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
	})
	require.NoError(t, err)
	if hosted {
		_, err = mcpserversRepo.New(conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
			ID:         ts.ID,
			ProjectID:  projectID,
			Name:       pgtype.Text{String: slug, Valid: true},
			Slug:       pgtype.Text{String: slug, Valid: true},
			ToolsetID:  uuid.NullUUID{UUID: ts.ID, Valid: true},
			Visibility: "private",
		})
		require.NoError(t, err)
	}

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

// A toolset is only an MCP server through its canonical wrapper.
func TestListOrganizationProjects_CountsOnlyHostedToolsets(t *testing.T) {
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

func TestListOrganizationProjects_IgnoresDeletedMCPServerRows(t *testing.T) {
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

	// Canonical wrappers and other mcp_servers rows are seeded in every project here.
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

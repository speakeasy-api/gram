package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func seedProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, slug string) uuid.UUID {
	t.Helper()

	p, err := projectsRepo.New(conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return p.ID
}

func TestGetProject_ByID(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	id := seedProject(t, ctx, conn, "org_one", "default")

	got, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: id.String()})
	require.NoError(t, err)

	require.Equal(t, id.String(), got.ID)
	require.Equal(t, "default", got.Slug)
	require.Equal(t, "org_one", got.OrganizationID)
	// A project with no children reports zero rather than omitting the counts.
	require.Equal(t, 0, got.ToolsetCount)
	require.Equal(t, 0, got.HTTPToolCount)
}

// The slug every organization uses. Both organizations own a project called
// `default`, which is the shape that made this read never return: the lookup
// used to price every match, and the detail query counts six child tables per
// row.
func TestGetProject_BySlugHeldByEveryOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_two", name: "Two", slug: "two"})
	seedProject(t, ctx, conn, "org_one", "default")
	seedProject(t, ctx, conn, "org_two", "default")

	got, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: "default"})
	require.NoError(t, err)
	require.Equal(t, "default", got.Slug)

	// Whichever of the two it named, naming it by id has to describe the same
	// project in the same words: one query answers both addresses now.
	byID, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: got.ID})
	require.NoError(t, err)
	require.Equal(t, got, byID)

	// And the same call twice is the same answer, which is what the ORDER BY on
	// the resolve is for.
	again, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: "default"})
	require.NoError(t, err)
	require.Equal(t, got.ID, again.ID)
}

// A slug is only ever resolved against one organization's worth of projects
// once the caller reaches this point, so the project it names must belong to
// the organization the detail reports. Reading the organization off the wrong
// row is what a lookup that matched many rows could do.
func TestGetProject_BySlugReportsTheOwningOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedProject(t, ctx, conn, "org_one", "solo")

	got, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: "solo"})
	require.NoError(t, err)
	require.Equal(t, "org_one", got.OrganizationID)
}

func TestGetProject_UnknownSlugIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedProject(t, ctx, conn, "org_one", "default")

	_, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: "no-such-project"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetProject_UnknownIDIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: uuid.New().String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Soft deletion is filtered on the way in, not only on the detail read. A
// resolve that ignored it would hand the detail query an id it then refuses,
// which reads the same from outside but walks a deleted row first.
func TestGetProject_DeletedProjectIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	id := seedProject(t, ctx, conn, "org_one", "default")

	_, err := projectsRepo.New(conn).DeleteProject(ctx, id)
	require.NoError(t, err)

	_, err = svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: "default"})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = svc.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: id.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

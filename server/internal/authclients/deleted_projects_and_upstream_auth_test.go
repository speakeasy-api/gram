package authclients_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// A project client is gone with its project: it is neither listed, read nor
// managed once the project is deleted.
func TestAuthClientsOfDeletedProjectsAreGone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	queries := testrepo.New(ti.conn)
	slug := "p-" + uuid.NewString()[:8]
	deletedProject, err := queries.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID:             uuid.New(),
		Name:           slug,
		Slug:           slug,
		OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)
	kept := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Kept")
	gone := seedStaticClient(t, ctx, ti.conn, ti.organizationID, deletedProject, "Gone")
	n, err := queries.SoftDeleteAttachmentProjectFixture(ctx, deletedProject)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	listed, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{Limit: 50})
	require.NoError(t, err)
	ids := make([]string, 0, len(listed.Items))
	for _, client := range listed.Items {
		ids = append(ids, client.ID)
	}
	require.Contains(t, ids, kept.String())
	require.NotContains(t, ids, gone.String())

	_, err = ti.service.GetAuthClient(ctx, &gen.GetAuthClientPayload{ID: gone.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: gone.String(), Name: new("Renamed")})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// The upstream auth requirement write checks the server's organization itself.
func TestSetMCPServerRequiresUpstreamAuthIsOrganizationScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	otherOrganization, _ := seedOtherOrganization(t, ctx, ti.conn)
	queries := repo.New(ti.conn)
	params := repo.SetMCPServerRequiresUpstreamAuthParams{
		RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true},
		ID:                   server.id,
		ProjectID:            server.projectID,
		OrganizationID:       otherOrganization,
	}

	n, err := queries.SetMCPServerRequiresUpstreamAuth(ctx, params)
	require.NoError(t, err)
	require.Zero(t, n, "another organization cannot write the server")

	params.OrganizationID = ti.organizationID
	n, err = queries.SetMCPServerRequiresUpstreamAuth(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

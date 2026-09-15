package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestEMASchema_ProjectTenantConstraint(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	err := repo.New(ti.conn).EnsureEMABinding(ctx, repo.EnsureEMABindingParams{
		ProjectID: *auth.ProjectID, OrganizationID: "other-organization", UserSessionIssuerID: in.UserSessionIssuerID,
		RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
	})
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
}

func TestEMASchema_ClientIssuerConstraint(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	// A client from another issuer must not be accepted even by a direct repository write.
	otherIssuer := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "other-preparation", ""))
	otherClient := preparationManualClient(t, ctx, ti, *auth.ProjectID, otherIssuer, "other-client")
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	b, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
		UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID,
		ExpectedGeneration: b.Generation, Generation: b.Generation + 1, State: b.State, GrantSource: b.GrantSource,
		RequestedScopes: b.RequestedScopes, RemoteSessionClientID: conv.ToNullUUID(otherClient)})
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
}

func TestEMASchema_ProjectSoftDeleteGuard(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err = projectrepo.New(ti.conn).DeleteProject(ctx, *auth.ProjectID)
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
}

func TestEMASchema_ClientAudienceGuard(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	update := repo.UpdateRemoteSessionClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), Audience: conv.ToPGText("https://changed.example.com/")}
	_, err = q.UpdateRemoteSessionClient(ctx, update)
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
	in.ExpectedGeneration = prepared.Generation
	_, err = ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	_, err = q.UpdateRemoteSessionClient(ctx, update)
	require.NoError(t, err)
	_, err = projectrepo.New(ti.conn).DeleteProject(ctx, *auth.ProjectID)
	require.NoError(t, err)
}

package projects_test

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProjectsService_DeleteProjectActiveEMABindingConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)
	project := createProjectForDeletion(t, ctx, ti, "ema-delete-project")
	auth, _ := contextvalues.GetAuthContext(ctx)
	user, err := userrepo.New(ti.conn).CreateUserSessionIssuer(ctx, userrepo.CreateUserSessionIssuerParams{ProjectID: project.ID, Slug: "ema-human", AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: 3600000000, Valid: true}})
	require.NoError(t, err)
	q := remoterepo.New(ti.conn)
	remote, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(project.ID), Slug: "ema-remote", Issuer: "https://issuer.example.com", ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}})
	require.NoError(t, err)
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams{ProjectID: project.ID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user.ID, RemoteSessionIssuerID: remote.ID, Resource: "https://resource.example.com/"}))
	err = ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{ID: project.ID.String()})
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeConflict, shared.Code)
	_, err = projectsrepo.New(ti.conn).GetProjectByID(ctx, project.ID)
	require.NoError(t, err, "failed deletion must preserve the project")
	binding, err := q.GetEMABinding(ctx, remoterepo.GetEMABindingParams{ProjectID: project.ID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user.ID, RemoteSessionIssuerID: remote.ID, Resource: "https://resource.example.com/"})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: binding.ID, ProjectID: project.ID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: "unlinked", GrantSource: "unknown", RequestedScopes: []string{}})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{ID: project.ID.String()}))
	_, err = projectsrepo.New(ti.conn).GetProjectByID(ctx, project.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "successful deletion must hide the project")
}

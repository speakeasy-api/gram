package slackdirectoryconnections_test

import (
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

func createProject(t *testing.T, f *fixture, organizationID string) string {
	t.Helper()
	slug := "p-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: organizationID})
	require.NoError(t, err)
	return project.ID.String()
}

func TestAuthorizeDefaultsToDefaultProject(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Default", Slug: "default", OrganizationID: f.auth.ActiveOrganizationID})
	require.NoError(t, err)
	projectID := project.ID.String()

	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.Equal(t, &projectID, first.DefaultProjectID)

	// The default project answers in one workspace only.
	second := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE02")
	require.Nil(t, second.DefaultProjectID)

	// Reconnecting a workspace with no project picks up the default once it is free.
	_, err = f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: first.ID, ProjectID: nil})
	require.NoError(t, err)
	reconnected := authorize(t, ctx, f, begin(t, ctx, f, &second.ID), "TEXAMPLE02")
	require.Equal(t, &projectID, reconnected.DefaultProjectID)
}

func TestSetDefaultProjectSetsAndClears(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	connection := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.Nil(t, connection.DefaultProjectID)
	projectID := createProject(t, f, f.auth.ActiveOrganizationID)

	updated, err := f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: connection.ID, ProjectID: &projectID})
	require.NoError(t, err)
	require.Equal(t, &projectID, updated.DefaultProjectID)
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, &projectID, list.Connections[0].DefaultProjectID)

	cleared, err := f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: connection.ID, ProjectID: nil})
	require.NoError(t, err)
	require.Nil(t, cleared.DefaultProjectID)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionDefaultProject)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}

func TestSetDefaultProjectRejectsInvalidTargets(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	connection := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")

	foreign := createProject(t, f, "org_other_synthetic")
	_, err := f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: connection.ID, ProjectID: &foreign})
	requireMappingCode(t, err, oops.CodeBadRequest)

	missing := uuid.NewString()
	_, err = f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: missing, ProjectID: nil})
	requireMappingCode(t, err, oops.CodeNotFound)

	denied := authztest.WithExactGrants(t, ctx)
	_, err = f.service.SetDefaultProject(denied, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: connection.ID, ProjectID: nil})
	require.Error(t, err)

	row, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Nil(t, row.Connections[0].DefaultProjectID)
}

func TestSetDefaultProjectRejectsDuplicateProject(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	second := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE02")
	projectID := createProject(t, f, f.auth.ActiveOrganizationID)

	_, err := f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: first.ID, ProjectID: &projectID})
	require.NoError(t, err)
	_, err = f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: second.ID, ProjectID: &projectID})
	requireMappingCode(t, err, oops.CodeConflict)
}

func TestSetDefaultProjectRequiresConnectedWorkspace(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	connection := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	projectID := createProject(t, f, f.auth.ActiveOrganizationID)
	_, err := f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: connection.ID, ProjectID: &projectID})
	require.NoError(t, err)
	disconnected, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: connection.ID, Generation: connection.Generation})
	require.NoError(t, err)
	require.Nil(t, disconnected.DefaultProjectID, "disconnecting frees the project for another workspace")

	_, err = f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: disconnected.ID, ProjectID: conv.PtrEmpty(projectID)})
	requireMappingCode(t, err, oops.CodeBadRequest)
	cleared, err := f.service.SetDefaultProject(ctx, &gen.SetDefaultProjectPayload{SessionToken: nil, ID: disconnected.ID, ProjectID: nil})
	require.NoError(t, err)
	require.Nil(t, cleared.DefaultProjectID)
}

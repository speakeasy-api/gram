package audit_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestChannelsAuditUsesProjectIdentity(t *testing.T) {
	t.Parallel()
	ctx := contextvalues.SetActingSurface(t.Context(), string(audit.SurfaceChannels))
	db, err := infra.CloneTestDatabase(t, "channels_audit")
	require.NoError(t, err)
	orgID := uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test organization", Slug: "test-" + orgID,
		WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{},
	})
	require.NoError(t, err)
	logger := audit.NewLogger()
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "user_admin")
	for _, slug := range []string{"first", "second"} {
		project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: orgID})
		require.NoError(t, err)
		tx := testenv.BeginTx(t, ctx, db)
		require.NoError(t, logger.LogChannelsSettingsUpdate(ctx, tx, audit.LogChannelsSettingsUpdateEvent{
			OrganizationID: orgID, ProjectID: project.ID, Actor: actor, ActorDisplayName: nil, ActorSlug: nil,
			SnapshotBefore: &audit.ChannelsSettingsSnapshot{Enabled: false}, SnapshotAfter: &audit.ChannelsSettingsSnapshot{Enabled: true},
		}))
		settings, err := audittest.LatestAuditLogByActionAndSubject(ctx, tx, audit.ActionChannelsSettingsUpdate, project.ID.String())
		require.NoError(t, err)
		require.Equal(t, orgID, settings.OrganizationID)
		require.Equal(t, uuid.NullUUID{UUID: project.ID, Valid: true}, settings.ProjectID)
		require.Equal(t, project.ID.String(), settings.SubjectID)
		require.Equal(t, "channels_settings", settings.SubjectType)
		require.Equal(t, actor.ID, settings.ActorID)
		require.Equal(t, string(audit.SurfaceChannels), *settings.ActingSurface)
		require.JSONEq(t, `{"enabled":false}`, string(settings.BeforeSnapshot))
		require.JSONEq(t, `{"enabled":true}`, string(settings.AfterSnapshot))
		require.NoError(t, logger.LogChannelsAccessUpdate(ctx, tx, audit.LogChannelsAccessUpdateEvent{
			OrganizationID: orgID, ProjectID: project.ID, Actor: actor, ActorDisplayName: nil, ActorSlug: nil,
			MemberID: "user_member", Removed: false,
			SnapshotBefore: nil, SnapshotAfter: &audit.ChannelsAccessSnapshot{CanConnect: true, CanReadAll: false},
		}))
		access, err := audittest.LatestAuditLogByActionAndSubject(ctx, tx, audit.ActionChannelsAccessUpdate, "user_member")
		require.NoError(t, err)
		require.Equal(t, uuid.NullUUID{UUID: project.ID, Valid: true}, access.ProjectID)
		require.Equal(t, "user_member", access.SubjectID)
		require.Equal(t, "channels_access", access.SubjectType)
		require.JSONEq(t, `{"can_connect":true,"can_read_all":false}`, string(access.AfterSnapshot))
		require.JSONEq(t, `{"exception_removed":false}`, string(access.Metadata))
		require.NoError(t, tx.Rollback(ctx))
	}
	count, err := audittest.AuditLogCount(ctx, db)
	require.NoError(t, err)
	require.Zero(t, count, "audit writes must roll back with the Channels transaction")
}

func TestChannelsAuditRequiresProject(t *testing.T) {
	t.Parallel()
	logger := audit.NewLogger()
	err := logger.LogChannelsSettingsUpdate(t.Context(), nil, audit.LogChannelsSettingsUpdateEvent{})
	require.ErrorContains(t, err, "requires a project ID")
	err = logger.LogChannelsAccessUpdate(t.Context(), nil, audit.LogChannelsAccessUpdateEvent{})
	require.ErrorContains(t, err, "requires a project ID")
}

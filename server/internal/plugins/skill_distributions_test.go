package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestPluginsService_DeletePluginRevokesSkillDistributions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	doomed, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Doomed Plugin"})
	require.NoError(t, err)
	kept, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Kept Plugin"})
	require.NoError(t, err)

	skills := skillsrepo.New(ti.conn)
	skill, err := skills.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID:   projectID,
		Name:        "revoked-on-plugin-delete",
		DisplayName: "revoked-on-plugin-delete",
		Summary:     pgtype.Text{},
	})
	require.NoError(t, err)
	version, err := skills.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "---\nname: revoked-on-plugin-delete\ndescription: d\n---\n\nbody\n",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  authCtx.UserID,
		ProjectID:        projectID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)
	for _, plugin := range []string{doomed.ID, kept.ID} {
		_, err = skills.CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
			PluginID:        uuid.MustParse(plugin),
			PinnedVersionID: uuid.NullUUID{},
			CreatedByUserID: authCtx.UserID,
			ProjectID:       projectID,
			SkillID:         skill.ID,
		})
		require.NoError(t, err)
	}

	undistributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: doomed.ID}))

	active, err := skills.ListActiveSkillDistributions(ctx, skillsrepo.ListActiveSkillDistributionsParams{ProjectID: projectID, CursorCreatedAt: pgtype.Timestamptz{}, CursorID: uuid.NullUUID{}, PageLimit: 50})
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, uuid.MustParse(kept.ID), active[0].SkillDistribution.PluginID.UUID)

	undistributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeBefore+1, undistributeAfter)
	auditRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	auditSnapshot, err := audittest.DecodeAuditData(auditRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, version.ID.String(), auditSnapshot["ResolvedVersionID"])
}

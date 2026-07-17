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
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
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

func TestListPluginSkillsForProjectResolvesContent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Skill Carrier"})
	require.NoError(t, err)
	pluginID := uuid.MustParse(plugin.ID)

	skills := skillsrepo.New(ti.conn)
	makeSkill := func(name string) skillsrepo.Skill {
		skill, err := skills.CreateSkill(ctx, skillsrepo.CreateSkillParams{
			ProjectID:   projectID,
			Name:        name,
			DisplayName: name,
			Summary:     pgtype.Text{},
		})
		require.NoError(t, err)
		return skill
	}
	makeVersion := func(skillID uuid.UUID, content string, valid bool) skillsrepo.SkillVersion {
		version, err := skills.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
			Content:          content,
			CanonicalSha256:  uuid.NewString(),
			RawSha256:        uuid.NewString(),
			Description:      pgtype.Text{},
			Metadata:         []byte(`{}`),
			SpecValid:        valid,
			ValidationErrors: []byte(`[]`),
			CreatedByUserID:  authCtx.UserID,
			ProjectID:        projectID,
			SkillID:          skillID,
		})
		require.NoError(t, err)
		return version
	}
	distribute := func(skillID uuid.UUID, pinned uuid.NullUUID) {
		_, err := skills.CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
			PluginID:        pluginID,
			PinnedVersionID: pinned,
			CreatedByUserID: authCtx.UserID,
			ProjectID:       projectID,
			SkillID:         skillID,
		})
		require.NoError(t, err)
	}

	// Latest-tracking distribution resolves to the newest valid version.
	tracked := makeSkill("tracked")
	makeVersion(tracked.ID, "tracked-v1", true)
	makeVersion(tracked.ID, "tracked-v2", true)
	distribute(tracked.ID, uuid.NullUUID{})

	// Pinned distribution stays on its pinned version despite newer ones.
	pinned := makeSkill("pinned")
	pinnedVersion := makeVersion(pinned.ID, "pinned-v1", true)
	makeVersion(pinned.ID, "pinned-v2", true)
	distribute(pinned.ID, uuid.NullUUID{UUID: pinnedVersion.ID, Valid: true})

	// A skill with no valid version resolves to nothing and is dropped.
	invalidOnly := makeSkill("invalid-only")
	makeVersion(invalidOnly.ID, "broken", false)
	distribute(invalidOnly.ID, uuid.NullUUID{})

	// Revoked distributions are excluded.
	revoked := makeSkill("revoked")
	makeVersion(revoked.ID, "revoked-v1", true)
	distribute(revoked.ID, uuid.NullUUID{})
	_, err = skills.RevokeActiveSkillDistribution(ctx, skillsrepo.RevokeActiveSkillDistributionParams{
		ProjectID: projectID,
		SkillID:   revoked.ID,
		PluginID:  uuid.NullUUID{UUID: pluginID, Valid: true},
	})
	require.NoError(t, err)

	rows, err := pluginsrepo.New(ti.conn).ListPluginSkillsForProject(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "pinned", rows[0].SkillName)
	require.Equal(t, "pinned-v1", rows[0].SkillContent)
	require.Equal(t, "tracked", rows[1].SkillName)
	require.Equal(t, "tracked-v2", rows[1].SkillContent)
	require.Equal(t, pluginID, rows[0].PluginID)
	require.Equal(t, plugin.Slug, rows[0].PluginSlug)
}

package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestSkillDistributionFullLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "distributed-skill", "First valid version.")
	require.True(t, created.Version.SpecValid)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "lifecycle-plugin")

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.Skill.ID, distribution.SkillID)
	require.Equal(t, plugin.ID.String(), *distribution.PluginID)
	require.Equal(t, "lifecycle-plugin", *distribution.PluginName)
	require.Equal(t, created.Version.ID, distribution.ResolvedVersionID)
	require.Nil(t, distribution.PinnedVersionID)
	require.Equal(t, "plugin", distribution.Channel)
	require.Equal(t, ti.authContext.UserID, distribution.CreatedByUserID)

	listed, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Distributions, 1)
	require.Equal(t, distribution.ID, listed.Distributions[0].ID)
	require.Equal(t, plugin.ID.String(), listed.Distributions[0].PluginID)

	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("distributed-skill", "Second valid version.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, second.Version.SpecValid)
	listed, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, second.Version.ID, listed.Distributions[0].ResolvedVersionID)

	pinned, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: &created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, distribution.ID, pinned.ID)
	require.Equal(t, distribution.CreatedAt, pinned.CreatedAt)
	require.Equal(t, distribution.CreatedByUserID, pinned.CreatedByUserID)
	require.Equal(t, created.Version.ID, *pinned.PinnedVersionID)
	require.Equal(t, created.Version.ID, pinned.ResolvedVersionID)

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	listed, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, listed.Distributions)
	require.Empty(t, listed.Distributions)

	recreated, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEqual(t, distribution.ID, recreated.ID)
	require.Equal(t, second.Version.ID, recreated.ResolvedVersionID)
}

func TestSkillDistributionMultiPluginEdges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "multi-plugin-skill", "First valid version.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("multi-plugin-skill", "Second valid version.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	pluginA := createPlugin(t, ctx, ti, ti.projectID, "plugin-a")
	pluginB := createPlugin(t, ctx, ti, ti.projectID, "plugin-b")

	tracked, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(pluginA.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	pinnedEdge, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(pluginB.ID.String()), PinnedVersionID: &created.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEqual(t, tracked.ID, pinnedEdge.ID)
	require.Equal(t, second.Version.ID, tracked.ResolvedVersionID)
	require.Equal(t, created.Version.ID, pinnedEdge.ResolvedVersionID)

	listed, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Distributions, 2)
	require.Equal(t, "multi-plugin-skill", listed.Distributions[0].SkillName)

	pluginID := pluginA.ID.String()
	byPlugin, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{PluginID: &pluginID, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, byPlugin.Distributions, 1)
	require.Equal(t, tracked.ID, byPlugin.Distributions[0].ID)

	bySkill, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{SkillID: &created.Skill.ID, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, bySkill.Distributions, 2)

	otherSkillID := uuid.New().String()
	byOtherSkill, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{SkillID: &otherSkillID, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, byOtherSkill.Distributions)

	badFilter := "not-a-uuid"
	_, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{SkillID: &badFilter, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Revoking one edge leaves the other plugin's distribution active.
	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(pluginA.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	listed, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Distributions, 1)
	require.Equal(t, pinnedEdge.ID, listed.Distributions[0].ID)
}

func TestSkillDistributionQueriesRejectMalformedTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "malformed-target-guard", "Valid.")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "malformed-target-plugin")
	assistant := createAssistant(t, ctx, ti, ti.projectID, "Malformed target assistant")
	valid, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	skillID := uuid.MustParse(created.Skill.ID)
	versionID := uuid.MustParse(created.Version.ID)

	malformedTargets := []struct {
		pluginID    uuid.NullUUID
		assistantID uuid.NullUUID
	}{
		{pluginID: uuid.NullUUID{}, assistantID: uuid.NullUUID{}},
		{pluginID: uuid.NullUUID{UUID: plugin.ID, Valid: true}, assistantID: uuid.NullUUID{UUID: assistant.ID, Valid: true}},
	}
	for _, target := range malformedTargets {
		_, err = ti.repo.CreateSkillDistribution(ctx, repo.CreateSkillDistributionParams{
			PluginID: target.pluginID, AssistantID: target.assistantID, PinnedVersionID: uuid.NullUUID{},
			Channel: "plugin", CreatedByUserID: ti.authContext.UserID, ProjectID: ti.projectID, SkillID: skillID,
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)

		_, err = ti.repo.GetActiveSkillDistributionRecord(ctx, repo.GetActiveSkillDistributionRecordParams{
			ProjectID: ti.projectID, SkillID: skillID,
			PluginID: target.pluginID, AssistantID: target.assistantID, Channel: "plugin",
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)

		_, err = ti.repo.UpdateSkillDistribution(ctx, repo.UpdateSkillDistributionParams{
			PinnedVersionID: uuid.NullUUID{UUID: versionID, Valid: true},
			ProjectID:       ti.projectID, SkillID: skillID,
			PluginID: target.pluginID, AssistantID: target.assistantID, Channel: "plugin",
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)

		_, err = ti.repo.RevokeActiveSkillDistribution(ctx, repo.RevokeActiveSkillDistributionParams{
			ProjectID: ti.projectID, SkillID: skillID,
			PluginID: target.pluginID, AssistantID: target.assistantID, Channel: "plugin",
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}

	active, err := ti.repo.GetActiveSkillDistributionRecord(ctx, repo.GetActiveSkillDistributionRecordParams{
		ProjectID: ti.projectID, SkillID: skillID,
		PluginID: uuid.NullUUID{UUID: plugin.ID, Valid: true}, AssistantID: uuid.NullUUID{}, Channel: "plugin",
	})
	require.NoError(t, err)
	require.Equal(t, valid.ID, active.SkillDistribution.ID.String())
	require.False(t, active.SkillDistribution.PinnedVersionID.Valid)
}

func TestSkillDistributionVersionValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "version-plugin")
	invalidOnly := createSkill(t, ctx, ti, "Invalid_Only", "Invalid name format.")
	require.False(t, invalidOnly.Version.SpecValid)
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: invalidOnly.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	valid := createSkill(t, ctx, ti, "pin-target", "Valid target.")
	other := createSkill(t, ctx, ti, "other-pin-target", "Other valid target.")
	newerInvalid, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: valid.Skill.ID, Content: skillManifest("Pin_Target", "Newer invalid version.", "newer"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, newerInvalid.Version.SpecValid)

	tracked, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, valid.Version.ID, tracked.ResolvedVersionID)

	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: &newerInvalid.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: &other.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	badUUID := "not-a-uuid"
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: &badUUID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillDistributionPluginValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "plugin-validation", "Valid version.")

	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new("not-a-uuid"), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(uuid.NewString()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	deleted := createPlugin(t, ctx, ti, ti.projectID, "deleted-plugin")
	deletePlugin(t, ctx, ti, deleted)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(deleted.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, otherProjectID := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	otherProjectPlugin := createPlugin(t, ctx, ti, otherProjectID, "other-project-plugin")
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(otherProjectPlugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillDistributionProjectIsolationAndArchiveRevocation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "isolated-distribution", "Project one.")
	pluginA := createPlugin(t, ctx, ti, ti.projectID, "isolated-plugin-a")
	pluginB := createPlugin(t, ctx, ti, ti.projectID, "isolated-plugin-b")
	assistant := createAssistant(t, ctx, ti, ti.projectID, "isolated-assistant")
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(pluginA.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(pluginB.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	assistantCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
	_, err = ti.service.Distribute(assistantCtx, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	otherCtx, _ := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	_, err = ti.service.Distribute(otherCtx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(pluginA.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	otherList, err := ti.service.ListDistributions(otherCtx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, otherList.Distributions)
	err = ti.service.Undistribute(otherCtx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(pluginA.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Archiving a skill revokes every active edge, one undistribute audit per edge.
	undistributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeBefore+3, undistributeAfter)
	archiveUndistribute, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	archiveUndistributeSnapshot, err := audittest.DecodeAuditData(archiveUndistribute.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, created.Version.ID, archiveUndistributeSnapshot["ResolvedVersionID"])
	listed, err := ti.repo.ListActiveSkillDistributions(ctx, repo.ListActiveSkillDistributionsParams{ProjectID: ti.projectID, CursorCreatedAt: pgtype.Timestamptz{}, CursorID: uuid.NullUUID{}, PageLimit: 50})
	require.NoError(t, err)
	require.Empty(t, listed)
	err = ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(pluginA.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	withoutDistribution := createSkill(t, ctx, ti, "archive-without-distribution", "No distribution.")
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: withoutDistribution.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeFinal, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeAfter, undistributeFinal)
}

func TestSkillDistributionAuditSnapshotsAndNoOpDeltas(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "audit-distribution", "Sensitive summary.")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "audit-plugin")
	distributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	updateBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdateDistribution)
	require.NoError(t, err)
	undistributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	distributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, distributeBefore+1, distributeAfter)
	createRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Nil(t, createRecord.BeforeSnapshot)
	createSnapshot, err := audittest.DecodeAuditData(createRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, distribution.ID, createSnapshot["ID"])
	require.Equal(t, created.Skill.ID, createSnapshot["SkillID"])
	require.Equal(t, plugin.ID.String(), createSnapshot["PluginID"])
	require.Equal(t, distribution.ResolvedVersionID, createSnapshot["ResolvedVersionID"])
	require.NotContains(t, createSnapshot, "Summary")

	unchanged, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, distribution.UpdatedAt, unchanged.UpdatedAt)
	distributeNoOp, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, distributeAfter, distributeNoOp)

	pinned, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: &created.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	updateAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdateDistribution)
	require.NoError(t, err)
	require.Equal(t, updateBefore+1, updateAfter)
	updateRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUpdateDistribution)
	require.NoError(t, err)
	updateBeforeSnapshot, err := audittest.DecodeAuditData(updateRecord.BeforeSnapshot)
	require.NoError(t, err)
	updateAfterSnapshot, err := audittest.DecodeAuditData(updateRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, updateBeforeSnapshot["PinnedVersionID"])
	require.Equal(t, created.Version.ID, updateAfterSnapshot["PinnedVersionID"])
	require.Equal(t, distribution.ResolvedVersionID, updateBeforeSnapshot["ResolvedVersionID"])
	require.Equal(t, pinned.ResolvedVersionID, updateAfterSnapshot["ResolvedVersionID"])
	require.Equal(t, distribution.CreatedByUserID, pinned.CreatedByUserID)

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeBefore+1, undistributeAfter)
	undistributeRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	undistributeBeforeSnapshot, err := audittest.DecodeAuditData(undistributeRecord.BeforeSnapshot)
	require.NoError(t, err)
	undistributeAfterSnapshot, err := audittest.DecodeAuditData(undistributeRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, undistributeBeforeSnapshot["RevokedAt"])
	require.NotNil(t, undistributeAfterSnapshot["RevokedAt"])

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeNoOp, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeAfter, undistributeNoOp)
}

func TestSkillDistributionReadScopeAndWriteMutations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "distribution-rbac", "Valid.")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "rbac-plugin")
	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err := ti.service.ListDistributions(readCtx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Distribute(readCtx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(readCtx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}

func TestSkillDistributionListPaginates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "paginated-distribution", "Valid.")
	for _, name := range []string{"page-plugin-a", "page-plugin-b", "page-plugin-c"} {
		plugin := createPlugin(t, ctx, ti, ti.projectID, name)
		_, err := ti.service.Distribute(ctx, &gen.DistributePayload{
			ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
			SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		require.NoError(t, err)
	}

	first, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, first.Distributions, 2)
	require.NotNil(t, first.NextCursor)

	second, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: first.NextCursor, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, second.Distributions, 1)
	require.Nil(t, second.NextCursor)
	require.Equal(t, "page-plugin-c", second.Distributions[0].PluginName)
	require.NotEqual(t, first.Distributions[0].ID, second.Distributions[0].ID)
	require.NotEqual(t, first.Distributions[1].ID, second.Distributions[0].ID)

	invalid := "not-a-cursor"
	_, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: &invalid, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestAssistantSkillDistributionLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()),
	)
	created := createSkill(t, authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String())), ti, "assistant-skill", "First version.")
	assistant := createAssistant(t, ctx, ti, ti.projectID, "Assistant target")
	distributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "assistant", distribution.Channel)
	require.Equal(t, assistant.ID.String(), *distribution.AssistantID)
	require.Equal(t, assistant.Name, *distribution.AssistantName)
	require.Nil(t, distribution.PluginID)
	require.Nil(t, distribution.PluginName)
	require.Equal(t, created.Version.ID, distribution.ResolvedVersionID)
	distributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, distributeBefore+1, distributeAfter)
	auditEntry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	auditSnapshot, err := audittest.DecodeAuditData(auditEntry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, assistant.ID.String(), auditSnapshot["AssistantID"])
	require.Nil(t, auditSnapshot["PluginID"])

	unchanged, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, distribution.ID, unchanged.ID)
	require.Equal(t, distribution.UpdatedAt, unchanged.UpdatedAt)

	details, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.EqualValues(t, 1, details.AssistantCount)

	listed, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Distributions)

	writeCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String()))
	second, err := ti.service.AddVersion(writeCtx, &gen.AddVersionPayload{ID: created.Skill.ID, Content: skillManifest("assistant-skill", "Second version.", "second"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	tracked, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, second.Version.ID, tracked.ResolvedVersionID)

	pinned, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), PinnedVersionID: &created.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, created.Version.ID, pinned.ResolvedVersionID)
	require.Equal(t, created.Version.ID, *pinned.PinnedVersionID)

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	details, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Zero(t, details.AssistantCount)

	recreated, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEqual(t, distribution.ID, recreated.ID)
	deleteAssistant(t, ctx, ti, ti.projectID, assistant.ID)
	details, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Zero(t, details.AssistantCount)
}

func TestAssistantSkillDistributionTargetValidationAndRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "assistant-target-validation", "Valid.")
	assistant := createAssistant(t, ctx, ti, ti.projectID, "Validation target")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "validation-plugin")

	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new("bad"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err = ti.service.Distribute(readOnly, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	projectOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
	_, err = ti.service.Distribute(projectOnly, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	both := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
	_, err = ti.service.Distribute(both, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	deleted := createAssistant(t, ctx, ti, ti.projectID, "Deleted target")
	deleteAssistant(t, ctx, ti, ti.projectID, deleted.ID)
	_, err = ti.service.Distribute(both, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(deleted.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, otherProjectID := createProjectContext(t, ctx, ti, authz.ScopeProjectWrite, authz.ScopeSkillRead)
	foreign := createAssistant(t, ctx, ti, otherProjectID, "Foreign target")
	_, err = ti.service.Distribute(both, &gen.DistributePayload{ID: created.Skill.ID, AssistantID: new(foreign.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Plugin distribution remains skill:write-only.
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}

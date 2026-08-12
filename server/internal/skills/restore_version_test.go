package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	skillservice "github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestRestoreSkillVersionUpdatesCurrentResolversWithoutMovingPins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "restorable", "First summary.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("restorable", "Second summary.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	third, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("restorable", "Third summary.", "third"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	skillID := uuid.MustParse(first.Skill.ID)
	firstID := uuid.MustParse(first.Version.ID)
	thirdID := uuid.MustParse(third.Version.ID)
	immutableBefore, err := ti.repo.GetProjectSkillVersion(ctx, repo.GetProjectSkillVersionParams{ProjectID: ti.projectID, SkillVersionID: firstID})
	require.NoError(t, err)

	trackedPlugin := createPlugin(t, ctx, ti, ti.projectID, "restore-tracked")
	pinnedPlugin := createPlugin(t, ctx, ti, ti.projectID, "restore-pinned")
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: first.Skill.ID, PluginID: new(trackedPlugin.ID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: first.Skill.ID, PluginID: new(pinnedPlugin.ID.String()), PinnedVersionID: &third.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	assistant := createAssistant(t, ctx, ti, ti.projectID, "Restore pinned assistant")
	assistantCtx := authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()),
	)
	_, err = ti.service.Distribute(assistantCtx, &gen.DistributePayload{ID: first.Skill.ID, AssistantID: new(assistant.ID.String()), PinnedVersionID: &third.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	open, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, third.Version.Content, skillManifest("restorable", "Third summary.", "third, expanded")),
		Rationale:    "stale", ScoredSessionCount: 0,
		BaseVersionID: thirdID, ProjectID: ti.projectID, SkillID: skillID,
	})
	require.NoError(t, err)

	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillRestoreVersion)
	require.NoError(t, err)
	restored, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: first.Skill.ID, VersionID: first.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, restored.CreatedSkill)
	require.False(t, restored.CreatedVersion)
	require.Equal(t, first.Version.ID, restored.Version.ID)
	require.Equal(t, first.Version.ID, *restored.Skill.LatestVersionID)
	require.Equal(t, "First summary.", *restored.Skill.Summary)
	require.Equal(t, int64(3), restored.Skill.VersionCount)

	got, err := ti.service.Get(ctx, &gen.GetPayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, first.Version.ID, got.LatestVersion.ID)
	listed, err := ti.service.List(ctx, &gen.ListPayload{Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, first.Version.ID, *listed.Skills[0].LatestVersionID)
	history, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: first.Skill.ID, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []string{third.Version.ID, second.Version.ID, first.Version.ID}, []string{history.Versions[0].ID, history.Versions[1].ID, history.Versions[2].ID})
	shared, err := ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: link.Token})
	require.NoError(t, err)
	require.Equal(t, first.Version.Content, shared.Content)

	distributions, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, distributions.Distributions, 2)
	resolvedByPlugin := map[string]string{}
	for _, distribution := range distributions.Distributions {
		resolvedByPlugin[distribution.PluginID] = distribution.ResolvedVersionID
	}
	require.Equal(t, first.Version.ID, resolvedByPlugin[trackedPlugin.ID.String()])
	require.Equal(t, third.Version.ID, resolvedByPlugin[pinnedPlugin.ID.String()])
	pluginSkills, err := pluginsrepo.New(ti.conn).ListPluginSkillsForProject(ctx, pluginsrepo.ListPluginSkillsForProjectParams{ProjectID: ti.projectID, PluginIds: nil})
	require.NoError(t, err)
	contentByPlugin := map[uuid.UUID]string{}
	for _, row := range pluginSkills {
		contentByPlugin[row.PluginID] = row.SkillContent
	}
	require.Equal(t, first.Version.Content, contentByPlugin[trackedPlugin.ID])
	require.Equal(t, third.Version.Content, contentByPlugin[pinnedPlugin.ID])
	assistantSkills, err := assistantsrepo.New(ti.conn).LoadAssistantSkills(ctx, assistantsrepo.LoadAssistantSkillsParams{AssistantIds: []uuid.UUID{assistant.ID}, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Len(t, assistantSkills, 1)
	require.Equal(t, thirdID, assistantSkills[0].ResolvedVersionID)
	require.Equal(t, thirdID, assistantSkills[0].PinnedVersionID.UUID)

	base, err := ti.repo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, firstID, base.BaseVersionID)
	require.Equal(t, thirdID, base.PredecessorVersionID)
	suggestion, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, open.ID, suggestion.ID)
	require.Equal(t, "superseded", suggestion.Status)

	immutableAfter, err := ti.repo.GetProjectSkillVersion(ctx, repo.GetProjectSkillVersionParams{ProjectID: ti.projectID, SkillVersionID: firstID})
	require.NoError(t, err)
	require.Equal(t, immutableBefore.ID, immutableAfter.ID)
	require.Equal(t, immutableBefore.CanonicalSha256, immutableAfter.CanonicalSha256)
	require.Equal(t, immutableBefore.RawSha256, immutableAfter.RawSha256)
	require.Equal(t, immutableBefore.CreatedAt, immutableAfter.CreatedAt)
	require.Equal(t, immutableBefore.CreatedByUserID, immutableAfter.CreatedByUserID)
	require.True(t, immutableAfter.PromotedAt.Valid)
	promotedAt := immutableAfter.PromotedAt
	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillRestoreVersion)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter)
	auditRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillRestoreVersion)
	require.NoError(t, err)
	require.NotContains(t, string(auditRecord.BeforeSnapshot), "summary")
	require.NotContains(t, string(auditRecord.AfterSnapshot), "summary")

	idempotent, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: first.Skill.ID, VersionID: first.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, first.Version.ID, idempotent.Version.ID)
	immutableIdempotent, err := ti.repo.GetProjectSkillVersion(ctx, repo.GetProjectSkillVersionParams{ProjectID: ti.projectID, SkillVersionID: firstID})
	require.NoError(t, err)
	require.Equal(t, promotedAt, immutableIdempotent.PromotedAt)
	auditIdempotent, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillRestoreVersion)
	require.NoError(t, err)
	require.Equal(t, auditAfter, auditIdempotent)
}

func TestRestoreSkillVersionPreservesLineageAndRemovesCapturedOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	captured, err := skillservice.CaptureSkillContent(ctx, ti.conn, ti.projectID, skillManifest("captured-restore", "Captured.", "captured"))
	require.NoError(t, err)
	manual, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: captured.SkillID.String(), Content: skillManifest("captured-restore", "Manual.", "manual"),
		DerivedFromVersionID: conv.PtrEmpty(captured.SkillVersionID.String()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: captured.SkillID.String(), VersionID: captured.SkillVersionID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.repo.GetSkillVersionOrigin(ctx, repo.GetSkillVersionOriginParams{ProjectID: ti.projectID, SkillID: captured.SkillID, SkillVersionID: captured.SkillVersionID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	manualDetails, err := ti.repo.GetSkillVersionDetails(ctx, repo.GetSkillVersionDetailsParams{ProjectID: ti.projectID, SkillID: captured.SkillID, SkillVersionID: uuid.MustParse(manual.Version.ID)})
	require.NoError(t, err)
	require.Equal(t, captured.SkillVersionID, manualDetails.DerivedFromVersionID.UUID)
	_, err = ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: captured.SkillID.String(), VersionID: manual.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	manualDetailsAfter, err := ti.repo.GetSkillVersionDetails(ctx, repo.GetSkillVersionDetailsParams{ProjectID: ti.projectID, SkillID: captured.SkillID, SkillVersionID: uuid.MustParse(manual.Version.ID)})
	require.NoError(t, err)
	require.Equal(t, manualDetails.DerivedFromVersionID, manualDetailsAfter.DerivedFromVersionID)
}

func TestRestoreSkillVersionPromotesValidVersionAfterNewerInvalidVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "restore-after-invalid", "Valid.")
	invalid, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: "---\nname: restore-after-invalid\n---\n\ninvalid\n",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, invalid.Version.SpecValid)
	require.Equal(t, invalid.Version.ID, *invalid.Skill.LatestVersionID)

	restored, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{
		ID: created.Skill.ID, VersionID: created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.Version.ID, *restored.Skill.LatestVersionID)
	version, err := ti.repo.GetProjectSkillVersion(ctx, repo.GetProjectSkillVersionParams{
		ProjectID: ti.projectID, SkillVersionID: uuid.MustParse(created.Version.ID),
	})
	require.NoError(t, err)
	require.True(t, version.PromotedAt.Valid)
}

func TestRestoreSkillVersionPromotesCurrentCapturedTargetToManual(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	manual := createSkill(t, ctx, ti, "restore-current-captured", "Manual.")
	captured, err := skillservice.CaptureSkillContent(ctx, ti.conn, ti.projectID, capturedManifest("restore-current-captured", "Captured.", "captured"))
	require.NoError(t, err)
	require.NotEqual(t, manual.Version.ID, captured.SkillVersionID.String())
	state, err := ti.repo.GetSkillState(ctx, repo.GetSkillStateParams{ProjectID: ti.projectID, SkillID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, captured.SkillVersionID, state.LatestVersionID)
	runtimeVersionID, err := ti.repo.GetLatestValidSkillVersion(ctx, repo.GetLatestValidSkillVersionParams{ProjectID: ti.projectID, SkillID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(manual.Version.ID), runtimeVersionID)

	plugin := createPlugin(t, ctx, ti, ti.projectID, "restore-current-captured-plugin")
	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: manual.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, manual.Version.ID, distribution.ResolvedVersionID)
	open, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, manual.Version.Content, skillManifest("restore-current-captured", "Manual.", "# restore-current-captured, expanded")),
		Rationale:    "stale", ScoredSessionCount: 0,
		BaseVersionID: uuid.MustParse(manual.Version.ID), ProjectID: ti.projectID, SkillID: captured.SkillID,
	})
	require.NoError(t, err)
	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillRestoreVersion)
	require.NoError(t, err)

	restored, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{
		ID: manual.Skill.ID, VersionID: captured.SkillVersionID.String(),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, captured.SkillVersionID.String(), *restored.Skill.LatestVersionID)
	require.Equal(t, "Captured.", *restored.Skill.Summary)
	_, err = ti.repo.GetSkillVersionOrigin(ctx, repo.GetSkillVersionOriginParams{
		ProjectID: ti.projectID, SkillID: captured.SkillID, SkillVersionID: captured.SkillVersionID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	capturedVersion, err := ti.repo.GetProjectSkillVersion(ctx, repo.GetProjectSkillVersionParams{ProjectID: ti.projectID, SkillVersionID: captured.SkillVersionID})
	require.NoError(t, err)
	require.True(t, capturedVersion.PromotedAt.Valid)
	runtimeVersionID, err = ti.repo.GetLatestValidSkillVersion(ctx, repo.GetLatestValidSkillVersionParams{ProjectID: ti.projectID, SkillID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, captured.SkillVersionID, runtimeVersionID)
	pluginSkills, err := pluginsrepo.New(ti.conn).ListPluginSkillsForProject(ctx, pluginsrepo.ListPluginSkillsForProjectParams{ProjectID: ti.projectID, PluginIds: nil})
	require.NoError(t, err)
	require.Len(t, pluginSkills, 1)
	require.Equal(t, capturedManifest("restore-current-captured", "Captured.", "captured"), pluginSkills[0].SkillContent)
	suggestion, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, open.ID, suggestion.ID)
	require.Equal(t, "superseded", suggestion.Status)
	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillRestoreVersion)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter)
}

func TestRestoreSkillVersionUsesPreviousEffectiveCurrentOverHistoricalLineage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "restore-lineage-predecessor", "First.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("restore-lineage-predecessor", "Second.", "second"),
		DerivedFromVersionID: conv.PtrEmpty(first.Version.ID), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	third, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("restore-lineage-predecessor", "Third.", "third"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{
		ID: first.Skill.ID, VersionID: second.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	base, err := ti.repo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{
		ProjectID: ti.projectID, SkillID: uuid.MustParse(first.Skill.ID),
	})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(second.Version.ID), base.BaseVersionID)
	require.Equal(t, uuid.MustParse(third.Version.ID), base.PredecessorVersionID)
	require.NotEqual(t, uuid.MustParse(first.Version.ID), base.PredecessorVersionID)
}

func TestRestoreSkillVersionRejectsInvalidAndCrossSkillTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "restore-validation", "Valid.")
	other := createSkill(t, ctx, ti, "restore-other", "Other.")
	invalid, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{ID: created.Skill.ID, Content: "---\nname: restore-validation\n---\n\ninvalid\n", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, invalid.Version.SpecValid)

	for _, versionID := range []string{invalid.Version.ID, other.Version.ID, uuid.NewString()} {
		_, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: created.Skill.ID, VersionID: versionID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	_, err = ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: uuid.NewString(), VersionID: created.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{ID: "bad", VersionID: created.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestRestoreSkillVersionRequiresWriteScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "restore-access", "Valid.")
	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err := ti.service.RestoreVersion(readCtx, &gen.RestoreVersionPayload{ID: created.Skill.ID, VersionID: created.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

package skills_test

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func insertSkillObservation(t *testing.T, ti *testInstance, name, source, sourceLevel, rawSHA256 string, seenAt time.Time) {
	t.Helper()
	insertSkillObservationForProject(t, ti, ti.projectID, "test", name, source, sourceLevel, rawSHA256, seenAt)
}

func insertSkillObservationForProject(t *testing.T, ti *testInstance, projectID uuid.UUID, provider, name, source, sourceLevel, rawSHA256 string, seenAt time.Time) {
	t.Helper()
	require.NoError(t, hooksrepo.New(ti.conn).InsertSkillObservation(t.Context(), hooksrepo.InsertSkillObservationParams{
		ProjectID:      projectID,
		IdempotencyKey: conv.ToPGText(uuid.NewString()),
		Provider:       provider,
		UserID:         conv.ToPGTextEmpty(""),
		UserEmail:      conv.ToPGTextEmpty(""),
		Hostname:       conv.ToPGTextEmpty(""),
		SessionID:      conv.ToPGTextEmpty(""),
		SkillName:      name,
		Source:         conv.ToPGTextEmpty(source),
		SourceLevel:    conv.ToPGTextEmpty(sourceLevel),
		SourcePath:     conv.ToPGTextEmpty(""),
		RawSha256:      conv.ToPGTextEmpty(rawSHA256),
		SeenAt:         conv.ToPGTimestamptz(seenAt),
	}))
}

func TestListProjectsWithPendingSkillObservationsPaginatesDistinctProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, secondProjectID := createProjectContext(t, ctx, ti)
	_, thirdProjectID := createProjectContext(t, ctx, ti)
	projectIDs := []uuid.UUID{ti.projectID, secondProjectID, thirdProjectID}
	sort.Slice(projectIDs, func(i, j int) bool { return projectIDs[i].String() < projectIDs[j].String() })
	for _, projectID := range projectIDs {
		insertSkillObservationForProject(t, ti, projectID, "test", "pending-skill", "", "project", "", time.Now().UTC())
		insertSkillObservationForProject(t, ti, projectID, "test", "another-pending-skill", "", "project", "", time.Now().UTC())
	}

	firstPage, err := ti.repo.ListProjectsWithPendingSkillObservations(ctx, repo.ListProjectsWithPendingSkillObservationsParams{
		PageLimit: 2, ProjectCursor: uuid.Nil,
	})
	require.NoError(t, err)
	require.Equal(t, projectIDs[:2], firstPage)
	secondPage, err := ti.repo.ListProjectsWithPendingSkillObservations(ctx, repo.ListProjectsWithPendingSkillObservationsParams{
		PageLimit: 2, ProjectCursor: firstPage[1],
	})
	require.NoError(t, err)
	require.Equal(t, projectIDs[2:], secondPage)
}

func TestReconcileSkillObservations_NormalizesAndAggregatesMetadataOnlySightings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	firstSeen := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	lastSeen := firstSeen.Add(30 * time.Second)
	insertSkillObservation(t, ti, "Build_Skill", "", "project", "", firstSeen)
	insertSkillObservation(t, ti, "build.skill", "", "personal", "", lastSeen)

	result, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 2, result.Processed)
	require.False(t, result.HasMore)

	skill, err := ti.repo.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: ti.projectID, Name: "build-skill"})
	require.NoError(t, err)
	require.Equal(t, "captured", skill.SourceKind)
	require.Equal(t, "custom", skill.Classification)
	require.Equal(t, int64(2), skill.SeenCount)
	require.True(t, skill.FirstSeenAt.Time.Equal(firstSeen))
	require.True(t, skill.LastSeenAt.Time.Equal(lastSeen))
	versions, err := ti.repo.ListSkillVersions(ctx, repo.ListSkillVersionsParams{
		ProjectID: ti.projectID, SkillID: skill.ID, CursorCreatedAt: pgtype.Timestamptz{}, CursorID: uuid.NullUUID{}, PageLimit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, versions)

	result, err = skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Zero(t, result.Processed)
	skill, err = ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: skill.ID})
	require.NoError(t, err)
	require.Equal(t, int64(2), skill.SeenCount)
}

func TestReconcileSkillObservations_RoutesToManualSkill(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	manual := createSkill(t, ctx, ti, "manual-skill", "Manual skill.")
	insertSkillObservation(t, ti, "Manual.Skill", "", "project", "", time.Now().UTC())

	result, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: uuid.MustParse(manual.Skill.ID)})
	require.NoError(t, err)
	require.Equal(t, "manual", skill.SourceKind)
	require.Equal(t, "custom", skill.Classification)
	require.Equal(t, int64(1), skill.SeenCount)
}

func TestReconcileSkillObservations_ManualCreateFillsMetadataOnlyPlaceholder(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	insertSkillObservation(t, ti, "vendor:placeholder-skill", "marketplace", "plugin", "", time.Now().UTC())
	_, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	placeholder, err := ti.repo.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: ti.projectID, Name: "placeholder-skill"})
	require.NoError(t, err)
	require.Equal(t, "captured", placeholder.SourceKind)
	require.Equal(t, "built_in", placeholder.Classification)
	createAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)

	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:      capturedManifest("placeholder-skill", "Recorded.", "body"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, created.CreatedSkill)
	require.True(t, created.CreatedVersion)
	require.Equal(t, placeholder.ID.String(), created.Skill.ID)
	require.Equal(t, "manual", created.Skill.SourceKind)
	require.Equal(t, "custom", created.Skill.Classification)
	createAuditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	require.Equal(t, createAudits+1, createAuditsAfter)
}

func TestReconcileSkillObservations_ClassifiesExternalPluginAsBuiltIn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("external-plugin", "External.", "body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)
	insertSkillObservation(t, ti, "vendor:external-plugin", "marketplace", "plugin", contentSHA256(content), time.Now().UTC())

	_, err = skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, "built_in", skill.Classification)
}

func TestReconcileSkillObservations_StripsPluginPrefixWithoutSourceLevel(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	insertSkillObservation(t, ti, "vendor:external-plugin", "marketplace", "", "", time.Now().UTC())

	result, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)
	_, err = ti.repo.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{
		ProjectID: ti.projectID,
		Name:      "external-plugin",
	})
	require.NoError(t, err)
}

func TestReconcileSkillObservations_ClassifiesBuiltInProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	insertSkillObservationForProject(t, ti, ti.projectID, "claude", "provider-skill", "", "", "", time.Now().UTC())

	_, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	skill, err := ti.repo.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: ti.projectID, Name: "provider-skill"})
	require.NoError(t, err)
	require.Equal(t, "built_in", skill.Classification)
}

func TestReconcileSkillObservations_DistributedPluginRemainsCustom(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("distributed-skill", "Distributed.", "body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "distribution-plugin")
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: captured.SkillID.String(), PluginID: plugin.ID.String(), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	insertSkillObservation(t, ti, "distribution-plugin:distributed-skill", "marketplace", "plugin", contentSHA256(content), time.Now().UTC())

	_, err = skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, "custom", skill.Classification)
}

func TestReconcileSkillObservations_HashlessDistributedPluginRemainsCustom(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("hashless-distributed", "Distributed.", "body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "hashless-plugin")
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: captured.SkillID.String(), PluginID: plugin.ID.String(), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	insertSkillObservation(t, ti, "hashless-plugin:hashless-distributed", "marketplace", "plugin", "", time.Now().UTC())

	_, err = skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, "custom", skill.Classification)
}

func TestReconcileSkillObservations_LaggingDistributedPluginVersionRemainsCustom(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	oldContent := capturedManifest("lagging-distributed", "Old.", "old body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, oldContent)
	require.NoError(t, err)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "lagging-plugin")
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: captured.SkillID.String(), PluginID: plugin.ID.String(), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, capturedManifest("lagging-distributed", "New.", "new body"))
	require.NoError(t, err)
	insertSkillObservation(t, ti, "lagging-plugin:lagging-distributed", "marketplace", "plugin", contentSHA256(oldContent), time.Now().UTC())

	_, err = skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, "custom", skill.Classification)
}

func TestReconcileSkillObservations_InvalidNameIsTerminal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	insertSkillObservation(t, ti, "not/a/skill", "", "project", "", time.Now().UTC())

	result, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)
	rows, err := hooksrepo.New(ti.conn).ListSkillObservations(ctx, ti.projectID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].ReconciledAt.Valid)
	require.False(t, rows[0].SkillID.Valid)
	require.Equal(t, "invalid_name", rows[0].ReconcileErrorCode.String)
}

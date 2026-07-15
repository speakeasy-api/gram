package skills_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestSkillsAuditAndOutboxAreAtomicAndSnapshotsAreContentFree(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	addBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	archiveBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)
	outboxBefore, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.SkillV1.EventType()))
	require.NoError(t, err)

	const firstSummary = "Sensitive first summary."
	const secondSummary = "Sensitive second summary."
	const firstBody = "sensitive-first-body-marker"
	const secondBody = "sensitive-second-body-marker"

	firstContent := skillManifest("audited-skill", firstSummary, firstBody)
	first, err := ti.service.Create(ctx, &gen.CreatePayload{Content: firstContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)
	outboxAfterCreate, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.SkillV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxBefore+1, outboxAfterCreate)

	duplicate, err := ti.service.Create(ctx, &gen.CreatePayload{Content: firstContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, duplicate.CreatedVersion)
	require.Equal(t, first.Version.ID, duplicate.Version.ID)
	requireAuditAndOutboxCounts(t, ctx, ti, createAfter, addBefore, archiveBefore, outboxAfterCreate)

	secondContent := skillManifest("audited-skill", secondSummary, secondBody)
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{ID: first.Skill.ID, Content: secondContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	addAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	require.Equal(t, addBefore+1, addAfter)
	outboxAfterAdd, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.SkillV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxAfterCreate+1, outboxAfterAdd)

	addRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	addBeforeSnapshot, err := audittest.DecodeAuditData(addRecord.BeforeSnapshot)
	require.NoError(t, err)
	addAfterSnapshot, err := audittest.DecodeAuditData(addRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, first.Version.ID, addBeforeSnapshot["LatestVersionID"])
	require.InDelta(t, 1, addBeforeSnapshot["VersionCount"], 0)
	require.Equal(t, first.Skill.UpdatedAt, addBeforeSnapshot["UpdatedAt"])
	require.Nil(t, addBeforeSnapshot["ArchivedAt"])
	require.Equal(t, second.Version.ID, addAfterSnapshot["LatestVersionID"])
	require.InDelta(t, 2, addAfterSnapshot["VersionCount"], 0)
	require.Equal(t, second.Skill.UpdatedAt, addAfterSnapshot["UpdatedAt"])
	require.Nil(t, addAfterSnapshot["ArchivedAt"])

	_, err = ti.service.Create(ctx, &gen.CreatePayload{Content: "---\nname: [\n---\n", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               first.Skill.ID,
		Content:          skillManifest("wrong-name", "Invalid transaction.", "body"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	requireAuditAndOutboxCounts(t, ctx, ti, createAfter, addAfter, archiveBefore, outboxAfterAdd)

	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	archiveAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)
	require.Equal(t, archiveBefore+1, archiveAfter)
	outboxAfterArchive, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.SkillV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxAfterAdd+1, outboxAfterArchive)

	archiveRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)
	archiveBeforeSnapshot, err := audittest.DecodeAuditData(archiveRecord.BeforeSnapshot)
	require.NoError(t, err)
	archiveAfterSnapshot, err := audittest.DecodeAuditData(archiveRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, second.Version.ID, archiveBeforeSnapshot["LatestVersionID"])
	require.InDelta(t, 2, archiveBeforeSnapshot["VersionCount"], 0)
	require.Equal(t, second.Skill.UpdatedAt, archiveBeforeSnapshot["UpdatedAt"])
	require.Nil(t, archiveBeforeSnapshot["ArchivedAt"])
	require.Equal(t, archiveBeforeSnapshot["LatestVersionID"], archiveAfterSnapshot["LatestVersionID"])
	require.Equal(t, archiveBeforeSnapshot["VersionCount"], archiveAfterSnapshot["VersionCount"])
	require.NotEmpty(t, archiveAfterSnapshot["UpdatedAt"])
	require.NotNil(t, archiveAfterSnapshot["ArchivedAt"])
	require.NotContains(t, addBeforeSnapshot, "Summary")
	require.NotContains(t, addAfterSnapshot, "Summary")
	require.NotContains(t, archiveBeforeSnapshot, "Summary")
	require.NotContains(t, archiveAfterSnapshot, "Summary")
	for _, snapshot := range [][]byte{addRecord.BeforeSnapshot, addRecord.AfterSnapshot, archiveRecord.BeforeSnapshot, archiveRecord.AfterSnapshot} {
		for _, sensitiveValue := range []string{firstSummary, secondSummary, firstBody, secondBody} {
			require.NotContains(t, string(snapshot), sensitiveValue)
		}
	}
}

func requireAuditAndOutboxCounts(t *testing.T, ctx context.Context, ti *testInstance, create, addVersion, archive, outbox int64) {
	t.Helper()

	actualCreate, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	actualAdd, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	actualArchive, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)
	actualOutbox, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.SkillV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, create, actualCreate)
	require.Equal(t, addVersion, actualAdd)
	require.Equal(t, archive, actualArchive)
	require.Equal(t, outbox, actualOutbox)
}

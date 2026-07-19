package skills_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillservice "github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestSkillsCreateValidManifestRoundTrip(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	empty, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, empty.Skills)
	require.Empty(t, empty.Skills)

	content := "---\nname: My_Skill\ndescription: First summary.\nmetadata:\n  owner: platform\n---\n\n# Body\n"
	result, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.True(t, result.CreatedSkill)
	require.True(t, result.CreatedVersion)
	require.Equal(t, "my-skill", result.Skill.Name)
	require.Equal(t, "My_Skill", result.Skill.DisplayName)
	require.Equal(t, "First summary.", *result.Skill.Summary)
	require.Equal(t, int64(1), result.Skill.VersionCount)
	require.NotNil(t, result.Skill.LatestVersionID)
	require.Equal(t, result.Version.ID, *result.Skill.LatestVersionID)
	require.Equal(t, content, result.Version.Content)
	rawDigest := sha256.Sum256([]byte(content))
	require.Equal(t, hex.EncodeToString(rawDigest[:]), result.Version.RawSha256)
	require.Equal(t, "2ec7c886f769f3f8a417d2b6a62c55de38a3aa5aaf1655e519203b67daa51721", result.Version.CanonicalSha256)
	require.False(t, result.Version.SpecValid)
	require.Equal(t, map[string]any{"owner": "platform"}, result.Version.Metadata)
	require.Equal(t, map[string]any{
		"name":        "My_Skill",
		"description": "First summary.",
		"metadata":    map[string]any{"owner": "platform"},
	}, result.Version.Frontmatter)
	require.Equal(t, []*types.SkillValidationError{{
		Code:    "invalid_format",
		Field:   "name",
		Message: "name must contain only lowercase letters, digits, and single hyphens, with alphanumeric boundaries, and be at most 64 characters",
	}}, result.Version.ValidationErrors)

	got, err := ti.service.Get(ctx, &gen.GetPayload{ID: result.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, result.Skill, got.Skill)
	require.Equal(t, result.Version, got.LatestVersion)

	listed, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*types.Skill{result.Skill}, listed.Skills)
	require.Nil(t, listed.NextCursor)

	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: result.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, versions.Versions)
	require.Equal(t, []*types.SkillVersion{result.Version}, versions.Versions)
	require.Nil(t, versions.NextCursor)
}

func TestSkillsCreateSpecInvalidAcceptedAndMalformedRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	invalid := `---
name: My_Skill
description: []
compatibility: 12
metadata:
  z: [one]
  a: true
---
body
`
	result, err := ti.service.Create(ctx, &gen.CreatePayload{Content: invalid, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, result.Version.SpecValid)
	require.Equal(t, []*types.SkillValidationError{
		{Code: "invalid_type", Field: "compatibility", Message: "compatibility must be a string"},
		{Code: "invalid_type", Field: "description", Message: "description must be a string"},
		{Code: "invalid_type", Field: "metadata.a", Message: "metadata values must be strings"},
		{Code: "invalid_type", Field: "metadata.z", Message: "metadata values must be strings"},
		{Code: "invalid_format", Field: "name", Message: "name must contain only lowercase letters, digits, and single hyphens, with alphanumeric boundaries, and be at most 64 characters"},
	}, result.Version.ValidationErrors)

	beforeAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	beforeList, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	_, err = ti.service.Create(ctx, &gen.CreatePayload{Content: "---\nname: [\n---\n", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	afterAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit, afterAudit)
	afterList, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, beforeList.Skills, afterList.Skills)
}

func TestSkillsVersioningByNormalizedNameAndExplicitAdd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	first := createSkill(t, ctx, ti, "My_Skill", "First summary.")
	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	addVersionBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	secondContent := skillManifest("my.skill", "Second summary.", "second body")
	second, err := ti.service.Create(ctx, &gen.CreatePayload{Content: secondContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, second.CreatedSkill)
	require.True(t, second.CreatedVersion)
	require.Equal(t, first.Skill.ID, second.Skill.ID)
	require.NotEqual(t, first.Version.ID, second.Version.ID)
	require.Equal(t, "my-skill", second.Skill.Name)
	require.Equal(t, "My_Skill", second.Skill.DisplayName)
	require.Equal(t, "First summary.", *second.Skill.Summary)
	require.Equal(t, int64(2), second.Skill.VersionCount)
	require.NotNil(t, second.Skill.LatestVersionID)
	require.Equal(t, second.Version.ID, *second.Skill.LatestVersionID)
	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillCreate)
	require.NoError(t, err)
	require.Equal(t, createBefore, createAfter)
	addVersionAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	require.Equal(t, addVersionBefore+1, addVersionAfter)

	thirdContent := skillManifest("my_skill", "Third summary.", "third body")
	third, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{ID: first.Skill.ID, Content: thirdContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, third.CreatedSkill)
	require.True(t, third.CreatedVersion)
	require.Equal(t, first.Skill.ID, third.Skill.ID)
	require.Equal(t, int64(3), third.Skill.VersionCount)
	require.NotNil(t, third.Skill.LatestVersionID)
	require.Equal(t, third.Version.ID, *third.Skill.LatestVersionID)
	require.Equal(t, "First summary.", *third.Skill.Summary)

	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               first.Skill.ID,
		Content:          skillManifest("different-skill", "Mismatch.", "body"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: first.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, versions.Versions, 3)
}

func TestSkillsCurateCapturedSkillWithVersionLineage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	originalContent := skillManifest("captured-name", "Captured summary.", "captured body")
	captured, err := skillservice.CaptureSkillContent(ctx, ti.conn, ti.projectID, originalContent)
	require.NoError(t, err)
	updateAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdate)
	require.NoError(t, err)

	updated, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: captured.SkillID.String(), Name: "curated-name", DisplayName: "Curated skill",
		Summary: conv.PtrEmpty("Curated summary."), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, captured.SkillID.String(), updated.ID)
	require.Equal(t, "curated-name", updated.Name)
	require.Equal(t, "Curated skill", updated.DisplayName)
	require.Equal(t, "captured", updated.SourceKind)
	updateAuditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdate)
	require.NoError(t, err)
	require.Equal(t, updateAudits+1, updateAuditsAfter)
	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: updated.ID, Name: updated.Name, DisplayName: strings.Repeat("界", 257),
		Summary: updated.Summary, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: updated.ID, Name: updated.Name, DisplayName: updated.DisplayName,
		Summary: conv.PtrEmpty(strings.Repeat("界", 1025)), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	createSkill(t, ctx, ti, "occupied-name", "Occupied.")
	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: updated.ID, Name: "occupied-name", DisplayName: updated.DisplayName,
		Summary: updated.Summary, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	derivedContent := skillManifest("captured-name", "Edited description.", "curated body")
	derived, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: updated.ID, Content: derivedContent, DerivedFromVersionID: conv.PtrEmpty(captured.SkillVersionID.String()),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, derived.Version.DerivedFromVersionID)
	require.Equal(t, captured.SkillVersionID.String(), *derived.Version.DerivedFromVersionID)
	require.Equal(t, "curated-name", derived.Skill.Name)
	require.Equal(t, "Curated skill", derived.Skill.DisplayName)
	require.Equal(t, "Curated summary.", *derived.Skill.Summary)

	other := createSkill(t, ctx, ti, "other-skill", "Other summary.")
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: updated.ID, Content: derivedContent, DerivedFromVersionID: conv.PtrEmpty(other.Version.ID),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	err = ti.repo.CreateSkillVersionLineage(ctx, repo.CreateSkillVersionLineageParams{
		ProjectID:            ti.projectID,
		SkillID:              uuid.MustParse(updated.ID),
		SkillVersionID:       captured.SkillVersionID,
		DerivedFromVersionID: uuid.MustParse(other.Version.ID),
	})
	require.ErrorContains(t, err, "skill_version_lineages_skill_id_derived_from_version_id_fkey")
	insertSkillObservation(t, ti, "captured-name", "", "project", contentSHA256(originalContent), time.Now().UTC())
	result, err := skillservice.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)
	afterActivation, err := ti.service.Get(ctx, &gen.GetPayload{ID: updated.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, int64(1), afterActivation.Skill.SeenCount)

	plugin := createPlugin(t, ctx, ti, ti.projectID, "curated-plugin")
	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: updated.ID, PluginID: plugin.ID.String(), PinnedVersionID: conv.PtrEmpty(derived.Version.ID),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, derived.Version.ID, distribution.ResolvedVersionID)
}

func TestSkillsCanonicalDuplicatesPreserveOriginalVersionAndParent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	base := "---\nname: canonical-skill\ndescription: Canonical summary.\nmetadata:\n  owner: team\n---\n\n# Body\n"
	first, err := ti.service.Create(ctx, &gen.CreatePayload{Content: base, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	parentBefore := *first.Skill
	versionBefore := *first.Version

	variants := []string{
		base,
		strings.ReplaceAll(base, "\n", " \t\n"),
		strings.ReplaceAll(base, "\n", "\u00a0\u2003\n"),
		"\ufeff" + strings.ReplaceAll(base, "\n", "\r\n"),
		"---\nmetadata: {owner: \"team\"}\ndescription: 'Canonical summary.'\nname: canonical-skill\n---\n\n# Body\n",
	}
	for _, variant := range variants {
		duplicate, err := ti.service.Create(ctx, &gen.CreatePayload{Content: variant, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.False(t, duplicate.CreatedSkill)
		require.False(t, duplicate.CreatedVersion)
		require.Equal(t, first.Skill.ID, duplicate.Skill.ID)
		require.Equal(t, first.Version.ID, duplicate.Version.ID)
		require.Equal(t, parentBefore.UpdatedAt, duplicate.Skill.UpdatedAt)
		require.NotNil(t, parentBefore.LatestVersionID)
		require.NotNil(t, duplicate.Skill.LatestVersionID)
		require.Equal(t, *parentBefore.LatestVersionID, *duplicate.Skill.LatestVersionID)
		require.Equal(t, parentBefore.VersionCount, duplicate.Skill.VersionCount)
		require.Equal(t, versionBefore.Content, duplicate.Version.Content)
		require.Equal(t, versionBefore.RawSha256, duplicate.Version.RawSha256)
		require.Equal(t, versionBefore.CanonicalSha256, duplicate.Version.CanonicalSha256)
		require.Equal(t, versionBefore.CreatedByUserID, duplicate.Version.CreatedByUserID)
	}

	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: first.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, versions.Versions, 1)
}

func TestSkillsHistoricalDuplicateDoesNotMoveLatestBackward(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	firstContent := skillManifest("history", "First.", "old")
	first, err := ti.service.Create(ctx, &gen.CreatePayload{Content: firstContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, first.Version.Metadata)
	require.Empty(t, first.Version.Metadata)
	require.NotNil(t, first.Version.ValidationErrors)
	require.Empty(t, first.Version.ValidationErrors)
	newer := createSkill(t, ctx, ti, "history", "Second.")
	require.NotEqual(t, first.Version.ID, newer.Version.ID)

	duplicate, err := ti.service.Create(ctx, &gen.CreatePayload{Content: firstContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, duplicate.CreatedVersion)
	require.Equal(t, first.Version.ID, duplicate.Version.ID)
	require.NotNil(t, duplicate.Skill.LatestVersionID)
	require.Equal(t, newer.Version.ID, *duplicate.Skill.LatestVersionID)
	require.Equal(t, int64(2), duplicate.Skill.VersionCount)
	require.Equal(t, "First.", *duplicate.Skill.Summary)
}

func TestSkillsArchiveIsIdempotentAndAllowsReplacement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	content := skillManifest("replaceable", "Original.", "same body")
	created, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	beforeArchive, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)

	err = ti.service.Archive(ctx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	afterArchive, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)
	require.Equal(t, beforeArchive+1, afterArchive)

	_, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	listed, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, listed.Skills)
	require.Empty(t, listed.Skills)
	_, err = ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: created.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	otherCtx, _ := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	require.NoError(t, ti.service.Archive(otherCtx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	finalArchive, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillArchive)
	require.NoError(t, err)
	require.Equal(t, afterArchive, finalArchive)

	replacement, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.True(t, replacement.CreatedSkill)
	require.True(t, replacement.CreatedVersion)
	require.NotEqual(t, created.Skill.ID, replacement.Skill.ID)
	require.NotEqual(t, created.Version.ID, replacement.Version.ID)
	require.Equal(t, created.Version.Content, replacement.Version.Content)
	require.Equal(t, created.Version.RawSha256, replacement.Version.RawSha256)
	require.Equal(t, created.Version.CanonicalSha256, replacement.Version.CanonicalSha256)
}

func TestSkillsProjectIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "shared-name", "Project one.")
	otherCtx, otherProjectID := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	second := createSkill(t, otherCtx, ti, "shared-name", "Project two.")
	require.NotEqual(t, first.Skill.ID, second.Skill.ID)
	require.Equal(t, ti.projectID.String(), first.Skill.ProjectID)
	require.Equal(t, otherProjectID.String(), second.Skill.ProjectID)

	_, err := ti.service.Get(otherCtx, &gen.GetPayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.ListVersions(otherCtx, &gen.ListVersionsPayload{ID: first.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.AddVersion(otherCtx, &gen.AddVersionPayload{
		ID:               first.Skill.ID,
		Content:          skillManifest("shared-name", "Cross project.", "body"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.AddVersion(otherCtx, &gen.AddVersionPayload{
		ID: second.Skill.ID, Content: skillManifest("shared-name", "Cross-project parent.", "body"),
		DerivedFromVersionID: conv.PtrEmpty(first.Version.ID), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.NoError(t, ti.service.Archive(otherCtx, &gen.ArchivePayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))

	firstStillExists, err := ti.service.Get(ctx, &gen.GetPayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, first.Skill.ID, firstStillExists.Skill.ID)
	firstList, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []string{first.Skill.ID}, []string{firstList.Skills[0].ID})
	secondList, err := ti.service.List(otherCtx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []string{second.Skill.ID}, []string{secondList.Skills[0].ID})
}

func TestSkillsPaginationAndInvalidCursors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	for _, name := range []string{"charlie", "alpha", "bravo"} {
		createSkill(t, ctx, ti, name, name+" summary")
	}

	var names []string
	var cursor *string
	for {
		page, err := ti.service.List(ctx, &gen.ListPayload{Cursor: cursor, Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.NotNil(t, page.Skills)
		require.Len(t, page.Skills, 1)
		names = append(names, page.Skills[0].Name)
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
	}
	require.Equal(t, []string{"alpha", "bravo", "charlie"}, names)
	require.Len(t, names, len(map[string]struct{}{"alpha": {}, "bravo": {}, "charlie": {}}))

	_, err := ti.service.List(ctx, &gen.ListPayload{Cursor: conv.PtrEmpty("%%%"), Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	versioned := createSkill(t, ctx, ti, "versions", "one")
	for _, description := range []string{"two", "three"} {
		_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
			ID:               versioned.Skill.ID,
			Content:          skillManifest("versions", description, description),
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
	}
	all, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: versioned.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, all.Versions, 3)
	expectedIDs := []string{all.Versions[0].ID, all.Versions[1].ID, all.Versions[2].ID}

	var actualIDs []string
	cursor = nil
	for {
		page, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: versioned.Skill.ID, Cursor: cursor, Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.NotNil(t, page.Versions)
		require.Len(t, page.Versions, 1)
		actualIDs = append(actualIDs, page.Versions[0].ID)
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
	}
	require.Equal(t, expectedIDs, actualIDs)
	require.Len(t, actualIDs, 3)

	_, err = ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: versioned.Skill.ID, Cursor: conv.PtrEmpty("%%%"), Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillsReadAndWriteRBACExpansion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "rbac-skill", "Initial.")
	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))

	_, err := ti.service.List(readCtx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Get(readCtx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.ListVersions(readCtx, &gen.ListVersionsPayload{ID: created.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Create(readCtx, &gen.CreatePayload{Content: skillManifest("denied-create", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.AddVersion(readCtx, &gen.AddVersionPayload{ID: created.Skill.ID, Content: skillManifest("rbac-skill", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Update(readCtx, &gen.UpdatePayload{ID: created.Skill.ID, Name: "rbac-skill", DisplayName: "Denied", Summary: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Archive(readCtx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	writeCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String()))
	writable := createSkill(t, writeCtx, ti, "write-skill", "One.")
	_, err = ti.service.List(writeCtx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Get(writeCtx, &gen.GetPayload{ID: writable.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.ListVersions(writeCtx, &gen.ListVersionsPayload{ID: writable.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.AddVersion(writeCtx, &gen.AddVersionPayload{ID: writable.Skill.ID, Content: skillManifest("write-skill", "Two.", "two"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Update(writeCtx, &gen.UpdatePayload{ID: writable.Skill.ID, Name: "renamed-write-skill", DisplayName: "Renamed write skill", Summary: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NoError(t, ti.service.Archive(writeCtx, &gen.ArchivePayload{ID: writable.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
}

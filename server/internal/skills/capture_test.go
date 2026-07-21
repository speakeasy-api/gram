package skills_test

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func capturedManifest(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body + "\n"
}

func contentSHA256(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func TestCaptureSkillContent_CreatesCapturedSkillVersionAndAlias(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("captured-skill", "Captured summary.", "body")

	result, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)
	require.True(t, result.CreatedSkill)
	require.True(t, result.CreatedVersion)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: result.SkillID})
	require.NoError(t, err)
	require.Equal(t, "captured", skill.SourceKind)
	require.Equal(t, "custom", skill.Classification)
	origin, err := ti.repo.GetSkillVersionOrigin(ctx, repo.GetSkillVersionOriginParams{
		ProjectID: ti.projectID, SkillID: result.SkillID, SkillVersionID: result.SkillVersionID,
	})
	require.NoError(t, err)
	require.Equal(t, "captured", origin.Origin)
	alias, err := ti.repo.GetSkillRawHash(ctx, repo.GetSkillRawHashParams{ProjectID: ti.projectID, RawSha256: contentSHA256(content)})
	require.NoError(t, err)
	require.NotEmpty(t, alias.CanonicalSha256)
}

func TestCaptureSkillContent_RawVariantsCollapseAndSameNameContentVersions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	lf := capturedManifest("variant-skill", "Summary.", "body")
	crlf := "---\r\nname: variant-skill\r\ndescription: Summary.\r\n---\r\n\r\nbody\r\n"

	first, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, lf)
	require.NoError(t, err)
	variant, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, crlf)
	require.NoError(t, err)
	require.Equal(t, first.SkillVersionID, variant.SkillVersionID)
	require.False(t, variant.CreatedVersion)
	_, err = ti.repo.GetSkillRawHash(ctx, repo.GetSkillRawHashParams{ProjectID: ti.projectID, RawSha256: contentSHA256(lf)})
	require.NoError(t, err)
	_, err = ti.repo.GetSkillRawHash(ctx, repo.GetSkillRawHashParams{ProjectID: ti.projectID, RawSha256: contentSHA256(crlf)})
	require.NoError(t, err)

	second, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, capturedManifest("variant-skill", "Summary two.", "new body"))
	require.NoError(t, err)
	require.Equal(t, first.SkillID, second.SkillID)
	require.NotEqual(t, first.SkillVersionID, second.SkillVersionID)
}

func TestCaptureSkillContent_DedupesManualWithoutOriginAndPreservesPresentation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("manual-skill", "Manual summary.", "body")
	manual, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	result, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)
	require.False(t, result.CreatedVersion)
	require.Equal(t, manual.Version.ID, result.SkillVersionID.String())
	_, err = ti.repo.GetSkillVersionOrigin(ctx, repo.GetSkillVersionOriginParams{
		ProjectID: ti.projectID, SkillID: result.SkillID, SkillVersionID: result.SkillVersionID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	different := capturedManifest("manual-skill", "Captured replacement.", "different")
	_, err = skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, different)
	require.NoError(t, err)
	skill, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: result.SkillID})
	require.NoError(t, err)
	require.Equal(t, "manual-skill", skill.DisplayName)
	require.Equal(t, "Manual summary.", skill.Summary.String)
}

func TestCaptureSkillContent_ReusesRenamedSkillByRawHash(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("local-name", "Captured summary.", "body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)

	updated, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: captured.SkillID.String(), Name: "curated-name", DisplayName: "Curated name",
		Summary: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	replayed, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)
	require.Equal(t, captured.SkillID, replayed.SkillID)
	require.Equal(t, captured.SkillVersionID, replayed.SkillVersionID)
	require.False(t, replayed.CreatedSkill)
	require.False(t, replayed.CreatedVersion)
	stored, err := ti.repo.GetSkill(ctx, repo.GetSkillParams{ProjectID: ti.projectID, ID: captured.SkillID})
	require.NoError(t, err)
	require.Equal(t, updated.Name, stored.Name)
	require.Equal(t, updated.DisplayName, stored.DisplayName)
}

func TestCaptureSkillContent_ManualReuseRemovesCapturedOrigin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("promoted-skill", "Captured.", "body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)

	manual, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.False(t, manual.CreatedVersion)
	_, err = ti.repo.GetSkillVersionOrigin(ctx, repo.GetSkillVersionOriginParams{
		ProjectID: ti.projectID, SkillID: captured.SkillID, SkillVersionID: captured.SkillVersionID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestCaptureSkillContent_AddVersionReuseRemovesCapturedOrigin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("promoted-version", "Captured.", "body")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
	require.NoError(t, err)

	manual, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: captured.SkillID.String(), Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, manual.CreatedVersion)
	_, err = ti.repo.GetSkillVersionOrigin(ctx, repo.GetSkillVersionOriginParams{
		ProjectID: ti.projectID, SkillID: captured.SkillID, SkillVersionID: captured.SkillVersionID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestCaptureSkillContent_ConcurrentDuplicateCreatesOneVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	content := capturedManifest("concurrent-capture", "Captured.", "body")

	const uploads = 6
	results := make(chan *skills.CaptureResult, uploads)
	errs := make(chan error, uploads)
	var wg sync.WaitGroup
	for range uploads {
		wg.Go(func() {
			result, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, content)
			results <- result
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var versionID string
	for result := range results {
		require.NotNil(t, result)
		if versionID == "" {
			versionID = result.SkillVersionID.String()
		}
		require.Equal(t, versionID, result.SkillVersionID.String())
	}
}

func TestCapturedVersionDoesNotOutrankManualDistribution(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	manual := createSkill(t, ctx, ti, "distribution-priority", "Manual version.")
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, capturedManifest("distribution-priority", "Captured version.", "newer"))
	require.NoError(t, err)
	require.NotEqual(t, manual.Version.ID, captured.SkillVersionID.String())
	plugin := createPlugin(t, ctx, ti, ti.projectID, "capture-priority-plugin")

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: manual.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, manual.Version.ID, distribution.ResolvedVersionID)
	listed, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Limit: 10, Cursor: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, manual.Version.ID, listed.Distributions[0].ResolvedVersionID)
}

func TestPurelyCapturedSkillRemainsDistributable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	captured, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, capturedManifest("capture-only", "Captured.", "body"))
	require.NoError(t, err)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "capture-only-plugin")

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: captured.SkillID.String(), PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, captured.SkillVersionID.String(), distribution.ResolvedVersionID)
}

func TestCaptureSkillContent_InvalidManifestLeavesNoWrites(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, "not a manifest")
	require.ErrorIs(t, err, skills.ErrInvalidCapture)

	rows, err := ti.repo.ListSkills(ctx, repo.ListSkillsParams{ProjectID: ti.projectID, CursorName: pgtype.Text{}, PageLimit: 10})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestCaptureSkillContent_ConflictingAliasRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	firstContent := capturedManifest("first-alias", "First.", "body")
	first, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, firstContent)
	require.NoError(t, err)
	firstVersion, err := ti.repo.GetSkillVersionByHash(ctx, repo.GetSkillVersionByHashParams{
		ProjectID: ti.projectID, SkillID: first.SkillID,
		CanonicalSha256: func() string {
			alias, aliasErr := ti.repo.GetSkillRawHash(ctx, repo.GetSkillRawHashParams{ProjectID: ti.projectID, RawSha256: contentSHA256(firstContent)})
			require.NoError(t, aliasErr)
			return alias.CanonicalSha256
		}(),
	})
	require.NoError(t, err)

	secondContent := capturedManifest("second-alias", "Second.", "body")
	matches, err := ti.repo.StoreSkillRawHashAlias(ctx, repo.StoreSkillRawHashAliasParams{
		RawSha256: contentSHA256(secondContent), ProjectID: ti.projectID, SkillID: first.SkillID,
		SkillVersionID: firstVersion.ID, CanonicalSha256: firstVersion.CanonicalSha256,
	})
	require.NoError(t, err)
	require.True(t, matches)

	_, err = skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, secondContent)
	require.ErrorIs(t, err, skills.ErrCaptureHashConflict)
	_, err = ti.repo.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: ti.projectID, Name: "second-alias"})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

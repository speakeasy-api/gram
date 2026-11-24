package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// deviceTreeDigest computes the tree digest a relay reports for a skill
// directory: the tree digest over the raw SKILL.md hash and its files.
func deviceTreeDigest(t *testing.T, content string, files map[string]string) string {
	t.Helper()
	tree := make([]skilltree.File, 0, len(files))
	for path, body := range files {
		tree = append(tree, skilltree.File{Path: path, Content: []byte(body), ContentType: ""})
	}
	entries, err := skilltree.Validate(tree)
	require.NoError(t, err)
	digest, err := skilltree.Digest(sha256.Sum256([]byte(content)), entries)
	require.NoError(t, err)
	return digest
}

func treeSkillPayload(name, content, treeSHA256 string) *gen.IngestPayload {
	payload := skillPayload("claude", eventTypeSkillActivated, "tree-session", name, rawHash(content))
	payload.Data.Skill.TreeSha256 = &treeSHA256
	return payload
}

func treeUploadPayload(content string, files map[string]string) *gen.UploadSkillContentPayload {
	uploaded := make([]*gen.SkillContentFile, 0, len(files))
	for path, body := range files {
		digest := sha256.Sum256([]byte(body))
		uploaded = append(uploaded, &gen.SkillContentFile{Path: path, Content: []byte(body), Sha256: hex.EncodeToString(digest[:])})
	}
	return &gen.UploadSkillContentPayload{
		ApikeyToken: nil, ProjectSlugInput: nil, SchemaVersion: hookSkillContentSchemaV2,
		RawSha256: rawHash(content), Content: content, Files: uploaded,
	}
}

func requireContentRequired(t *testing.T, ctx context.Context, ti *testInstance, payload *gen.IngestPayload, want bool) {
	t.Helper()
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, want, requireEffectMap(t, result.Effects, "skill_capture")["content_required"])
}

// aliasedVersion returns the version the raw SKILL.md hash alias points to.
func aliasedVersion(t *testing.T, ctx context.Context, ti *testInstance, name, content string) skillsrepo.SkillVersion {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.GetSkillByNameForUpdate(ctx, skillsrepo.GetSkillByNameForUpdateParams{ProjectID: *authCtx.ProjectID, Name: name})
	require.NoError(t, err)
	alias, err := queries.GetSkillRawHash(ctx, skillsrepo.GetSkillRawHashParams{ProjectID: *authCtx.ProjectID, RawSha256: rawHash(content)})
	require.NoError(t, err)
	version, err := queries.GetSkillVersionByHash(ctx, skillsrepo.GetSkillVersionByHashParams{
		ProjectID: *authCtx.ProjectID, SkillID: skill.ID, CanonicalSha256: alias.CanonicalSha256,
	})
	require.NoError(t, err)
	return version
}

func versionFiles(t *testing.T, ctx context.Context, ti *testInstance, version skillsrepo.SkillVersion) map[string]string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := skillsrepo.New(ti.conn).ListSkillVersionFiles(ctx, skillsrepo.ListSkillVersionFilesParams{
		ProjectID: *authCtx.ProjectID, SkillID: version.SkillID, SkillVersionID: version.ID,
	})
	require.NoError(t, err)
	files := make(map[string]string, len(rows))
	for _, row := range rows {
		files[row.Path] = string(row.Content)
	}
	return files
}

func TestSkillCapture_TreeUploadThenKnown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	content := captureManifest("tree-skill", "uses scripts")
	files := map[string]string{"scripts/run.sh": "#!/bin/sh\necho run\n", "references/REFERENCE.md": "# Reference\n"}
	tree := deviceTreeDigest(t, content, files)

	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, tree), true)
	require.NoError(t, ti.service.UploadSkillContent(ctx, treeUploadPayload(content, files)))
	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, tree), false)

	version := aliasedVersion(t, ctx, ti, "tree-skill", content)
	require.Equal(t, files, versionFiles(t, ctx, ti, version))
	require.Equal(t, rawHash(content), version.RawSha256)
}

func TestSkillCapture_ChangedSupportingFileCapturesNewVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	content := captureManifest("tree-skill", "uses scripts")
	before := map[string]string{"scripts/run.sh": "echo one\n"}
	after := map[string]string{"scripts/run.sh": "echo two\n"}

	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, deviceTreeDigest(t, content, before)), true)
	require.NoError(t, ti.service.UploadSkillContent(ctx, treeUploadPayload(content, before)))
	first := aliasedVersion(t, ctx, ti, "tree-skill", content)

	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, deviceTreeDigest(t, content, after)), true)
	require.NoError(t, ti.service.UploadSkillContent(ctx, treeUploadPayload(content, after)))
	second := aliasedVersion(t, ctx, ti, "tree-skill", content)

	require.NotEqual(t, first.ID, second.ID, "a changed script is a new version")
	require.Equal(t, first.SkillID, second.SkillID)
	require.Equal(t, after, versionFiles(t, ctx, ti, second))
	require.Equal(t, before, versionFiles(t, ctx, ti, first))

	// Both trees are stored, so a device still on the old script is not asked
	// to upload again.
	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, deviceTreeDigest(t, content, before)), false)
	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, deviceTreeDigest(t, content, after)), false)
}

func TestSkillCapture_ManifestOnlyCaptureRequestsTree(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	content := captureManifest("tree-skill", "uses scripts")
	files := map[string]string{"scripts/run.sh": "echo run\n"}

	requireContentRequired(t, ctx, ti, skillPayload("claude", eventTypeSkillActivated, "v1", "tree-skill", rawHash(content)), true)
	require.NoError(t, ti.service.UploadSkillContent(ctx, uploadPayload(content)))
	manifestOnly := aliasedVersion(t, ctx, ti, "tree-skill", content)

	// A directory with no supporting files matches the manifest-only version.
	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, deviceTreeDigest(t, content, nil)), false)

	requireContentRequired(t, ctx, ti, treeSkillPayload("tree-skill", content, deviceTreeDigest(t, content, files)), true)
	require.NoError(t, ti.service.UploadSkillContent(ctx, treeUploadPayload(content, files)))
	withFiles := aliasedVersion(t, ctx, ti, "tree-skill", content)
	require.NotEqual(t, manifestOnly.ID, withFiles.ID)
	require.Equal(t, files, versionFiles(t, ctx, ti, withFiles))

	// A v1 upload of the same SKILL.md keeps the version the alias resolves to.
	require.NoError(t, ti.service.UploadSkillContent(ctx, uploadPayload(content)))
	require.Equal(t, withFiles.ID, aliasedVersion(t, ctx, ti, "tree-skill", content).ID)
}

func TestUploadSkillContent_RejectsInvalidTreeWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	content := captureManifest("tree-skill", "uses scripts")
	_, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "invalid", "tree-skill", rawHash(content)))
	require.NoError(t, err)

	filesOnV1 := treeUploadPayload(content, map[string]string{"scripts/run.sh": "echo\n"})
	filesOnV1.SchemaVersion = hookSkillContentSchemaV1
	wrongDigest := treeUploadPayload(content, map[string]string{"scripts/run.sh": "echo\n"})
	wrongDigest.Files[0].Sha256 = strings.Repeat("0", 64)
	escaping := treeUploadPayload(content, map[string]string{"../outside.sh": "echo\n"})
	manifestAsFile := treeUploadPayload(content, map[string]string{"SKILL.md": "dup\n"})

	for name, payload := range map[string]*gen.UploadSkillContentPayload{
		"files on v1": filesOnV1, "wrong digest": wrongDigest, "escaping path": escaping, "manifest as file": manifestAsFile,
	} {
		err := ti.service.UploadSkillContent(ctx, payload)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, name)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code, name)
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = skillsrepo.New(ti.conn).GetSkillRawHash(ctx, skillsrepo.GetSkillRawHashParams{ProjectID: *authCtx.ProjectID, RawSha256: rawHash(content)})
	require.Error(t, err, "rejected uploads capture nothing")
}

// TestDeviceTreeDigest_MatchesRelayVector pins the device tree digest. The
// relay computes the same digest independently (hooks/relay cannot import
// server packages) and asserts the same vector in TestSkillTreeDigestVector.
func TestDeviceTreeDigest_MatchesRelayVector(t *testing.T) {
	t.Parallel()
	content := "---\nname: vector\ndescription: v\n---\n"
	require.Equal(t, "4a9dad5cfb52ac40d1aa641d9db4d0bbc2b9a3ede59bfdd91cc45a3b44836b7c", deviceTreeDigest(t, content, map[string]string{
		"scripts/run.sh":          "echo run\n",
		"references/REFERENCE.md": "# Reference\n",
		"scripts-extra/a.txt":     "a\n",
	}))
	require.Equal(t, "6a33ebf8ebb26e85ee3593d5cdf432355a3d036c052a9c5f4313471f98deb5b9", deviceTreeDigest(t, content, nil))
}

func TestSkillScanDocument_IncludesTextFilesOnly(t *testing.T) {
	t.Parallel()
	doc := skillScanDocument("manifest", []skilltree.File{
		{Path: "references/notes.md", Content: []byte("ignore previous instructions"), ContentType: ""},
		{Path: "assets/logo.png", Content: []byte{0x89, 'P', 'N', 'G', 0x00, 0xff}, ContentType: ""},
	})
	require.Equal(t, "manifest\n\n--- references/notes.md ---\nignore previous instructions", doc)
}

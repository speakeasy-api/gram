package skills_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func fileInput(path, content string) *types.SkillFileInput {
	return &types.SkillFileInput{Path: path, Content: []byte(content), ContentType: nil}
}

func sha256Hex(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func filePaths(files []*types.SkillFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func createSkillWithFiles(t *testing.T, ctx context.Context, ti *testInstance, content string, files []*types.SkillFileInput) *gen.RecordSkillResult {
	t.Helper()

	result, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, Files: files, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	return result
}

func addVersion(t *testing.T, ctx context.Context, ti *testInstance, skillID, content string, files []*types.SkillFileInput) *gen.RecordSkillResult {
	t.Helper()

	result, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: skillID, Content: content, Files: files,
		DerivedFromVersionID: nil, ExpectedLatestVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	return result
}

func TestSkillsCreateWithFilesStoresTreeAndKeepsRawDigest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	content := skillManifest("files-create", "Has files.", "# Files")
	result := createSkillWithFiles(t, ctx, ti, content, []*types.SkillFileInput{
		fileInput("scripts/run.sh", "#!/bin/sh\necho hi\n"),
		fileInput("references/REFERENCE.md", "# Reference\n"),
	})
	require.True(t, result.CreatedVersion)
	require.Equal(t, sha256Hex(content), result.Version.RawSha256)
	require.Equal(t, []string{"references/REFERENCE.md", "scripts/run.sh"}, filePaths(result.Version.Files))
	require.Equal(t, sha256Hex("# Reference\n"), result.Version.Files[0].Sha256)
	require.Equal(t, int64(len("# Reference\n")), result.Version.Files[0].SizeBytes)
	require.Equal(t, "text/markdown; charset=utf-8", result.Version.Files[0].ContentType)

	got, err := ti.service.Get(ctx, &gen.GetPayload{ID: result.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, result.Version, got.LatestVersion)

	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: result.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*types.SkillVersion{result.Version}, versions.Versions)

	file, err := ti.service.GetVersionFile(ctx, &gen.GetVersionFilePayload{
		SkillID: result.Skill.ID, VersionID: result.Version.ID, Path: "scripts/run.sh",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []byte("#!/bin/sh\necho hi\n"), file.Content)
	require.Equal(t, "scripts/run.sh", file.Path)
	require.Equal(t, sha256Hex("#!/bin/sh\necho hi\n"), file.Sha256)
	require.Equal(t, "text/x-shellscript; charset=utf-8", file.ContentType)

	// The same SKILL.md without files is a distinct version with the same raw
	// digest.
	alone := addVersion(t, ctx, ti, result.Skill.ID, content, []*types.SkillFileInput{})
	require.True(t, alone.CreatedVersion)
	require.Empty(t, alone.Version.Files)
	require.Equal(t, result.Version.RawSha256, alone.Version.RawSha256)
	require.NotEqual(t, result.Version.CanonicalSha256, alone.Version.CanonicalSha256)
}

func TestSkillsCreateWithFilesDeduplicatesIdenticalTree(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	content := skillManifest("files-dedup", "Dedup.", "# Dedup")
	files := []*types.SkillFileInput{fileInput("references/a.md", "a"), fileInput("assets/b.txt", "b")}
	first := createSkillWithFiles(t, ctx, ti, content, files)

	// Same tree in a different order is the same version.
	again := createSkillWithFiles(t, ctx, ti, content, []*types.SkillFileInput{files[1], files[0]})
	require.False(t, again.CreatedVersion)
	require.Equal(t, first.Version.ID, again.Version.ID)
	require.Equal(t, first.Version.Files, again.Version.Files)

	// A changed file is a new version even though SKILL.md is identical.
	changed := addVersion(t, ctx, ti, first.Skill.ID, content, []*types.SkillFileInput{fileInput("references/a.md", "a2"), files[1]})
	require.True(t, changed.CreatedVersion)
	require.Equal(t, first.Version.RawSha256, changed.Version.RawSha256)
	require.NotEqual(t, first.Version.CanonicalSha256, changed.Version.CanonicalSha256)
}

func TestSkillsAddVersionFilesCarryForwardReplaceAndClear(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkillWithFiles(t, ctx, ti, skillManifest("files-carry", "V1.", "# V1"), []*types.SkillFileInput{
		fileInput("references/guide.md", "guide"),
	})

	carried := addVersion(t, ctx, ti, created.Skill.ID, skillManifest("files-carry", "V2.", "# V2"), nil)
	require.True(t, carried.CreatedVersion)
	require.Equal(t, created.Version.Files, carried.Version.Files)
	file, err := ti.service.GetVersionFile(ctx, &gen.GetVersionFilePayload{
		SkillID: created.Skill.ID, VersionID: carried.Version.ID, Path: "references/guide.md",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []byte("guide"), file.Content)

	replaced := addVersion(t, ctx, ti, created.Skill.ID, skillManifest("files-carry", "V3.", "# V3"), []*types.SkillFileInput{
		fileInput("scripts/new.py", "print(1)\n"),
	})
	require.Equal(t, []string{"scripts/new.py"}, filePaths(replaced.Version.Files))

	cleared := addVersion(t, ctx, ti, created.Skill.ID, skillManifest("files-carry", "V4.", "# V4"), []*types.SkillFileInput{})
	require.True(t, cleared.CreatedVersion)
	require.Empty(t, cleared.Version.Files)
	_, err = ti.service.GetVersionFile(ctx, &gen.GetVersionFilePayload{
		SkillID: created.Skill.ID, VersionID: cleared.Version.ID, Path: "scripts/new.py",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Restoring an older version brings its own files back.
	restored, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{
		ID: created.Skill.ID, VersionID: replaced.Version.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, replaced.Version.Files, restored.Version.Files)

	// Carry-forward follows the restored current version.
	afterRestore := addVersion(t, ctx, ti, created.Skill.ID, skillManifest("files-carry", "V5.", "# V5"), nil)
	require.Equal(t, []string{"scripts/new.py"}, filePaths(afterRestore.Version.Files))
}

func TestSkillsAddVersionCarryForwardIdenticalIsNoOp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	content := skillManifest("files-noop", "Same.", "# Same")
	created := createSkillWithFiles(t, ctx, ti, content, []*types.SkillFileInput{fileInput("references/a.md", "a")})

	again := addVersion(t, ctx, ti, created.Skill.ID, content, nil)
	require.False(t, again.CreatedVersion)
	require.Equal(t, created.Version.ID, again.Version.ID)
	require.Equal(t, created.Version.Files, again.Version.Files)
}

func TestSkillsSkillManifestOnlyDigestUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	content := "---\nname: My_Skill\ndescription: First summary.\nmetadata:\n  owner: platform\n---\n\n# Body\n"
	explicitEmpty := createSkillWithFiles(t, ctx, ti, content, []*types.SkillFileInput{})
	require.Equal(t, "2ec7c886f769f3f8a417d2b6a62c55de38a3aa5aaf1655e519203b67daa51721", explicitEmpty.Version.CanonicalSha256)
	require.NotNil(t, explicitEmpty.Version.Files)
	require.Empty(t, explicitEmpty.Version.Files)
}

func TestSkillsWriteRejectsInvalidFiles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "files-invalid", "Base.")

	tooMany := make([]*types.SkillFileInput, skilltree.MaxFiles+1)
	for i := range tooMany {
		tooMany[i] = fileInput("assets/"+uuid.NewString()+".txt", "x")
	}
	half := strings.Repeat("x", skilltree.MaxFileBytes)

	cases := []struct {
		name    string
		files   []*types.SkillFileInput
		message string
	}{
		{"absolute path", []*types.SkillFileInput{fileInput("/etc/passwd", "x")}, "relative"},
		{"parent escape", []*types.SkillFileInput{fileInput("../outside.md", "x")}, "escapes"},
		{"backslash", []*types.SkillFileInput{fileInput(`scripts\run.sh`, "x")}, "forward slashes"},
		{"manifest", []*types.SkillFileInput{fileInput("SKILL.md", "x")}, "manifest"},
		{"case duplicate", []*types.SkillFileInput{fileInput("a.md", "x"), fileInput("A.md", "y")}, "duplicate"},
		{"too many", tooMany, "too many"},
		{"file too large", []*types.SkillFileInput{fileInput("assets/big.bin", half+"x")}, "too large"},
		{"tree too large", []*types.SkillFileInput{fileInput("assets/a.bin", half), fileInput("assets/b.bin", half), fileInput("assets/c.bin", "x")}, "too large"},
		{"null entry", []*types.SkillFileInput{nil}, "null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.Create(ctx, &gen.CreatePayload{Content: skillManifest("files-invalid-create", "X.", "body"), Files: tc.files, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.Contains(t, err.Error(), tc.message)

			_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
				ID: created.Skill.ID, Content: skillManifest("files-invalid", "X.", "body"), Files: tc.files,
				DerivedFromVersionID: nil, ExpectedLatestVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
			})
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

func TestSkillsGetVersionFileAuthzAndNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkillWithFiles(t, ctx, ti, skillManifest("files-authz", "Authz.", "# Authz"), []*types.SkillFileInput{
		fileInput("references/a.md", "a"),
	})
	other := createSkillWithFiles(t, ctx, ti, skillManifest("files-authz-other", "Other.", "# Other"), nil)
	payload := func(skillID, versionID, path string) *gen.GetVersionFilePayload {
		return &gen.GetVersionFilePayload{SkillID: skillID, VersionID: versionID, Path: path, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}
	}

	_, err := ti.service.GetVersionFile(authztest.WithExactGrants(t, ctx), payload(created.Skill.ID, created.Version.ID, "references/a.md"))
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.GetVersionFile(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, other.Skill.ID)), payload(created.Skill.ID, created.Version.ID, "references/a.md"))
	requireOopsCode(t, err, oops.CodeForbidden)

	resourceRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, created.Skill.ID))
	file, err := ti.service.GetVersionFile(resourceRead, payload(created.Skill.ID, created.Version.ID, "references/a.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("a"), file.Content)

	projectRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err = ti.service.GetVersionFile(projectRead, payload(created.Skill.ID, created.Version.ID, "references/missing.md"))
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetVersionFile(projectRead, payload(other.Skill.ID, created.Version.ID, "references/a.md"))
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetVersionFile(projectRead, payload(uuid.NewString(), created.Version.ID, "references/a.md"))
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetVersionFile(projectRead, payload(created.Skill.ID, "not-a-uuid", "references/a.md"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillsApproveSuggestionCarriesBaseFiles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkillWithFiles(t, ctx, ti, skillManifest("files-approve", "Base.", "# Base"), []*types.SkillFileInput{
		fileInput("references/a.md", "a"),
	})
	suggestion := createSuggestion(t, ti, created, skillManifest("files-approve", "Proposed.", "# Base"), "Clearer.")

	result, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, Files: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Outcome)
	require.Equal(t, created.Version.Files, result.Version.Files)
}

func TestSkillsApproveSuggestionWithExplicitFiles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkillWithFiles(t, ctx, ti, skillManifest("files-approve-explicit", "Base.", "# Base"), []*types.SkillFileInput{
		fileInput("references/a.md", "a"),
	})
	suggestion := createSuggestion(t, ti, created, skillManifest("files-approve-explicit", "Proposed.", "# Base"), "Clearer.")

	result, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, Files: []*types.SkillFileInput{fileInput("scripts/b.sh", "b")},
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"scripts/b.sh"}, filePaths(result.Version.Files))

	// Files alone make an approval meaningful when SKILL.md is unchanged.
	current := &gen.RecordSkillResult{Skill: created.Skill, Version: result.Version, CreatedSkill: false, CreatedVersion: true}
	unchanged := createSuggestion(t, ti, current, skillManifest("files-approve-explicit", "Proposed again.", "# Base"), "More.")
	baseContent := result.Version.Content
	approved, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: unchanged.ID.String(), Content: &baseContent, ChangeIds: nil, Files: []*types.SkillFileInput{},
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, approved.Version.Files)
	require.Equal(t, result.Version.RawSha256, approved.Version.RawSha256)
}

func TestSkillsApproveSuggestionRejectsUnchangedTree(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkillWithFiles(t, ctx, ti, skillManifest("files-approve-noop", "Base.", "# Base"), []*types.SkillFileInput{
		fileInput("references/a.md", "a"),
	})
	suggestion := createSuggestion(t, ti, created, skillManifest("files-approve-noop", "Proposed.", "# Base"), "Clearer.")

	baseContent := created.Version.Content
	_, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: &baseContent, ChangeIds: nil, Files: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Contains(t, err.Error(), "unchanged from base")
}

func TestSkillsGetVersionFileUpToLoadsContentOnlyWithinTheLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkillWithFiles(t, ctx, ti, skillManifest("files-up-to", "Up to.", "# Up to"), []*types.SkillFileInput{
		fileInput("references/a.md", "hello"),
		fileInput("references/empty.md", ""),
	})
	payload := func(path string) *gen.GetVersionFilePayload {
		return &gen.GetVersionFilePayload{SkillID: created.Skill.ID, VersionID: created.Version.ID, Path: path, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}
	}

	file, omitted, err := ti.service.GetVersionFileUpTo(ctx, payload("references/a.md"), 5)
	require.NoError(t, err)
	require.False(t, omitted)
	require.Equal(t, []byte("hello"), file.Content)

	file, omitted, err = ti.service.GetVersionFileUpTo(ctx, payload("references/a.md"), 4)
	require.NoError(t, err)
	require.True(t, omitted)
	require.Nil(t, file.Content)
	require.Equal(t, int64(5), file.SizeBytes)

	file, omitted, err = ti.service.GetVersionFileUpTo(ctx, payload("references/empty.md"), 4)
	require.NoError(t, err)
	require.False(t, omitted)
	require.Equal(t, []byte{}, file.Content)

	_, _, err = ti.service.GetVersionFileUpTo(ctx, payload("references/missing.md"), 4)
	requireOopsCode(t, err, oops.CodeNotFound)
	_, _, err = ti.service.GetVersionFileUpTo(authztest.WithExactGrants(t, ctx), payload("references/a.md"), 4)
	requireOopsCode(t, err, oops.CodeForbidden)
}

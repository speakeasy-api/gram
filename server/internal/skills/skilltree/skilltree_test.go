package skilltree_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// singleFileDigest is the digest every skill version stored before supporting
// files existed. Digest must reproduce it for a skill with no supporting files.
func singleFileDigest(manifestDigest [sha256.Size]byte) string {
	preimage := []byte("skill-manifest-v1\x00SKILL.md\x00")
	preimage = append(preimage, manifestDigest[:]...)
	preimage = append(preimage, 0)
	sum := sha256.Sum256(preimage)
	return hex.EncodeToString(sum[:])
}

func TestDigest_ManifestOnlyMatchesSingleFileDigest(t *testing.T) {
	t.Parallel()

	manifestDigest := sha256.Sum256([]byte("---\nname: demo\n---\n"))

	got, err := skilltree.Digest(manifestDigest, nil)
	require.NoError(t, err)
	require.Equal(t, singleFileDigest(manifestDigest), got)
}

func TestDigest_ChangesWhenASupportingFileChanges(t *testing.T) {
	t.Parallel()

	manifestDigest := sha256.Sum256([]byte("manifest"))
	before, err := skilltree.Validate([]skilltree.File{{Path: "scripts/run.py", Content: []byte("print(1)"), ContentType: ""}})
	require.NoError(t, err)
	after, err := skilltree.Validate([]skilltree.File{{Path: "scripts/run.py", Content: []byte("print(2)"), ContentType: ""}})
	require.NoError(t, err)

	beforeDigest, err := skilltree.Digest(manifestDigest, before)
	require.NoError(t, err)
	afterDigest, err := skilltree.Digest(manifestDigest, after)
	require.NoError(t, err)
	manifestOnly, err := skilltree.Digest(manifestDigest, nil)
	require.NoError(t, err)

	require.NotEqual(t, beforeDigest, afterDigest)
	require.NotEqual(t, manifestOnly, beforeDigest)
}

func TestDigest_IgnoresInputOrder(t *testing.T) {
	t.Parallel()

	manifestDigest := sha256.Sum256([]byte("manifest"))
	a := skilltree.File{Path: "references/a.md", Content: []byte("a"), ContentType: ""}
	b := skilltree.File{Path: "assets/b.png", Content: []byte("b"), ContentType: ""}

	forward, err := skilltree.Validate([]skilltree.File{a, b})
	require.NoError(t, err)
	reverse, err := skilltree.Validate([]skilltree.File{b, a})
	require.NoError(t, err)

	forwardDigest, err := skilltree.Digest(manifestDigest, forward)
	require.NoError(t, err)
	reverseDigest, err := skilltree.Digest(manifestDigest, []skilltree.Entry{reverse[1], reverse[0]})
	require.NoError(t, err)
	require.Equal(t, forwardDigest, reverseDigest)
}

func TestDigest_RejectsMalformedEntryDigest(t *testing.T) {
	t.Parallel()

	_, err := skilltree.Digest(sha256.Sum256(nil), []skilltree.Entry{{Path: "a.txt", Content: nil, SHA256: "not-hex", Size: 0, ContentType: ""}})
	require.Error(t, err)
}

func TestValidatePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		path  string
		valid bool
	}{
		{name: "reference file", path: "references/REFERENCE.md", valid: true},
		{name: "root file", path: "LICENSE.txt", valid: true},
		{name: "nested asset", path: "assets/templates/report.html", valid: true},
		{name: "hidden file", path: ".env.example", valid: true},
		{name: "empty", path: "", valid: false},
		{name: "absolute", path: "/etc/passwd", valid: false},
		{name: "parent escape", path: "../secret", valid: false},
		{name: "inner parent segment", path: "scripts/../../x", valid: false},
		{name: "dot segment", path: "./scripts/run.sh", valid: false},
		{name: "double slash", path: "scripts//run.sh", valid: false},
		{name: "trailing slash", path: "scripts/", valid: false},
		{name: "backslash", path: `scripts\run.sh`, valid: false},
		{name: "control character", path: "scripts/run\x00.sh", valid: false},
		{name: "skill root", path: ".", valid: false},
		{name: "bidi override", path: "scripts/run\u202egnp.sh", valid: false},
		{name: "zero width joiner", path: "scripts/r\u200dun.sh", valid: false},
		{name: "non-ascii letters", path: "references/café.md", valid: true},
		{name: "invalid utf8", path: "scripts/\xff.sh", valid: false},
		{name: "manifest", path: "SKILL.md", valid: false},
		{name: "manifest other case", path: "skill.md", valid: false},
		{name: "under manifest", path: "SKILL.md/x", valid: false},
		{name: "under manifest other case", path: "Skill.MD/scripts/run.sh", valid: false},
		{name: "manifest name deeper", path: "references/SKILL.md", valid: true},
		{name: "too long", path: strings.Repeat("a", skilltree.MaxPathBytes+1), valid: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := skilltree.ValidatePath(tc.path)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, skilltree.ErrInvalidPath)
		})
	}
}

func TestValidate_SortsEntriesAndComputesMetadata(t *testing.T) {
	t.Parallel()

	entries, err := skilltree.Validate([]skilltree.File{
		{Path: "scripts/run.py", Content: []byte("print(1)"), ContentType: ""},
		{Path: "assets/logo.bin", Content: []byte{0x00, 0x01}, ContentType: "application/x-custom"},
		{Path: "references/REFERENCE.md", Content: []byte("# Ref"), ContentType: ""},
	})
	require.NoError(t, err)
	require.Len(t, entries, 3)

	require.Equal(t, "assets/logo.bin", entries[0].Path)
	require.Equal(t, "application/x-custom", entries[0].ContentType)
	require.Equal(t, "references/REFERENCE.md", entries[1].Path)
	require.Equal(t, "text/markdown; charset=utf-8", entries[1].ContentType)
	require.Equal(t, "scripts/run.py", entries[2].Path)
	require.Equal(t, "text/x-python; charset=utf-8", entries[2].ContentType)

	sum := sha256.Sum256([]byte("print(1)"))
	require.Equal(t, hex.EncodeToString(sum[:]), entries[2].SHA256)
	require.Equal(t, int64(len("print(1)")), entries[2].Size)
}

func TestValidate_RejectsCaseOnlyDuplicates(t *testing.T) {
	t.Parallel()

	_, err := skilltree.Validate([]skilltree.File{
		{Path: "references/Guide.md", Content: []byte("a"), ContentType: ""},
		{Path: "references/guide.md", Content: []byte("b"), ContentType: ""},
	})
	require.ErrorIs(t, err, skilltree.ErrDuplicatePath)
}

func TestValidate_RejectsFileThatIsAlsoADirectory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		paths []string
		want  error
	}{
		{name: "file then nested file", paths: []string{"scripts", "scripts/run.py"}, want: skilltree.ErrPathConflict},
		{name: "nested file then file", paths: []string{"scripts/run.py", "scripts"}, want: skilltree.ErrPathConflict},
		{name: "deep ancestor", paths: []string{"assets/templates", "assets/templates/a/b.html"}, want: skilltree.ErrPathConflict},
		{name: "ancestor other case", paths: []string{"Scripts", "scripts/run.py"}, want: skilltree.ErrPathConflict},
		{name: "under manifest", paths: []string{"SKILL.md/x"}, want: skilltree.ErrInvalidPath},
		{name: "shared prefix is not a directory", paths: []string{"scripts", "scripts.md", "scripts-old/run.py"}, want: nil},
		{name: "siblings", paths: []string{"scripts/a.py", "scripts/b.py", "references/a.md"}, want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := make([]skilltree.File, 0, len(tc.paths))
			for _, p := range tc.paths {
				files = append(files, skilltree.File{Path: p, Content: []byte("x"), ContentType: ""})
			}
			_, err := skilltree.Validate(files)
			if tc.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestValidate_RejectsTooManyFiles(t *testing.T) {
	t.Parallel()

	files := make([]skilltree.File, skilltree.MaxFiles+1)
	for i := range files {
		files[i] = skilltree.File{Path: "f" + strings.Repeat("x", i), Content: nil, ContentType: ""}
	}

	_, err := skilltree.Validate(files)
	require.ErrorIs(t, err, skilltree.ErrTooManyFiles)
}

func TestValidate_RejectsOversizedFile(t *testing.T) {
	t.Parallel()

	_, err := skilltree.Validate([]skilltree.File{
		{Path: "assets/big.bin", Content: bytes.Repeat([]byte{1}, skilltree.MaxFileBytes+1), ContentType: ""},
	})
	require.ErrorIs(t, err, skilltree.ErrFileTooLarge)
}

func TestValidate_RejectsOversizedTree(t *testing.T) {
	t.Parallel()

	chunk := bytes.Repeat([]byte{1}, skilltree.MaxFileBytes)
	_, err := skilltree.Validate([]skilltree.File{
		{Path: "assets/a.bin", Content: chunk, ContentType: ""},
		{Path: "assets/b.bin", Content: chunk, ContentType: ""},
		{Path: "assets/c.bin", Content: []byte{1}, ContentType: ""},
	})
	require.ErrorIs(t, err, skilltree.ErrTreeTooLarge)
}

func TestValidateRootName(t *testing.T) {
	t.Parallel()

	require.NoError(t, skilltree.ValidateRootName("pdf-tools", ""))
	require.NoError(t, skilltree.ValidateRootName("pdf-tools", "pdf-tools"))
	require.Error(t, skilltree.ValidateRootName("pdf-tools", "pdf"))
}

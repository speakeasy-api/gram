package skills

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func TestWriteSkillArchiveRejectsInvalidPath(t *testing.T) {
	t.Parallel()

	files := []repo.ListSkillVersionFilesRow{{Path: "../escape.txt", Content: []byte("x"), Sha256: "", SizeBytes: 1, ContentType: "text/plain"}}
	err := writeSkillArchive(io.Discard, "bad-skill", "---\nname: bad-skill\n---\n", files)
	require.ErrorIs(t, err, skilltree.ErrInvalidPath, "a stored file the archive cannot hold fails the download instead of being left out")

	var buf bytes.Buffer
	require.NoError(t, writeSkillArchive(&buf, "good-skill", "---\nname: good-skill\n---\n", nil))
	require.NotZero(t, buf.Len())
}

func TestFormatFileSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		size int64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 1023, want: "1023 B"},
		{size: 1024, want: "1.0 KB"},
		{size: 1536, want: "1.5 KB"},
		{size: 1024*1024 - 1, want: "1.0 MB"},
		{size: 1024*1024 - 52*1024, want: "972.0 KB"},
		{size: 10 * 1024 * 1024, want: "10.0 MB"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, formatFileSize(tc.size), "size=%d", tc.size)
	}
}

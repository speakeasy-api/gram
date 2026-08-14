package ignore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sqlclint/ignore"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".sqlclintignore")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadSeparatesGlobsFromEntries(t *testing.T) {
	t.Parallel()

	f, err := ignore.Load(write(t, `
# a comment
server/internal/testenv/queries.sql
**/audittest/queries.sql

server/internal/chat/queries.sql::DeleteChatResolutions sha256:abc123
`))
	require.NoError(t, err)

	require.Equal(t, []string{"server/internal/testenv/queries.sql", "**/audittest/queries.sql"}, f.Globs)
	require.Len(t, f.Entries, 1)

	entry, ok := f.Lookup("server/internal/chat/queries.sql::DeleteChatResolutions")
	require.True(t, ok)
	require.Equal(t, "sha256:abc123", entry.Hash)
}

// A first run must work with no file present rather than requiring setup.
func TestLoadTreatsAMissingFileAsEmpty(t *testing.T) {
	t.Parallel()

	f, err := ignore.Load(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	require.Empty(t, f.Globs)
	require.Empty(t, f.Entries)
}

func TestLoadRejectsAHashedLineThatIsNotAQueryRef(t *testing.T) {
	t.Parallel()

	_, err := ignore.Load(write(t, "server/internal/chat/queries.sql sha256:abc123\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a <file>::<Query> reference")
}

func TestSkippedMatchesGlobs(t *testing.T) {
	t.Parallel()

	f, err := ignore.Load(write(t, "server/internal/testenv/queries.sql\n**/audittest/queries.sql\nserver/*/generated/*.sql\n"))
	require.NoError(t, err)

	require.True(t, f.Skipped("server/internal/testenv/queries.sql"))
	require.True(t, f.Skipped("server/internal/audit/audittest/queries.sql"))
	require.True(t, f.Skipped("server/x/generated/a.sql"))

	require.False(t, f.Skipped("server/internal/toolsets/queries.sql"))
	require.False(t, f.Skipped("server/internal/audittest.sql"))
	// ** must span whole segments, not partial names.
	require.False(t, f.Skipped("server/internal/testenv/other.sql"))
}

func TestSaveRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".sqlclintignore")
	globs := []string{"**/testenv/queries.sql"}
	entries := []ignore.Entry{
		{Ref: "b/queries.sql::Zeta", Hash: "sha256:222"},
		{Ref: "a/queries.sql::Alpha", Hash: "sha256:111"},
	}

	require.NoError(t, ignore.Save(path, globs, entries))

	f, err := ignore.Load(path)
	require.NoError(t, err)
	require.Equal(t, globs, f.Globs)
	require.Len(t, f.Entries, 2)
	require.Equal(t, "sha256:111", f.Entries["a/queries.sql::Alpha"].Hash)
}

// A file whose order depended on map iteration would produce a noisy diff on
// every regeneration and make the ratchet impossible to review.
func TestSaveIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()

	entries := []ignore.Entry{
		{Ref: "b/queries.sql::Zeta", Hash: "sha256:222"},
		{Ref: "a/queries.sql::Alpha", Hash: "sha256:111"},
	}

	first := filepath.Join(t.TempDir(), "one")
	second := filepath.Join(t.TempDir(), "two")
	require.NoError(t, ignore.Save(first, nil, entries))

	entries[0], entries[1] = entries[1], entries[0]
	require.NoError(t, ignore.Save(second, nil, entries))

	a, err := os.ReadFile(first)
	require.NoError(t, err)
	b, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, string(a), string(b))

	require.Less(t,
		indexOf(string(a), "a/queries.sql::Alpha"),
		indexOf(string(a), "b/queries.sql::Zeta"))
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

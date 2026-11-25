package runnerkeys_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
)

var (
	// queryNamePattern matches an sqlc "-- name: <Name> :<kind>" line.
	queryNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)

	// writePattern matches a statement that writes rows.
	writePattern = regexp.MustCompile(`(?is)\bUPDATE\s+[\w."]+(\s+(AS\s+)?\w+)?\s+SET\b|\bINSERT\s+INTO\b|\bDO\s+UPDATE\b`)

	// lineCommentPattern matches a SQL line comment.
	lineCommentPattern = regexp.MustCompile(`--[^\n]*`)

	// keyColumnPattern matches either copy of a runner key. The table name
	// is checked separately, since other tables may use the same names.
	keyColumnPattern = regexp.MustCompile(`(?i)\bencryption_key(_org_encrypted)?\b`)

	// tablePattern matches the runner key table.
	tablePattern = regexp.MustCompile(`(?i)\bfunctions_access\b`)
)

func TestRunnerKeyWritesAreFenced(t *testing.T) {
	t.Parallel()

	orgencryptiontest.RequireFencedWrites(t, mustRead(t, "queries.sql"), "encryption_key", "encryption_key_org_encrypted")
}

// TestOnlyRunnerKeyQueriesWriteTheKey keeps every write of a runner key copy
// in this package's queries.sql, behind Keys and the CopyStore. A statement
// elsewhere could write a copy without the fence, so a transition could miss
// it. Test fixtures, whose names contain Fixture, may plant columns directly.
func TestOnlyRunnerKeyQueriesWriteTheKey(t *testing.T) {
	t.Parallel()

	allowed, err := filepath.Abs("queries.sql")
	require.NoError(t, err)

	offenders := map[string][]string{}
	var fixtures []string
	err = filepath.WalkDir("../../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sql") || strings.Contains(path, "migrations"+string(filepath.Separator)) {
			return nil
		}
		absolute, err := filepath.Abs(path)
		require.NoError(t, err)
		if absolute == allowed {
			return nil
		}

		for _, name := range keyWrites(mustRead(t, path)) {
			if strings.Contains(name, "Fixture") {
				fixtures = append(fixtures, name)
				continue
			}
			offenders[path] = append(offenders[path], name)
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "these queries write a function runner key copy outside runnerkeys/queries.sql; write it through runnerkeys.Keys instead")
	require.NotEmpty(t, fixtures, "the guard reads the fixtures it exempts")
	require.Empty(t, fixtureCallers(t, fixtures), "production code calls a fixture that writes function runner key copies unfenced; write them through runnerkeys.Keys instead")
}

// fixtureCallers returns the non-test Go files, outside generated sqlc
// packages, that call any of fixtures.
func fixtureCallers(t *testing.T, fixtures []string) map[string][]string {
	t.Helper()

	callers := map[string][]string{}
	err := filepath.WalkDir("../../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".sql.go") {
			return nil
		}

		content := mustRead(t, path)
		for _, name := range fixtures {
			if strings.Contains(content, "."+name+"(") {
				callers[path] = append(callers[path], name)
			}
		}
		return nil
	})
	require.NoError(t, err)

	return callers
}

// keyWrites returns the names of the queries in queriesSQL whose statement
// writes functions_access and names a key copy.
func keyWrites(queriesSQL string) []string {
	var names []string
	locations := queryNamePattern.FindAllStringSubmatchIndex(queriesSQL, -1)
	for i, location := range locations {
		end := len(queriesSQL)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		statement := lineCommentPattern.ReplaceAllString(queriesSQL[location[1]:end], "")
		if writePattern.MatchString(statement) && tablePattern.MatchString(statement) && keyColumnPattern.MatchString(statement) {
			names = append(names, queriesSQL[location[2]:location[3]])
		}
	}

	return names
}

func mustRead(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(content)
}

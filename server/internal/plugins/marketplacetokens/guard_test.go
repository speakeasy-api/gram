package marketplacetokens_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fixtureCallersOutsideTests are non-test Go packages that exist to support
// tests and may call the fixture queries that plant token columns, relative
// to this package.
var fixtureCallersOutsideTests = []string{
	"./marketplacetokenstest",
}

var (
	// queryNamePattern matches an sqlc "-- name: <Name> :<kind>" line.
	queryNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)

	// writePattern matches a statement that writes rows.
	writePattern = regexp.MustCompile(`(?is)\bUPDATE\s+[\w."]+(\s+(AS\s+)?\w+)?\s+SET\b|\bINSERT\s+INTO\b|\bDO\s+UPDATE\b`)

	// lineCommentPattern matches a SQL line comment.
	lineCommentPattern = regexp.MustCompile(`--[^\n]*`)

	// tokenColumnPattern matches any stored form of a token: the hash, either
	// sealed copy, or the deprecated plaintext column.
	tokenColumnPattern = regexp.MustCompile(`(?i)\bmarketplace_token(_hash|_encrypted|_org_encrypted)?\b`)
)

// tokenWrites returns the names of the queries in queriesSQL whose statement
// writes rows and names a token column, fenced or not.
func tokenWrites(queriesSQL string) []string {
	var names []string
	locations := queryNamePattern.FindAllStringSubmatchIndex(queriesSQL, -1)
	for i, location := range locations {
		end := len(queriesSQL)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		statement := lineCommentPattern.ReplaceAllString(queriesSQL[location[1]:end], "")
		if writePattern.MatchString(statement) && tokenColumnPattern.MatchString(statement) {
			names = append(names, queriesSQL[location[2]:location[3]])
		}
	}

	return names
}

// TestOnlyMarketplaceTokenQueriesWriteTheToken keeps every write of a token
// column in marketplacetoken_queries.sql, behind Tokens and the CopyStore. A
// statement elsewhere could store a token in plaintext again, or write one
// copy without the hash, so a URL would stop resolving or stop opening. Test
// fixtures, whose names contain Fixture, may plant columns directly;
// production code never calls them.
func TestOnlyMarketplaceTokenQueriesWriteTheToken(t *testing.T) {
	t.Parallel()

	allowed, err := filepath.Abs(tokenQueriesPath)
	require.NoError(t, err)

	offenders := map[string][]string{}
	var fixtures []string
	err = filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if absolute == allowed {
			for _, name := range tokenWrites(mustRead(t, path)) {
				if strings.Contains(name, "Fixture") {
					fixtures = append(fixtures, name)
				}
			}
			return nil
		}

		for _, name := range tokenWrites(mustRead(t, path)) {
			if strings.Contains(name, "Fixture") {
				fixtures = append(fixtures, name)
				continue
			}
			offenders[path] = append(offenders[path], name)
		}

		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "these queries write a marketplace token column outside marketplacetoken_queries.sql; write it through marketplacetokens.Tokens instead")
	require.NotEmpty(t, fixtures, "the guard reads the fixtures it protects")

	callers := fixtureCallers(t, fixtures)
	require.Empty(t, callers, "production code calls a fixture that plants marketplace token columns directly; write the token through marketplacetokens.Tokens instead")
}

func mustRead(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(content)
}

// fixtureCallers returns the non-test Go files outside the generated repo
// packages and the test support packages that call any of fixtures.
func fixtureCallers(t *testing.T, fixtures []string) map[string][]string {
	t.Helper()

	exempt := make([]string, 0, len(fixtureCallersOutsideTests))
	for _, dir := range fixtureCallersOutsideTests {
		absolute, err := filepath.Abs(dir)
		require.NoError(t, err)
		exempt = append(exempt, absolute+string(filepath.Separator))
	}

	callers := map[string][]string{}
	err := filepath.WalkDir("../../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(filepath.Dir(path)) == "repo" {
			return nil
		}

		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		for _, dir := range exempt {
			if strings.HasPrefix(absolute, dir) {
				return nil
			}
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

package clientsecrets_test

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

// guardExemptFiles are queries files that write a column named like a client
// secret copy but are not covered by this guard, relative to this package.
var guardExemptFiles = map[string]string{
	// assistant_mcp_oauth_clients has its own client_secret_encrypted column,
	// which moves to organization keys separately (AIM-970).
	"../../assistants/queries.sql": "another table",

	// The demo seed stores a placeholder that is not a real secret and never
	// decrypts. Moving it onto Secrets is a follow-up.
	"../../demoseed/queries/03_deployment.sql": "demo placeholder",
	"../../demoseed/queries/04_servers.sql":    "demo placeholder",

	// The local development seed writes a platform-key secret for the local
	// identity chaining fixture, whose organization stays in platform_key.
	// Moving it onto Secrets is part of the same follow-up.
	"../../demoseed/queries/local.sql": "local development seed",
}

// fixtureCallersOutsideTests are non-test Go packages that exist to support
// tests and may call the fixture queries that plant secret copies, relative
// to this package.
var fixtureCallersOutsideTests = []string{
	"../../oauthtest",
	"../../testenv",
}

var (
	// queryNamePattern matches an sqlc "-- name: <Name> :<kind>" line.
	queryNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)

	// writePattern matches a statement that writes rows.
	writePattern = regexp.MustCompile(`(?is)\bUPDATE\s+[\w."]+(\s+(AS\s+)?\w+)?\s+SET\b|\bINSERT\s+INTO\b|\bDO\s+UPDATE\b`)

	// lineCommentPattern matches a SQL line comment.
	lineCommentPattern = regexp.MustCompile(`--[^\n]*`)

	// secretColumnPattern matches either copy's bare column name.
	secretColumnPattern = regexp.MustCompile(`(?i)\bclient_secret_(org_)?encrypted\b`)
)

// secretWrites returns the names of the queries in queriesSQL whose statement
// writes rows and names either secret copy, fenced or not.
func secretWrites(queriesSQL string) []string {
	var names []string
	locations := queryNamePattern.FindAllStringSubmatchIndex(queriesSQL, -1)
	for i, location := range locations {
		end := len(queriesSQL)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		statement := lineCommentPattern.ReplaceAllString(queriesSQL[location[1]:end], "")
		if writePattern.MatchString(statement) && secretColumnPattern.MatchString(statement) {
			names = append(names, queriesSQL[location[2]:location[3]])
		}
	}

	return names
}

// TestOnlyClientSecretQueriesWriteTheSecret keeps every write of either copy
// in clientsecret_queries.sql, behind Secrets and the CopyStore. A statement
// elsewhere could write one copy without the other, so a removed secret would
// come back from the copy it left behind, or skip the revision. Test
// fixtures, whose names contain Fixture, may plant copies directly;
// production code never calls them.
func TestOnlyClientSecretQueriesWriteTheSecret(t *testing.T) {
	t.Parallel()

	allowed, err := filepath.Abs(clientSecretQueriesPath)
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
			return nil
		}

		if _, skip := guardExemptFiles[filepath.ToSlash(path)]; skip {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		for _, name := range secretWrites(string(content)) {
			if strings.Contains(name, "Fixture") {
				fixtures = append(fixtures, name)
				continue
			}
			offenders[path] = append(offenders[path], name)
		}

		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "these queries write a client secret copy outside clientsecret_queries.sql; write it through clientsecrets.Secrets instead")

	callers := fixtureCallers(t, fixtures)
	require.Empty(t, callers, "production code calls a fixture that plants client secret copies directly; write the secret through clientsecrets.Secrets instead")
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
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
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

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for _, name := range fixtures {
			if strings.Contains(string(content), "."+name+"(") {
				callers[path] = append(callers[path], name)
			}
		}

		return nil
	})
	require.NoError(t, err)

	return callers
}

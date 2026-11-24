package ingresscredentials_test

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

// queriesRoot is the directory every sqlc queries file lives under, relative
// to this package.
const queriesRoot = "../.."

var (
	// queryNamePattern matches an sqlc "-- name: <Name> :<kind>" line.
	queryNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)

	// writePattern matches a statement that writes rows.
	writePattern = regexp.MustCompile(`(?is)\bUPDATE\s+[\w."]+(\s+(AS\s+)?\w+)?\s+SET\b|\bINSERT\s+INTO\b|\bDO\s+UPDATE\b`)

	// lineCommentPattern matches a SQL line comment.
	lineCommentPattern = regexp.MustCompile(`--[^\n]*`)

	// tablePattern matches the network ingress table name.
	tablePattern = regexp.MustCompile(`(?i)\bnetwork_ingresses\b`)

	// credentialColumnPattern matches either copy's bare column name. Other
	// tables have a credentials_encrypted column too, so a statement only
	// counts when it also names network_ingresses.
	credentialColumnPattern = regexp.MustCompile(`(?i)\bcredentials_(org_)?encrypted\b`)
)

// TestOnlyCredentialQueriesWriteTheCredentials keeps every write of either
// copy in credential_queries.sql, behind Credentials and the CopyStore. A
// statement elsewhere could write one copy without the other, or skip the
// transition fence.
func TestOnlyCredentialQueriesWriteTheCredentials(t *testing.T) {
	t.Parallel()

	allowed, err := filepath.Abs(credentialQueriesPath)
	require.NoError(t, err)

	var offenders []string
	err = filepath.WalkDir(queriesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if absolute == allowed {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		queries := string(contents)
		locations := queryNamePattern.FindAllStringSubmatchIndex(queries, -1)
		for i, location := range locations {
			end := len(queries)
			if i+1 < len(locations) {
				end = locations[i+1][0]
			}
			statement := lineCommentPattern.ReplaceAllString(queries[location[1]:end], "")
			if writePattern.MatchString(statement) && tablePattern.MatchString(statement) && credentialColumnPattern.MatchString(statement) {
				offenders = append(offenders, path+": "+queries[location[2]:location[3]])
			}
		}

		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "only %s may write network ingress credentials", credentialQueriesPath)
}

package orgencryptiontest

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// unfencedMarker exempts one statement from the fence check.
	unfencedMarker = "-- orgencryption:unfenced"

	// transitionTable is the table the fence subquery reads and locks.
	transitionTable = "organization_encryption_transitions"
)

var (
	// unfencedMarkerPattern matches unfencedMarker only as its own comment
	// line followed by a reason, so text inside a SQL string or another
	// comment cannot exempt a statement.
	unfencedMarkerPattern = regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(unfencedMarker) + `[ \t]+\S`)

	// queryNamePattern matches an sqlc "-- name: <Name> :<kind>" line.
	queryNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)

	// writePattern matches a statement that writes rows: UPDATE ... SET with
	// or without a table alias, INSERT INTO, or an upsert's ON CONFLICT ...
	// DO UPDATE.
	writePattern = regexp.MustCompile(`(?is)\bUPDATE\s+[\w."]+(\s+(AS\s+)?\w+)?\s+SET\b|\bINSERT\s+INTO\b|\bDO\s+UPDATE\b`)

	// fencePattern matches the shape of the fence predicate from the
	// orgencryption package documentation: the transition generation read in
	// a scalar subquery, with COALESCE mapping a missing row to generation 0,
	// compared with a query parameter. The first group is the rest of the
	// subquery, which fenceSubqueryPatterns check.
	fencePattern = regexp.MustCompile(`(?is)\bCOALESCE\s*\(\s*\(\s*SELECT\s+(?:\w+\.)?generation\s+FROM\s+organization_encryption_transitions\b([^;]*?)\)\s*,\s*0\s*\)\s*=\s*(?:@\w+|sqlc\.arg\s*\()`)

	// fenceSubqueryPatterns must all match the fence subquery: it compares the
	// transition's organization with the written row's own organization
	// column, and names a use case.
	fenceSubqueryPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?is)\bWHERE\b`),
		regexp.MustCompile(`(?is)\b\w+\.organization_id\s*=\s*\w+\.organization_id\b`),
		regexp.MustCompile(`(?is)\b(?:\w+\.)?use_case\s*=\s*(?:'[^']+'|@\w+|sqlc\.arg\s*\()`),
	}

	// fenceAliasPattern reads the alias the fence subquery gives the
	// transition table, if any, from the text right after the table name.
	fenceAliasPattern = regexp.MustCompile(`(?is)^\s+(?:AS\s+)?(\w+)`)

	// fenceLockPattern matches the locking clause that must end the fence
	// subquery, FOR KEY SHARE, with an optional OF list in the first group.
	// A transition waits for a write holding this lock to commit.
	fenceLockPattern = regexp.MustCompile(`(?is)\bFOR\s+KEY\s+SHARE(?:\s+OF\s+(\w+(?:\s*,\s*\w+)*))?\s*$`)

	// lineCommentPattern matches a SQL line comment.
	lineCommentPattern = regexp.MustCompile(`--[^\n]*`)
)

// UnfencedWrites returns the names of the sqlc queries in queriesSQL that
// write a statement mentioning any of columns without the fence predicate
// described in the orgencryption package documentation. A query is exempt
// when a line after its "-- name:" line starts with
// "-- orgencryption:unfenced", followed by the reason.
func UnfencedWrites(queriesSQL string, columns ...string) []string {
	columnPatterns := make([]*regexp.Regexp, 0, len(columns))
	for _, column := range columns {
		columnPatterns = append(columnPatterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(column)+`\b`))
	}

	var unfenced []string
	locations := queryNamePattern.FindAllStringSubmatchIndex(queriesSQL, -1)
	for i, location := range locations {
		end := len(queriesSQL)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		name := queriesSQL[location[2]:location[3]]
		raw := queriesSQL[location[1]:end]

		if unfencedMarkerPattern.MatchString(raw) {
			continue
		}

		statement := lineCommentPattern.ReplaceAllString(raw, "")
		if !writePattern.MatchString(statement) || fenced(statement) {
			continue
		}

		for _, pattern := range columnPatterns {
			if pattern.MatchString(statement) {
				unfenced = append(unfenced, name)
				break
			}
		}
	}

	return unfenced
}

// fenced reports whether statement carries a fence predicate whose subquery
// passes every fenceSubqueryPatterns check and locks the transition row.
func fenced(statement string) bool {
	for _, match := range fencePattern.FindAllStringSubmatch(statement, -1) {
		subquery := match[1]
		ok := locksTransitionRow(subquery)
		for _, pattern := range fenceSubqueryPatterns {
			if !pattern.MatchString(subquery) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}

	return false
}

// locksTransitionRow reports whether the fence subquery, given as the text
// after the transition table name, ends with FOR KEY SHARE on the transition
// table: either with no OF list, or with an OF list that names the table or
// its alias. A lock on another relation would leave the transition row
// unprotected.
func locksTransitionRow(subquery string) bool {
	lock := fenceLockPattern.FindStringSubmatch(subquery)
	if lock == nil {
		return false
	}
	if lock[1] == "" {
		return true
	}

	names := []string{transitionTable}
	if alias := fenceAliasPattern.FindStringSubmatch(subquery); alias != nil && !strings.EqualFold(alias[1], "WHERE") {
		names = append(names, alias[1])
	}

	for locked := range strings.SplitSeq(lock[1], ",") {
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(locked), name) {
				return true
			}
		}
	}

	return false
}

// RequireFencedWrites fails the test when a query in queriesSQL writes any of
// columns, the bare column names of both copies, without fencing on the
// transition generation. To exempt a statement, put
// "-- orgencryption:unfenced <reason>" on its own line after the statement's
// "-- name:" line. Erasing both copies is the usual exemption: it is correct
// under any transition state.
func RequireFencedWrites(t *testing.T, queriesSQL string, columns ...string) {
	t.Helper()

	require.NotEmpty(t, columns, "list the platform-key and organization-key columns to check")
	require.Empty(t, UnfencedWrites(queriesSQL, columns...), "these queries write an organization encryption column without the transition generation fence or %q", unfencedMarker)
}

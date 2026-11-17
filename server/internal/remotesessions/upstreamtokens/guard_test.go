package upstreamtokens_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// guardExemptFiles are queries files that write a column named like a stored
// token copy but are not covered by this guard, relative to this package.
var guardExemptFiles = map[string]string{
	// The demo seed stores placeholder tokens for the demo organization,
	// which stays in platform_key. Moving it onto Tokens is a follow-up.
	"../../demoseed/queries/04_fixture.sql": "demo placeholder",
	"../../demoseed/queries/04_servers.sql": "demo placeholder",
}

// tokenValues are the bare names of the values the use case holds. Each is
// stored as <value>_encrypted and <value>_org_encrypted.
var tokenValues = []string{"access_token", "refresh_token", "identity_assertion", "upstream_subject", "nonce"}

var (
	// queryNamePattern matches an sqlc "-- name: <Name> :<kind>" line.
	queryNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)

	// writePattern matches a statement that writes rows.
	writePattern = regexp.MustCompile(`(?is)\bUPDATE\s+[\w."]+(\s+(AS\s+)?\w+)?\s+SET\b|\bINSERT\s+INTO\b|\bDO\s+UPDATE\b`)

	// lineCommentPattern matches a SQL line comment.
	lineCommentPattern = regexp.MustCompile(`--[^\n]*`)

	// insertColumnsPattern matches an INSERT's column list.
	insertColumnsPattern = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+[\w."]+(?:\s+AS\s+\w+)?\s*\(([^)]*)\)`)
)

// setClauses returns the assignments of every SET list in a write statement:
// an UPDATE's SET and an upsert's DO UPDATE SET, split on top-level commas and
// ending at the first top-level WHERE, FROM or RETURNING.
func setClauses(statement string) []string {
	var assignments []string
	upper := strings.ToUpper(statement)
	for start := 0; ; {
		i := indexKeyword(upper, "SET", start)
		if i < 0 {
			return assignments
		}
		depth, caseDepth, from := 0, 0, i+len("SET")
		j := from
	scan:
		for ; j < len(statement); j++ {
			switch c := statement[j]; {
			case c == '(':
				depth++
			case c == ')':
				depth--
			case c == ',' && depth == 0 && caseDepth == 0:
				assignments = append(assignments, strings.TrimSpace(statement[from:j]))
				from = j + 1
			case isKeywordAt(upper, j, "CASE"):
				caseDepth++
			case isKeywordAt(upper, j, "END") && caseDepth > 0:
				caseDepth--
			case depth == 0 && caseDepth == 0 && (isKeywordAt(upper, j, "WHERE") || isKeywordAt(upper, j, "FROM") || isKeywordAt(upper, j, "RETURNING")):
				break scan
			}
		}
		assignments = append(assignments, strings.TrimSpace(statement[from:j]))
		start = j
	}
}

// indexKeyword returns the index of keyword as a whole word at or after start.
func indexKeyword(upper, keyword string, start int) int {
	for i := start; i+len(keyword) <= len(upper); i++ {
		if isKeywordAt(upper, i, keyword) {
			return i
		}
	}
	return -1
}

// isKeywordAt reports whether keyword appears as a whole word at i.
func isKeywordAt(upper string, i int, keyword string) bool {
	if !strings.HasPrefix(upper[i:], keyword) {
		return false
	}
	before := i == 0 || !isWordByte(upper[i-1])
	end := i + len(keyword)
	after := end >= len(upper) || !isWordByte(upper[end])
	return before && after
}

func isWordByte(c byte) bool {
	return c == '_' || c == '.' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

var (
	// assignmentPattern splits one SET assignment of a token copy.
	assignmentPattern = regexp.MustCompile(`(?is)^(?:\w+\.)?(\w+?)_(org_)?encrypted\s*=\s*(.*)$`)

	// caseResultPattern matches each result of a CASE expression.
	caseResultPattern = regexp.MustCompile(`(?is)\b(?:THEN|ELSE)\s+(\S+)`)

	// targetTablePattern matches the table a write statement changes.
	targetTablePattern = regexp.MustCompile(`(?is)\b(?:UPDATE|INSERT\s+INTO)\s+(\w+)`)
)

// tokenTables are the tables whose token copies the use case holds. Other
// tables reuse the column names for values that move separately.
var tokenTables = []string{"remote_sessions", "trusted_issuer_sessions", "remote_session_ema_credentials"}

// tokenWriteProblems reports how a write statement outside
// upstreamtoken_queries.sql writes a token copy other than by erasing it:
// every assigned copy must be set to NULL, or to a CASE whose results are NULL
// or the column itself, and its twin must be erased in the same statement. An
// INSERT never names a copy.
func tokenWriteProblems(statement string) []string {
	var problems []string
	assigned := map[string]map[bool]bool{}
	for _, assignment := range setClauses(statement) {
		match := assignmentPattern.FindStringSubmatch(assignment)
		if match == nil || !slices.Contains(tokenValues, strings.ToLower(match[1])) {
			continue
		}
		value, org, expression := strings.ToLower(match[1]), match[2] != "", strings.TrimSpace(match[3])
		if assigned[value] == nil {
			assigned[value] = map[bool]bool{}
		}
		assigned[value][org] = true

		column := value + "_encrypted"
		if org {
			column = value + "_org_encrypted"
		}
		if !erases(expression, column) {
			problems = append(problems, column+" is set to something other than NULL")
		}
	}
	for value, copies := range assigned {
		if !copies[false] || !copies[true] {
			problems = append(problems, value+": only one of its two copies is erased")
		}
	}
	if columns := insertColumnsPattern.FindStringSubmatch(statement); columns != nil {
		for _, value := range tokenValues {
			if regexp.MustCompile(`(?i)\b` + value + `_(org_)?encrypted\b`).MatchString(columns[1]) {
				problems = append(problems, value+" is inserted")
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// erases reports whether expression can only clear column.
func erases(expression, column string) bool {
	if strings.EqualFold(expression, "NULL") {
		return true
	}
	upper := strings.ToUpper(expression)
	if !strings.HasPrefix(upper, "CASE") || !strings.HasSuffix(upper, "END") {
		return false
	}
	for _, match := range caseResultPattern.FindAllStringSubmatch(expression, -1) {
		result := strings.ToLower(match[1])
		if i := strings.LastIndex(result, "."); i >= 0 {
			result = result[i+1:]
		}
		if result != "null" && result != column {
			return false
		}
	}
	return true
}

// TestOnlyUpstreamTokenQueriesStoreATokenCopy keeps every write that stores a
// token copy in upstreamtoken_queries.sql, behind Tokens and the CopyStores,
// and every statement elsewhere to erasing both copies of a value together.
// A statement that stored one copy could write it under the wrong key, and
// one that erased one copy would let a removed token come back from the
// other. Test fixtures, whose names contain Fixture, may plant copies
// directly; production code never calls them.
func TestOnlyUpstreamTokenQueriesStoreATokenCopy(t *testing.T) {
	t.Parallel()

	allowed, err := filepath.Abs(upstreamTokenQueriesPath)
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
		if _, skip := guardExemptFiles[filepath.ToSlash(path)]; skip || absolute == allowed {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		queries := string(content)
		locations := queryNamePattern.FindAllStringSubmatchIndex(queries, -1)
		for i, location := range locations {
			end := len(queries)
			if i+1 < len(locations) {
				end = locations[i+1][0]
			}
			name := queries[location[2]:location[3]]
			statement := lineCommentPattern.ReplaceAllString(queries[location[1]:end], "")
			target := targetTablePattern.FindStringSubmatch(statement)
			if !writePattern.MatchString(statement) || target == nil || !slices.Contains(tokenTables, strings.ToLower(target[1])) {
				continue
			}
			problems := tokenWriteProblems(statement)
			switch {
			case len(problems) == 0:
			case strings.Contains(name, "Fixture"):
				fixtures = append(fixtures, name)
			default:
				offenders[path+": "+name] = problems
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, fixtures, "UpsertRemoteSessionFixture", "the scan reaches the queries that plant token copies")
	require.Empty(t, offenders, "these queries store a token copy outside upstreamtoken_queries.sql, or erase only one copy of a value; store tokens through upstreamtokens.Tokens and erase both copies together")

	callers := fixtureCallers(t, fixtures)
	require.Empty(t, callers, "production code calls a fixture that plants token copies directly; store tokens through upstreamtokens.Tokens instead")
}

// fixtureCallersOutsideTests are non-test Go packages that exist to support
// tests and may call the fixture queries that plant token copies, relative to
// this package.
var fixtureCallersOutsideTests = []string{
	"../../oauthtest",
	"../../testenv",
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

func TestTokenWriteProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		statement string
		want      []string
	}{
		{
			name:      "erases both copies",
			statement: "UPDATE remote_sessions SET refresh_token_encrypted = NULL, refresh_token_org_encrypted = NULL, updated_at = now() WHERE id = $1",
			want:      nil,
		},
		{
			name:      "erases both copies conditionally",
			statement: "UPDATE trusted_issuer_sessions AS s SET nonce_encrypted = CASE WHEN x THEN NULL ELSE s.nonce_encrypted END, nonce_org_encrypted = CASE WHEN x THEN NULL ELSE s.nonce_org_encrypted END WHERE s.id = $1",
			want:      nil,
		},
		{
			name:      "erases one copy",
			statement: "UPDATE remote_session_ema_credentials SET access_token_encrypted = NULL, deleted_at = now() WHERE id = $1",
			want:      []string{"access_token: only one of its two copies is erased"},
		},
		{
			name:      "stores a copy",
			statement: "UPDATE remote_sessions SET access_token_encrypted = @access, access_token_org_encrypted = NULL WHERE id = $1",
			want:      []string{"access_token_encrypted is set to something other than NULL"},
		},
		{
			name:      "conditionally stores a copy",
			statement: "UPDATE remote_sessions AS s SET refresh_token_encrypted = CASE WHEN x THEN @refresh ELSE s.refresh_token_encrypted END, refresh_token_org_encrypted = NULL WHERE s.id = $1",
			want:      []string{"refresh_token_encrypted is set to something other than NULL"},
		},
		{
			name:      "inserts a copy",
			statement: "INSERT INTO remote_session_ema_credentials (subject_urn, access_token_encrypted) VALUES ($1, $2)",
			want:      []string{"access_token is inserted"},
		},
		{
			name:      "compares a copy without writing it",
			statement: "UPDATE remote_sessions AS s SET refresh_expires_at = $1 WHERE s.id = $2 AND s.refresh_token_encrypted = $3",
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, tokenWriteProblems(tc.statement))
		})
	}
}

var (
	// pairedPresencePattern matches a presence check that considers both
	// copies of a value, in the shape the AIM-1000 partial indexes use.
	pairedPresencePattern = regexp.MustCompile(`(?is)\(\s*((?:\w+\.)?)(\w+?)_encrypted\s+IS\s+NOT\s+NULL\s+OR\s+(?:\w+\.)?(\w+?)_org_encrypted\s+IS\s+NOT\s+NULL\s*\)|\(\s*((?:\w+\.)?)(\w+?)_encrypted\s+IS\s+NULL\s+AND\s+(?:\w+\.)?(\w+?)_org_encrypted\s+IS\s+NULL\s*\)`)

	// presencePattern matches any presence check of a platform-key copy.
	presencePattern = regexp.MustCompile(`(?is)\b(?:\w+\.)?(\w+?)_encrypted\s+IS\s+(?:NOT\s+)?NULL`)
)

// unpairedPresenceChecks returns the presence checks in statement that test a
// value's platform-key copy without its organization-key copy.
func unpairedPresenceChecks(statement string) []string {
	paired := pairedPresencePattern.ReplaceAllString(statement, "")
	var unpaired []string
	for _, match := range presencePattern.FindAllStringSubmatch(paired, -1) {
		if slices.Contains(tokenValues, strings.ToLower(match[1])) {
			unpaired = append(unpaired, strings.Join(strings.Fields(match[0]), " "))
		}
	}
	return unpaired
}

// TestEveryTokenPresenceCheckConsidersBothCopies keeps queries that decide
// whether a value is stored from looking at one copy. A finalized organization
// holds only the organization-key copy, so a check of the platform-key copy
// alone would stop its refreshes and cleanups. The CopyStore queries in
// upstreamtoken_queries.sql test one copy on purpose.
func TestEveryTokenPresenceCheckConsidersBothCopies(t *testing.T) {
	t.Parallel()

	allowed, err := filepath.Abs(upstreamTokenQueriesPath)
	require.NoError(t, err)

	offenders := map[string][]string{}
	var checked int
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
		if _, skip := guardExemptFiles[filepath.ToSlash(path)]; skip || absolute == allowed {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		queries := string(content)
		locations := queryNamePattern.FindAllStringSubmatchIndex(queries, -1)
		for i, location := range locations {
			end := len(queries)
			if i+1 < len(locations) {
				end = locations[i+1][0]
			}
			name := queries[location[2]:location[3]]
			statement := lineCommentPattern.ReplaceAllString(queries[location[1]:end], "")
			checked += len(pairedPresencePattern.FindAllString(statement, -1))
			if unpaired := unpairedPresenceChecks(statement); len(unpaired) > 0 {
				offenders[path+": "+name] = unpaired
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, checked, "the scan reaches the queries that check whether a token is stored")
	require.Empty(t, offenders, "these queries check whether a value is stored by one copy; test both, as (x_encrypted IS NOT NULL OR x_org_encrypted IS NOT NULL) or (x_encrypted IS NULL AND x_org_encrypted IS NULL)")
}

func TestUnpairedPresenceChecks(t *testing.T) {
	t.Parallel()

	require.Empty(t, unpairedPresenceChecks("WHERE (s.refresh_token_encrypted IS NOT NULL OR s.refresh_token_org_encrypted IS NOT NULL) AND (s.nonce_encrypted IS NULL AND s.nonce_org_encrypted IS NULL)"))
	require.Equal(t, []string{"s.refresh_token_encrypted IS NOT NULL"}, unpairedPresenceChecks("WHERE s.refresh_token_encrypted IS NOT NULL AND s.deleted IS FALSE"))
	require.Equal(t, []string{"c.access_token_encrypted IS NULL"}, unpairedPresenceChecks("WHERE c.access_token_encrypted IS NULL"))
	require.Empty(t, unpairedPresenceChecks("WHERE c.client_secret_encrypted IS NULL"), "other values are not this use case's")
}

// copyReadPattern matches Go code reading the text or presence of one stored
// token copy directly.
var copyReadPattern = regexp.MustCompile(`\b(?:AccessToken|RefreshToken|IdentityAssertion|UpstreamSubject|Nonce)(?:Org)?Encrypted\.(?:String|Valid)\b`)

// TestProductionCodeNeverReadsOneTokenCopy keeps production code from
// deciding anything from one copy of a stored token. Whether a value is set is
// Has over both copies, and its plaintext is Open with the copy the
// organization's transition state reads. Code passes the copies to Stored and
// leaves the rest to Tokens.
func TestProductionCodeNeverReadsOneTokenCopy(t *testing.T) {
	t.Parallel()

	self, err := filepath.Abs(".")
	require.NoError(t, err)

	offenders := map[string][]string{}
	err = filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "repo", "testrepo":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if filepath.Dir(absolute) == self {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if matches := copyReadPattern.FindAllString(string(content), -1); len(matches) > 0 {
			offenders[path] = matches
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "this code reads one copy of a stored token; use upstreamtokens.Has or Tokens.Open over both copies")
}

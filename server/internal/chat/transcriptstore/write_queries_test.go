package transcriptstore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sqlcPackages maps each SQL file that writes chat messages or their
// transcripts to the Go package sqlc generates its queries into.
var sqlcPackages = map[string]string{
	"internal/chat/queries.sql":                          "github.com/speakeasy-api/gram/server/internal/chat/repo",
	"internal/chat/transcriptstore/queries.sql":          "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo",
	"internal/chat/transcriptstore/fixtures/queries.sql": "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo",
	"internal/risk/queries.sql":                          "github.com/speakeasy-api/gram/server/internal/risk/repo",
	"internal/testenv/queries.sql":                       "github.com/speakeasy-api/gram/server/internal/testenv/testrepo",
}

// The demo seed loads plaintext rows for the demo org with raw SQL. It never
// runs through the app's writers, so it is checked by directory, not by name.
const demoSeedDir = "internal/demoseed/queries/"

var (
	insertsMessageTables = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(chat_messages|chat_message_transcripts|chat_message_tool_call_ids)\b`)
	deletesSideTables    = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(chat_message_transcripts|chat_message_tool_call_ids)\b`)
	updatesMessageTables = regexp.MustCompile(`(?is)\bUPDATE\s+(chat_messages|chat_message_transcripts)\b(.*?)(;|$)`)
	setsProtectedField   = regexp.MustCompile(`(?i)\b(content|content_raw|tool_calls|tool_outcome_notes|tool_call_summaries|storage_error)\s*=`)
)

// Every query that writes chat messages, their transcripts or their tool-call
// IDs must be guarded by glint's no-direct-chat-message-insert rule, so only
// the package that seals transcripts can call it. A new write query fails here
// until it is added to chatMessageWriteQueries in
// glint/no_direct_chat_message_insert.go.
func TestEveryChatMessageWriteQueryIsGuarded(t *testing.T) {
	t.Parallel()

	// go test runs in the package directory, server/internal/chat/transcriptstore.
	wd, err := os.Getwd()
	require.NoError(t, err)
	server := filepath.Join(wd, "..", "..", "..")
	guarded := glintGuardedQueries(t, filepath.Join(server, "..", "glint", "no_direct_chat_message_insert.go"))

	found := 0
	err = filepath.WalkDir(filepath.Join(server, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return err
		}
		rel, err := filepath.Rel(server, path)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, name := range writeQueries(string(raw)) {
			found++
			if strings.HasPrefix(rel, demoSeedDir) {
				continue
			}
			pkg, ok := sqlcPackages[rel]
			require.True(t, ok, "%s: query %s writes chat messages; map %s to its sqlc package here and guard the query in glint", rel, name, rel)
			require.True(t, guarded[pkg][name], "%s: query %s writes chat messages; add it to chatMessageWriteQueries in glint/no_direct_chat_message_insert.go", rel, name)
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, found, "the scan found no write queries; the patterns no longer match the SQL")
}

// writeQueries returns the names of the sqlc queries in a file that insert
// into chat_messages or its side tables, delete from the side tables, or
// update a protected field or a transcript.
func writeQueries(sql string) []string {
	var names []string
	// The leading newline lets a file's first query split like the rest.
	for _, block := range strings.Split("\n"+sql, "\n-- name: ")[1:] {
		fields := strings.Fields(block)
		if len(fields) == 0 {
			continue
		}
		var body strings.Builder
		for line := range strings.SplitSeq(block, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			body.WriteString(line + "\n")
		}
		if writesChatMessages(body.String()) {
			names = append(names, fields[0])
		}
	}
	return names
}

func writesChatMessages(body string) bool {
	if insertsMessageTables.MatchString(body) || deletesSideTables.MatchString(body) {
		return true
	}
	for _, m := range updatesMessageTables.FindAllStringSubmatch(body, -1) {
		if strings.EqualFold(m[1], "chat_message_transcripts") || setsProtectedField.MatchString(m[2]) {
			return true
		}
	}
	return false
}

// glintGuardedQueries reads chatMessageWriteQueries from the glint rule's
// source. glint is a separate module that cannot be imported here.
func glintGuardedQueries(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)

	consts := map[string]string{}
	var table *ast.CompositeLit
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}
				if lit, ok := value.Values[i].(*ast.BasicLit); ok && gen.Tok == token.CONST {
					s, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					consts[name.Name] = s
				}
				if lit, ok := value.Values[i].(*ast.CompositeLit); ok && name.Name == "chatMessageWriteQueries" {
					table = lit
				}
			}
		}
	}
	require.NotNil(t, table, "chatMessageWriteQueries not found in %s", path)

	guarded := map[string]map[string]bool{}
	for _, elt := range table.Elts {
		entry, ok := elt.(*ast.KeyValueExpr)
		require.True(t, ok)
		key, ok := entry.Key.(*ast.Ident)
		require.True(t, ok)
		pkg := consts[key.Name]
		require.NotEmpty(t, pkg, "unknown package constant %s", key.Name)
		guarded[pkg] = map[string]bool{}
		ast.Inspect(entry.Value, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				name, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				guarded[pkg][name] = true
			}
			return true
		})
	}
	return guarded
}

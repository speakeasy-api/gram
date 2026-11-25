package glint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noPlaintextTranscriptReadAnalyzer = "noplaintexttranscriptread"
	noPlaintextTranscriptReadDoc      = "only the transcript store may call sqlc queries that read the plaintext transcript columns of chat_messages; it opens encrypted shadows for projects that keep them"
)

type noPlaintextTranscriptReadSettings struct {
	Disabled bool `json:"disabled"`
}

// plaintextTranscriptColumns are the chat_messages columns that the encrypted
// transcript shadow (chat_message_transcripts.ciphertext) covers. They match
// the source hash in server/internal/chat/transcriptstore/queries.sql.
var plaintextTranscriptColumns = []string{
	"content",
	"content_raw",
	"tool_calls",
	"tool_outcome_notes",
	"tool_call_summaries",
	"storage_error",
}

// plaintextTranscriptStorePath is the one package that may read the plaintext
// transcript columns: it opens encrypted shadows for projects that keep them
// and falls back to plaintext for the rest. Its queries live in its internal
// package, so the compiler already stops other packages from calling them;
// this rule catches a new query added anywhere else.
const plaintextTranscriptStorePath = "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"

// sanctionedPlaintextTranscriptReaders may read the plaintext transcript
// columns outside the transcript store. Keys are types.Func.FullName values.
// Every entry needs a reason a reviewer can check. Keep it empty.
var sanctionedPlaintextTranscriptReaders = map[string]string{}

// pendingPlaintextTranscriptReaders read plaintext and have not moved behind
// the transcript store yet. Remove an entry when its reader moves, and never
// add one. Keep it empty.
var pendingPlaintextTranscriptReaders = map[string]string{}

type noPlaintextTranscriptReadAllowlist struct {
	sanctioned map[string]string
	pending    map[string]string
}

func (a noPlaintextTranscriptReadAllowlist) allowed(fullName string) bool {
	if _, ok := a.sanctioned[fullName]; ok {
		return true
	}
	_, ok := a.pending[fullName]
	return ok
}

func newNoPlaintextTranscriptReadAnalyzer(rule noPlaintextTranscriptReadSettings) *analysis.Analyzer {
	return newNoPlaintextTranscriptReadAnalyzerWith(rule, noPlaintextTranscriptReadAllowlist{
		sanctioned: sanctionedPlaintextTranscriptReaders,
		pending:    pendingPlaintextTranscriptReaders,
	})
}

func newNoPlaintextTranscriptReadAnalyzerWith(_ noPlaintextTranscriptReadSettings, allow noPlaintextTranscriptReadAllowlist) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noPlaintextTranscriptReadAnalyzer,
		Doc:      noPlaintextTranscriptReadDoc,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			if pass.Pkg.Path() == plaintextTranscriptStorePath || strings.HasPrefix(pass.Pkg.Path(), plaintextTranscriptStorePath+"/") {
				return nil, nil
			}

			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			queries := map[string]map[string]bool{}
			readers := map[string]bool{}

			ins.WithStack([]ast.Node{(*ast.SelectorExpr)(nil)}, func(node ast.Node, push bool, stack []ast.Node) bool {
				if !push {
					return true
				}
				sel := node.(*ast.SelectorExpr)
				if strings.HasSuffix(pass.Fset.Position(sel.Pos()).Filename, "_test.go") {
					return true
				}

				called, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
				if !ok || !isSqlcQueriesMethod(called) {
					return true
				}
				if !sqlcQueryReadsPlaintextTranscript(pass.Fset, called, queries) {
					return true
				}

				caller := enclosingFuncName(pass, stack)
				if caller != "" {
					readers[caller] = true
				}
				if allow.allowed(caller) {
					return true
				}

				pass.ReportRangef(sel, "do not call %q here: it reads the plaintext transcript columns of chat_messages; move the query into transcriptstore and read through transcriptstore.Transcripts (caller %q)", called.Name(), callerOrPackage(caller, pass))
				return true
			})

			reportStaleAllowlistEntries(pass, allow, readers)
			return nil, nil
		},
	}
}

// reportStaleAllowlistEntries keeps the allowlist honest: an entry for a
// function in this package that no longer reads plaintext must go.
func reportStaleAllowlistEntries(pass *analysis.Pass, allow noPlaintextTranscriptReadAllowlist, readers map[string]bool) {
	decls := map[string]*ast.FuncDecl{}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			decls[obj.FullName()] = fn
		}
	}
	if len(decls) == 0 {
		return
	}

	names := make([]string, 0, len(allow.sanctioned)+len(allow.pending))
	for name := range allow.sanctioned {
		names = append(names, name)
	}
	for name := range allow.pending {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if funcPackagePath(name) != pass.Pkg.Path() || readers[name] {
			continue
		}
		if fn, ok := decls[name]; ok {
			pass.ReportRangef(fn.Name, "remove %q from the plaintext transcript reader allowlist in glint: it no longer reads plaintext transcript columns", name)
			continue
		}
		pass.Reportf(pass.Files[0].Package, "remove %q from the plaintext transcript reader allowlist in glint: the function no longer exists", name)
	}
}

// funcPackagePath returns the package path of a types.Func.FullName value:
// "pkg.Func", "(pkg.T).M" or "(*pkg.T).M".
func funcPackagePath(fullName string) string {
	name := fullName
	if strings.HasPrefix(name, "(") {
		end := strings.Index(name, ")")
		if end < 0 {
			return ""
		}
		name = strings.TrimPrefix(name[1:end], "*")
	}
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return ""
	}
	return name[:dot]
}

func callerOrPackage(caller string, pass *analysis.Pass) string {
	if caller != "" {
		return caller
	}
	return pass.Pkg.Path()
}

// enclosingFuncName returns the full name of the declared function around the
// node, so calls inside closures belong to the function that holds them.
func enclosingFuncName(pass *analysis.Pass, stack []ast.Node) string {
	for _, s := range slices.Backward(stack) {
		fn, ok := s.(*ast.FuncDecl)
		if !ok {
			continue
		}
		obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
		if !ok {
			return ""
		}
		return obj.FullName()
	}
	return ""
}

// isSqlcQueriesMethod reports whether fn is a method on a sqlc Queries type.
func isSqlcQueriesMethod(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	return ok && named.Obj().Name() == "Queries"
}

// sqlcQueryReadsPlaintextTranscript reads the SQL that sqlc generated for the
// method. The signal lives only in SQL text, which has no type-system
// representation, so this parses the generated file (cached per file).
func sqlcQueryReadsPlaintextTranscript(fset *token.FileSet, fn *types.Func, cache map[string]map[string]bool) bool {
	filename := fset.Position(fn.Pos()).Filename
	if filename == "" {
		return false
	}
	byName, ok := cache[filename]
	if !ok {
		byName = parseSqlcQueries(filename)
		cache[filename] = byName
	}
	return byName[fn.Name()]
}

var sqlcQueryHeader = regexp.MustCompile(`^-- name: (\w+) :`)

// parseSqlcQueries maps each query name in a sqlc-generated file to whether
// its SQL reads plaintext transcript columns.
func parseSqlcQueries(filename string) map[string]bool {
	result := map[string]bool{}
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ParseComments)
	if err != nil || !ast.IsGenerated(file) {
		return result
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, expr := range value.Values {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				sql, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				m := sqlcQueryHeader.FindStringSubmatch(sql)
				if m == nil {
					continue
				}
				result[m[1]] = sqlReadsPlaintextTranscript(sql)
			}
		}
	}
	return result
}

var (
	sqlChatMessagesRef = regexp.MustCompile(`\bchat_messages\b`)
	sqlChatMessagesAs  = regexp.MustCompile(`\bchat_messages\s+(?:as\s+)?([a-z_][a-z0-9_]*)`)
	sqlInsertColumns   = regexp.MustCompile(`\binsert\s+into\s+[a-z0-9_.]+\s*(?:as\s+[a-z_][a-z0-9_]*\s*)?\([^)]*\)`)
	sqlSetClause       = regexp.MustCompile(`\bset\b(?s:(.*?))(\bwhere\b|\bfrom\b|\breturning\b|;|$)`)
	sqlSetTarget       = regexp.MustCompile(`^\s*[a-z_][a-z0-9_]*\s*=`)
	sqlColumnRef       = regexp.MustCompile(`(\bas\s+)?(?:([a-z_][a-z0-9_]*)\s*\.\s*)?\b(` + strings.Join(plaintextTranscriptColumns, "|") + `)\b`)
	// sqlQualifiedStar matches "alias.*"; sqlBareStar matches a bare "*" in a
	// select or returning list, but not count(*).
	sqlQualifiedStar = regexp.MustCompile(`\b([a-z_][a-z0-9_]*)\s*\.\s*\*`)
	sqlBareStar      = regexp.MustCompile(`(?:\bselect\s+(?:distinct\s+)?|\breturning\s+|,\s*)\*`)
)

// sqlKeywordsAfterTable are words that can follow a table name without being
// its alias.
var sqlKeywordsAfterTable = map[string]bool{
	"where": true, "join": true, "inner": true, "left": true, "right": true, "full": true,
	"cross": true, "natural": true, "on": true, "using": true, "set": true, "order": true,
	"group": true, "limit": true, "offset": true, "returning": true, "union": true,
	"for": true, "having": true, "window": true, "except": true, "intersect": true,
	"values": true, "default": true, "select": true, "lateral": true,
}

// sqlReadsPlaintextTranscript reports whether a query reads a plaintext
// transcript column of chat_messages: selecting, returning, filtering, joining
// or hashing it. Writing one (an INSERT column list or a SET target) is not a
// read. A column counts when it is qualified by a chat_messages alias or left
// unqualified in a query that touches chat_messages; a star counts the same
// way, since it selects every column.
//
// This is a lexical check, not a SQL parser. It does not follow a column
// through a derived table or CTE alias ("FROM (SELECT ... FROM chat_messages)
// t" then "t.content"); sqlc expands stars in the SQL it generates, and the
// compiler boundary of transcriptstore/internal is the real guard.
func sqlReadsPlaintextTranscript(sql string) bool {
	s := stripSQLCommentsAndStrings(strings.ToLower(sql))
	if !sqlChatMessagesRef.MatchString(s) {
		return false
	}

	aliases := map[string]bool{"chat_messages": true}
	for _, m := range sqlChatMessagesAs.FindAllStringSubmatch(s, -1) {
		if !sqlKeywordsAfterTable[m[1]] {
			aliases[m[1]] = true
		}
	}

	s = sqlInsertColumns.ReplaceAllString(s, " ")
	s = sqlSetClause.ReplaceAllStringFunc(s, stripSetTargets)

	if sqlBareStar.MatchString(s) {
		return true
	}
	for _, m := range sqlQualifiedStar.FindAllStringSubmatch(s, -1) {
		if aliases[m[1]] {
			return true
		}
	}

	for _, m := range sqlColumnRef.FindAllStringSubmatch(s, -1) {
		if m[1] != "" && m[2] == "" {
			// "AS content" names an output column; it is not a reference.
			continue
		}
		if m[2] == "" || aliases[m[2]] {
			return true
		}
	}
	return false
}

// stripSetTargets blanks the target of each assignment in a SET clause, so
// "SET content = $1" is not a read, while a column on the right-hand side
// ("SET x = CASE WHEN content = ” ...") still is.
func stripSetTargets(clause string) string {
	body := strings.TrimPrefix(clause, "set")
	parts := splitTopLevel(body, ',')
	for i, part := range parts {
		parts[i] = sqlSetTarget.ReplaceAllString(part, " ")
	}
	return "set" + strings.Join(parts, ",")
}

// splitTopLevel splits s at sep outside parentheses.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// stripSQLCommentsAndStrings walks the query once, left to right: comments
// become a space, string literals become ”, and double-quoted identifiers
// lose their quotes. One pass means a quote inside a comment, or "--" inside
// a string, cannot hide the text after it.
func stripSQLCommentsAndStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "--"):
			i = lineCommentEnd(s, i)
			b.WriteByte(' ')
		case strings.HasPrefix(s[i:], "/*"):
			i = blockCommentEnd(s, i)
			b.WriteByte(' ')
		case s[i] == '\'':
			i = stringLiteralEnd(s, i, isEscapeStringPrefix(s, i))
			b.WriteString("''")
		case s[i] == '"':
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				b.WriteString(s[i+1:])
				i = len(s)
				continue
			}
			b.WriteString(s[i+1 : i+1+end])
			i += end + 2
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

func lineCommentEnd(s string, i int) int {
	end := strings.IndexByte(s[i:], '\n')
	if end < 0 {
		return len(s)
	}
	return i + end
}

// blockCommentEnd returns the index after the comment that opens at i.
// PostgreSQL block comments nest.
func blockCommentEnd(s string, i int) int {
	depth := 0
	for i < len(s) {
		if strings.HasPrefix(s[i:], "/*") {
			depth++
			i += 2
			continue
		}
		if strings.HasPrefix(s[i:], "*/") {
			depth--
			i += 2
			if depth == 0 {
				return i
			}
			continue
		}
		i++
	}
	return len(s)
}

// stringLiteralEnd returns the index after the literal that opens at i. A
// doubled quote is an escaped quote; in an E” string a backslash escapes
// the next character too.
func stringLiteralEnd(s string, i int, backslashEscapes bool) int {
	for j := i + 1; j < len(s); j++ {
		if backslashEscapes && s[j] == '\\' {
			j++
			continue
		}
		if s[j] != '\'' {
			continue
		}
		if j+1 < len(s) && s[j+1] == '\'' {
			j++
			continue
		}
		return j + 1
	}
	return len(s)
}

// isEscapeStringPrefix reports whether the quote at i opens an E” string.
func isEscapeStringPrefix(s string, i int) bool {
	if i == 0 || s[i-1] != 'e' {
		return false
	}
	if i == 1 {
		return true
	}
	c := s[i-2]
	return c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9')
}

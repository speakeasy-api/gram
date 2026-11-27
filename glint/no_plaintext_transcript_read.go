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
	noPlaintextTranscriptReadDoc      = "only the transcript store may read, update or insert the plaintext transcript columns of chat_messages; it opens encrypted transcripts on read, writes every copy of a body through Transcripts.UpdateChatMessage, and the chat message writer seals inserts"
)

type noPlaintextTranscriptReadSettings struct {
	Disabled bool `json:"disabled"`
}

// plaintextTranscriptColumns are the chat_messages columns that the encrypted
// transcript (chat_message_transcripts.ciphertext) covers: the message body
// that transcriptcrypto.Message holds.
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

// sanctionedPlaintextTranscriptReaders may read or insert the plaintext
// transcript columns outside the transcript store. Keys are types.Func.FullName
// values. Every entry needs a reason a reviewer can check: demo seed data for
// projects that never keep encrypted transcripts.
var sanctionedPlaintextTranscriptReaders = map[string]string{
	"(*github.com/speakeasy-api/gram/server/internal/demoseed.seeder).activityInsertMessages":  "demo seed data for the demo organization, whose projects never keep encrypted transcripts",
	"(*github.com/speakeasy-api/gram/server/internal/demoseed.seeder).seedSignalConversations": "demo seed data for the demo organization, whose projects never keep encrypted transcripts",
}

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
			queries := map[string]map[string]plaintextTranscriptUse{}
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
				use := sqlcQueryPlaintextTranscriptUse(pass.Fset, called, queries)
				if use == plaintextTranscriptUnused {
					return true
				}

				caller := enclosingFuncName(pass, stack)
				if caller != "" {
					readers[caller] = true
				}
				if allow.allowed(caller) {
					return true
				}

				if use == plaintextTranscriptUpdate {
					pass.ReportRangef(sel, "do not call %q here: it updates the plaintext transcript columns of chat_messages; change the message through transcriptstore.Transcripts.UpdateChatMessage, which writes every copy (caller %q)", called.Name(), callerOrPackage(caller, pass))
					return true
				}
				if use == plaintextTranscriptInsert {
					pass.ReportRangef(sel, "do not call %q here: it inserts the plaintext transcript columns of chat_messages; insert messages through the chat message writer, which seals them in the same transaction (caller %q)", called.Name(), callerOrPackage(caller, pass))
					return true
				}
				pass.ReportRangef(sel, "do not call %q here: it reads the plaintext transcript columns of chat_messages; move the query into transcriptstore and read through transcriptstore.Transcripts (caller %q)", called.Name(), callerOrPackage(caller, pass))
				return true
			})

			reportRawPlaintextTranscriptWrites(pass, ins, allow, readers)
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
// plaintextTranscriptUse is how a sqlc query touches the plaintext transcript
// columns of chat_messages.
type plaintextTranscriptUse int

const (
	plaintextTranscriptUnused plaintextTranscriptUse = iota
	plaintextTranscriptRead
	plaintextTranscriptUpdate
	plaintextTranscriptInsert
)

func sqlcQueryPlaintextTranscriptUse(fset *token.FileSet, fn *types.Func, cache map[string]map[string]plaintextTranscriptUse) plaintextTranscriptUse {
	filename := fset.Position(fn.Pos()).Filename
	if filename == "" {
		return plaintextTranscriptUnused
	}
	byName, ok := cache[filename]
	if !ok {
		byName = parseSqlcQueries(filename)
		cache[filename] = byName
	}
	return byName[fn.Name()]
}

var sqlcQueryHeader = regexp.MustCompile(`^-- name: (\w+) :`)

// parseSqlcQueries maps each query name in a sqlc-generated file to how its
// SQL touches plaintext transcript columns. A read wins over an update, and an
// update over an insert.
func parseSqlcQueries(filename string) map[string]plaintextTranscriptUse {
	result := map[string]plaintextTranscriptUse{}
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
				switch {
				case sqlReadsPlaintextTranscript(sql):
					result[m[1]] = plaintextTranscriptRead
				case sqlUpdatesPlaintextTranscript(sql):
					result[m[1]] = plaintextTranscriptUpdate
				case sqlInsertsPlaintextTranscript(sql):
					result[m[1]] = plaintextTranscriptInsert
				default:
					result[m[1]] = plaintextTranscriptUnused
				}
			}
		}
	}
	return result
}

var (
	sqlChatMessagesRef = regexp.MustCompile(`\bchat_messages\b`)
	sqlChatMessagesAs  = regexp.MustCompile(`\bchat_messages\s+(?:as\s+)?([a-z_][a-z0-9_]*)`)
	sqlInsertColumns   = regexp.MustCompile(`\binsert\s+into\s+[a-z0-9_.]+\s*(?:as\s+[a-z_][a-z0-9_]*\s*)?\([^)]*\)`)
	sqlSetKeyword      = regexp.MustCompile(`\bset\b`)
	sqlUpdateTarget    = regexp.MustCompile(`\bupdate\s+(?:only\s+)?chat_messages\b`)
	sqlUpsertTarget    = regexp.MustCompile(`\binsert\s+into\s+chat_messages\b[\s\S]*\bon\s+conflict\b[\s\S]*\bdo\s+update\b`)
	sqlInsertBodyList  = regexp.MustCompile(`\binsert\s+into\s+chat_messages\s*\(([^)]*)\)`)
	sqlBodyColumn      = regexp.MustCompile(`\b(` + strings.Join(plaintextTranscriptColumns, "|") + `)\b`)
	sqlBodySetTarget   = regexp.MustCompile(`(?:^|,)\s*(` + strings.Join(plaintextTranscriptColumns, "|") + `)\s*=`)
	sqlSetTerminator   = regexp.MustCompile(`^(where|from|returning)\b`)
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
	s = stripSetClauses(s)

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

// sqlInsertsPlaintextTranscript reports whether a query inserts a plaintext
// transcript column of chat_messages: its INSERT column list names one. A new
// message must be sealed in the same transaction, which only the chat message
// writer does.
func sqlInsertsPlaintextTranscript(sql string) bool {
	s := stripSQLCommentsAndStrings(strings.ToLower(strings.ReplaceAll(sql, `"`, "")))
	m := sqlInsertBodyList.FindStringSubmatch(s)
	return m != nil && sqlBodyColumn.MatchString(m[1])
}

// sqlUpdatesPlaintextTranscript reports whether a query updates a plaintext
// transcript column of chat_messages: an UPDATE of chat_messages, or an INSERT
// into it with ON CONFLICT DO UPDATE, whose SET clause assigns one. Every body
// change goes through Transcripts.UpdateChatMessage, which writes every copy.
func sqlUpdatesPlaintextTranscript(sql string) bool {
	s := stripSQLCommentsAndStrings(strings.ToLower(sql))
	if !sqlUpdateTarget.MatchString(s) && !sqlUpsertTarget.MatchString(s) {
		return false
	}
	rest := s
	for {
		loc := sqlSetKeyword.FindStringIndex(rest)
		if loc == nil {
			return false
		}
		end := setClauseEnd(rest, loc[1])
		if sqlBodySetTarget.MatchString(rest[loc[1]:end]) {
			return true
		}
		rest = rest[end:]
	}
}

// stripSetClauses runs stripSetTargets over each SET clause. A clause ends at
// the first WHERE, FROM, RETURNING or ";" outside parentheses, so a subquery
// on an assignment's right-hand side does not end it early.
func stripSetClauses(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	rest := s
	for {
		loc := sqlSetKeyword.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:loc[0]])
		end := setClauseEnd(rest, loc[1])
		b.WriteString(stripSetTargets(rest[loc[0]:end]))
		rest = rest[end:]
	}
}

// setClauseEnd returns where the SET clause starting at from ends.
func setClauseEnd(s string, from int) int {
	depth := 0
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
			continue
		case ')':
			depth--
			continue
		case ';':
			if depth == 0 {
				return i
			}
			continue
		}
		if depth != 0 || (i > 0 && isIdentByte(s[i-1])) {
			continue
		}
		if sqlSetTerminator.MatchString(s[i:]) {
			return i
		}
	}
	return len(s)
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
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

// reportRawPlaintextTranscriptWrites flags SQL written as a Go string outside
// sqlc that updates a plaintext transcript column of chat_messages, or inserts
// one outside a sanctioned caller, which the sqlc call check cannot see.
// Generated sqlc files are left to that check, and tests may set up stale rows
// on purpose.
func reportRawPlaintextTranscriptWrites(pass *analysis.Pass, ins *inspector.Inspector, allow noPlaintextTranscriptReadAllowlist, readers map[string]bool) {
	ins.WithStack([]ast.Node{(*ast.BasicLit)(nil)}, func(node ast.Node, push bool, stack []ast.Node) bool {
		if !push {
			return true
		}
		lit := node.(*ast.BasicLit)
		if lit.Kind != token.STRING {
			return true
		}
		filename := pass.Fset.Position(lit.Pos()).Filename
		if strings.HasSuffix(filename, "_test.go") || strings.HasSuffix(filename, ".sql.go") {
			return true
		}
		sql, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if sqlUpdatesPlaintextTranscript(sql) {
			pass.ReportRangef(lit, "this SQL updates the plaintext transcript columns of chat_messages; change the message through transcriptstore.Transcripts.UpdateChatMessage, which writes every copy")
			return true
		}
		if !sqlInsertsPlaintextTranscript(sql) {
			return true
		}
		caller := enclosingFuncName(pass, stack)
		if caller != "" {
			readers[caller] = true
		}
		if allow.allowed(caller) {
			return true
		}
		pass.ReportRangef(lit, "this SQL inserts the plaintext transcript columns of chat_messages; insert messages through the chat message writer, which seals them in the same transaction (caller %q)", callerOrPackage(caller, pass))
		return true
	})
}

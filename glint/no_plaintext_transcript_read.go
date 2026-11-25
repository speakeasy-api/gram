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
	sqlLineComment     = regexp.MustCompile(`--[^\n]*`)
	sqlStringLiteral   = regexp.MustCompile(`'(?:[^']|'')*'`)
	sqlChatMessagesRef = regexp.MustCompile(`\bchat_messages\b`)
	sqlChatMessagesAs  = regexp.MustCompile(`\bchat_messages\s+(?:as\s+)?([a-z_][a-z0-9_]*)`)
	sqlInsertColumns   = regexp.MustCompile(`\binsert\s+into\s+[a-z0-9_."]+\s*(?:as\s+[a-z_][a-z0-9_]*\s*)?\([^)]*\)`)
	sqlSetClause       = regexp.MustCompile(`\bset\b(?s:(.*?))(\bwhere\b|\bfrom\b|\breturning\b|;|$)`)
	sqlSetTarget       = regexp.MustCompile(`([a-z_][a-z0-9_]*)\s*=`)
	sqlColumnRef       = regexp.MustCompile(`(\bas\s+)?(?:([a-z_][a-z0-9_]*)\s*\.\s*)?\b(` + strings.Join(plaintextTranscriptColumns, "|") + `)\b`)
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
// unqualified in a query that touches chat_messages.
func sqlReadsPlaintextTranscript(sql string) bool {
	s := strings.ToLower(sql)
	s = sqlLineComment.ReplaceAllString(s, " ")
	s = sqlStringLiteral.ReplaceAllString(s, "''")
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
	s = sqlSetClause.ReplaceAllStringFunc(s, func(clause string) string {
		return sqlSetTarget.ReplaceAllString(clause, " ")
	})

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

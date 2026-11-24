package glint

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noPublicSchemaSqlAnalyzer       = "nopublicschemasql"
	noPublicSchemaSqlDefaultMessage = "drop the public. schema prefix and let search_path resolve the table: deployed databases keep tables in the gram schema, so a public. name finds nothing in production"
)

// noPublicSchemaSql guards against SQL that names a table as public.<table>.
// Every deployed database keeps application tables in the gram schema and
// connects with search_path=gram, while local dev uses public. A public.
// name therefore works locally and in older test setups but finds no table in
// production: a startup check written that way crash-looped prod. Unqualified
// names resolve through search_path in every environment.

// publicSchemaRef matches a public.<identifier> reference that is not part of
// a longer dotted or hyphenated name such as a host name or URL. The schema
// only exists in SQL text, so there is no type to check: matching string
// literals is the only signal. It does not match Go selectors such as
// public.field, which are identifiers, not string literals.
var publicSchemaRef = regexp.MustCompile(`(?i)(?:^|[^\w./:@-])public\.([a-z_][a-z0-9_]*)(?:$|[^\w.-])`)

type noPublicSchemaSqlSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoPublicSchemaSqlAnalyzer(_ noPublicSchemaSqlSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noPublicSchemaSqlAnalyzer,
		Doc:      noPublicSchemaSqlDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.BasicLit)(nil)}, func(node ast.Node) {
				lit := node.(*ast.BasicLit)
				if lit.Kind != token.STRING {
					return
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return
				}
				match := publicSchemaRef.FindStringSubmatch(value)
				if match == nil {
					return
				}
				pass.ReportRangef(lit, "drop the public. schema prefix from %q and let search_path resolve it: deployed databases keep tables in the gram schema, so a public. name finds nothing in production", "public."+match[1])
			})

			return nil, nil
		},
	}
}

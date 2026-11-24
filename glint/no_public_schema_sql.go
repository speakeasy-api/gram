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
	noPublicSchemaSqlDefaultMessage = "drop the public. schema prefix and let search_path resolve the table; deployed databases keep tables in the gram schema"
)

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
				pass.ReportRangef(lit, "drop the public. schema prefix from %q and let search_path resolve it; deployed databases keep tables in the gram schema", "public."+match[1])
			})

			return nil, nil
		},
	}
}

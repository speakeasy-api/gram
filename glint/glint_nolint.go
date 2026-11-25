package glint

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	glintNolintAnalyzer = "glintnolint"
	glintNolintDoc      = "scope //nolint:glint directives to the offending line and name the suppressed glint analyzer in the explanation"
)

// newGlintNolintAnalyzer polices //nolint directives that list the glint
// linter. Because golangci-lint treats the whole glint plugin as a single
// linter, one //nolint:glint silences every glint analyzer, so this analyzer
// runs from the separate glintnolint plugin where that directive cannot
// suppress its own diagnostics.
//
// Directives are recognized the way golangci-lint v2 recognizes them: the
// comment text has leading slashes and spaces trimmed (so "// nolint:glint" is
// a directive too), must start with "nolint:", and the comma-separated linter
// list ends at the next "//". Everything after that "//" is the explanation.
//
// Two problems are reported:
//
//   - Placement: golangci-lint widens a directive whose comment group sits
//     directly above a node (next line, same column) to that whole node. A
//     directive in the file header before the package clause, or directly
//     above a top-level func, const, var, type, or import declaration,
//     therefore hides every glint rule for the file or declaration. Trailing
//     directives and own-line directives above a statement or spec inside a
//     declaration stay allowed.
//   - Explanation: the explanation must follow the grammar
//     "<analyzer>[,<analyzer>...]: <reason>", where each <analyzer> is the
//     Name of an analyzer built by the glint plugin (e.g. "notestingrawsql")
//     and <reason> is non-empty free text.
//
// Generated files are skipped. Directives that do not list glint (including
// ones listing only glintnolint) are ignored.
func newGlintNolintAnalyzer() (*analysis.Analyzer, error) {
	// Zero-value settings enable every glint analyzer, so analyzers added to
	// the glint plugin later become valid explanation scopes automatically.
	glintAnalyzers, err := (&plugin{}).BuildAnalyzers()
	if err != nil {
		return nil, fmt.Errorf("build glint analyzers: %w", err)
	}

	validNames := make(map[string]struct{}, len(glintAnalyzers))
	sortedNames := make([]string, 0, len(glintAnalyzers))
	for _, a := range glintAnalyzers {
		validNames[a.Name] = struct{}{}
		sortedNames = append(sortedNames, a.Name)
	}
	slices.Sort(sortedNames)
	nameList := strings.Join(sortedNames, ", ")

	return &analysis.Analyzer{
		Name: glintNolintAnalyzer,
		Doc:  glintNolintDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			// Directive placement is judged against each file's package clause
			// and top-level declarations only, so a per-file walk over
			// f.Comments and f.Decls is the natural unit; the shared inspector
			// would add nothing.
			for _, f := range pass.Files {
				if ast.IsGenerated(f) {
					continue
				}

				for _, group := range f.Comments {
					for _, c := range group.List {
						explanation, ok := parseGlintNolintDirective(c.Text)
						if !ok {
							continue
						}

						reportGlintNolintPlacement(pass, f, group, c)
						if msg := glintNolintExplanationProblem(explanation, validNames, nameList); msg != "" {
							pass.ReportRangef(c, "%s", msg)
						}
					}
				}
			}

			return nil, nil
		},
	}, nil
}

// parseGlintNolintDirective reports whether text is a //nolint directive whose
// linter list includes glint and returns its trimmed explanation (the text
// after the second "//"), which is empty when absent.
func parseGlintNolintDirective(text string) (string, bool) {
	if !strings.HasPrefix(text, "//") {
		return "", false
	}

	text = strings.TrimLeft(text, "/ ")
	if !strings.HasPrefix(text, "nolint:") || strings.HasPrefix(text, "nolint:all") {
		return "", false
	}

	linters, explanation, _ := strings.Cut(strings.TrimPrefix(text, "nolint:"), "//")

	for item := range strings.SplitSeq(linters, ",") {
		if strings.ToLower(strings.TrimSpace(item)) == pluginName {
			return strings.TrimSpace(explanation), true
		}
	}

	return "", false
}

func reportGlintNolintPlacement(pass *analysis.Pass, f *ast.File, group *ast.CommentGroup, c *ast.Comment) {
	if c.Pos() < f.Package {
		pass.ReportRangef(c, "move //nolint:glint from the file header onto the specific offending line(s); before the package clause it suppresses every glint rule for the whole file")
		return
	}

	// Mirror golangci-lint's range expansion: the directive's comment group
	// covers a node that starts on the line after the group ends and in the
	// same column the group starts in.
	groupEnd := pass.Fset.Position(group.End()).Line
	groupCol := pass.Fset.Position(group.Pos()).Column

	for _, decl := range f.Decls {
		declPos := pass.Fset.Position(decl.Pos())
		if declPos.Line != groupEnd+1 || declPos.Column != groupCol {
			continue
		}

		kind, name := describeTopLevelDecl(decl)
		pass.ReportRangef(c, "move //nolint:glint off top-level %s %q onto the specific offending line(s); directly above a top-level declaration it suppresses every glint rule for the whole declaration", kind, name)

		return
	}
}

func describeTopLevelDecl(decl ast.Decl) (string, string) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return "func", d.Name.Name
	case *ast.GenDecl:
		kind := d.Tok.String()
		if len(d.Specs) == 0 {
			return kind, ""
		}

		switch s := d.Specs[0].(type) {
		case *ast.ValueSpec:
			if len(s.Names) > 0 {
				return kind, s.Names[0].Name
			}
		case *ast.TypeSpec:
			return kind, s.Name.Name
		case *ast.ImportSpec:
			return kind, strings.Trim(s.Path.Value, "\"`")
		}

		return kind, ""
	default:
		return "declaration", ""
	}
}

// glintNolintExplanationProblem returns the diagnostic for an explanation that
// does not follow "<analyzer>[,<analyzer>...]: <reason>", or "" when it does.
func glintNolintExplanationProblem(explanation string, validNames map[string]struct{}, nameList string) string {
	missingScope := fmt.Sprintf("start the //nolint:glint explanation with the suppressed glint analyzer name(s) and a colon, e.g. \"//nolint:glint // notestingrawsql: <reason>\"; got %q", explanation)

	scope, reason, found := strings.Cut(explanation, ":")
	if !found {
		return missingScope
	}

	names := strings.Split(scope, ",")
	for i, name := range names {
		name = strings.TrimSpace(name)
		if !isGlintAnalyzerNameShape(name) {
			return missingScope
		}
		names[i] = name
	}

	for _, name := range names {
		if _, ok := validNames[name]; !ok {
			return fmt.Sprintf("%q in the //nolint:glint explanation is not a glint analyzer name; use one or more of: %s", name, nameList)
		}
	}

	if strings.TrimSpace(reason) == "" {
		return fmt.Sprintf("add a reason after %q in the //nolint:glint explanation", strings.Join(names, ",")+":")
	}

	return ""
}

// isGlintAnalyzerNameShape reports whether name looks like an analyzer name
// (lowercase letters and digits) rather than prose that happens to precede a
// colon.
func isGlintAnalyzerNameShape(name string) bool {
	if name == "" {
		return false
	}

	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}

	return true
}

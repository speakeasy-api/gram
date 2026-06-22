package celenv

// Completion is type-directed: instead of guessing what a dotted chain resolves
// to, the editor asks the engine. We lexically isolate the receiver expression
// (the thing left of the cursor's dot) and any enclosing comprehension bindings,
// then type-check the receiver in an env where each bound variable is declared
// with the real element type of the list it iterates. The receiver's resulting
// CEL type decides the offering — a field offers matchers, a list offers the
// comprehension macros, the tool object offers its members. No heuristic type
// table; the engine is the single source of truth here too.

import (
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// CompletionItem is one suggestion. Category lets the editor pick an icon and an
// insertion snippet without re-deriving what the item is.
type CompletionItem struct {
	Label    string `json:"label"`
	Detail   string `json:"detail"`
	Doc      string `json:"doc"`
	Category string `json:"category"` // matcher | macro | globalMacro | field | variable | bind
}

// Completion is the editor-facing answer for one cursor position.
type Completion struct {
	Context string           `json:"context"` // "member" (after a dot) | "name"
	Items   []CompletionItem `json:"items"`
}

// bind is one comprehension-bound variable and the source of the list it
// iterates, so the engine can resolve its element type rather than assume one.
type bind struct {
	name string
	iter string
}

var (
	// A comprehension header: `<list-expr>.<macro>(<bind>,`. The list-expr is a
	// dotted identifier chain (good enough; nested/parenthesised iterables fall
	// back to no binding, which only loses completion inside that rarer case).
	bindRe = regexp.MustCompile(`([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\.\s*(?:exists|exists_one|all|filter|map)\s*\(\s*([A-Za-z_]\w*)\s*,`)
	// A trailing primary expression: an identifier chain that may include simple
	// call segments like `.get("path")`. Anchored to the end so it captures the
	// receiver immediately left of the cursor.
	receiverRe = regexp.MustCompile(`[A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*(?:\s*\([^()]*\))?)*$`)
	trailingId = regexp.MustCompile(`[A-Za-z_]\w*$`)
)

// Complete returns the suggestions valid at the end of src (the text from the
// start of the expression up to the cursor).
func (e *Engine) Complete(src string) Completion {
	binds := scanBinds(src)

	// Strip the identifier fragment the author is mid-typing, then decide whether
	// we're completing a member (the remaining text ends in a dot) or a name.
	frag := trailingId.FindString(src)
	before := strings.TrimRight(src[:len(src)-len(frag)], " \t")

	if strings.HasSuffix(before, ".") {
		recv := receiverRe.FindString(strings.TrimRight(before[:len(before)-1], " \t"))
		return Completion{Context: "member", Items: e.memberItems(e.receiverKind(recv, binds))}
	}
	return Completion{Context: "name", Items: e.nameItems(binds)}
}

// scanBinds finds every comprehension binding in src, outermost first (source
// order), so an inner iterable that references an outer bind sees it declared.
func scanBinds(src string) []bind {
	var binds []bind
	for _, m := range bindRe.FindAllStringSubmatch(src, -1) {
		binds = append(binds, bind{name: m[2], iter: strings.TrimSpace(m[1])})
	}
	return binds
}

// receiverKind type-checks recv (with the comprehension binds declared) and maps
// its CEL type to the small vocabulary the editor offers against. Any failure —
// an unparseable fragment, an unknown reference — yields "field", the safe
// superset (matchers compose over everything), preserving completion on partial
// input.
func (e *Engine) receiverKind(recv string, binds []bind) string {
	if recv == "" {
		return ""
	}

	env := e.env
	for _, b := range binds {
		ast, iss := env.Compile(b.iter)
		if iss != nil && iss.Err() != nil {
			continue
		}
		lt := ast.OutputType()
		if lt.Kind() != types.ListKind || len(lt.Parameters()) != 1 {
			continue
		}
		if ext, err := env.Extend(cel.Variable(b.name, lt.Parameters()[0])); err == nil {
			env = ext
		}
	}

	ast, iss := env.Compile(recv)
	if iss != nil && iss.Err() != nil {
		return "field"
	}
	t := ast.OutputType()
	switch {
	case t.Kind() == types.ListKind:
		return "list"
	case t.TypeName() == toolTypeName:
		return "object"
	case t.TypeName() == fieldType.TypeName():
		return "field"
	default:
		return ""
	}
}

// memberItems is what completes after `<receiver>.` given the receiver's kind.
func (e *Engine) memberItems(kind string) []CompletionItem {
	ref := Describe()
	switch kind {
	case "list":
		// The comprehension macros that iterate a list (exists/all/...).
		var items []CompletionItem
		for _, m := range ref.Macros {
			if strings.HasPrefix(m.Signature, "list.") {
				items = append(items, CompletionItem{Label: m.Name, Detail: m.Signature, Doc: m.Description, Category: "macro"})
			}
		}
		return items
	case "object":
		// The tool object's members (name/server/function/args).
		var items []CompletionItem
		for _, f := range toolFields {
			items = append(items, CompletionItem{Label: f.Name, Detail: "field", Doc: f.Description, Category: "field"})
		}
		return items
	default:
		// A field (or an unresolved receiver): the matcher methods.
		items := make([]CompletionItem, 0, len(ref.Matchers))
		for _, f := range ref.Matchers {
			items = append(items, CompletionItem{Label: f.Name, Detail: f.Signature, Doc: f.Description, Category: "matcher"})
		}
		return items
	}
}

// nameItems is what completes at a name position (not after a dot): the
// variables, the in-scope comprehension binds, and the global macros (has).
func (e *Engine) nameItems(binds []bind) []CompletionItem {
	ref := Describe()
	items := make([]CompletionItem, 0, len(ref.Variables)+len(binds)+len(ref.Macros))
	for _, v := range ref.Variables {
		items = append(items, CompletionItem{Label: v.Name, Detail: v.Type, Doc: v.Description, Category: "variable"})
	}
	for _, b := range binds {
		items = append(items, CompletionItem{Label: b.name, Detail: "tool", Doc: "Iteration variable bound to a tool call.", Category: "bind"})
	}
	for _, m := range ref.Macros {
		if !strings.HasPrefix(m.Signature, "list.") {
			items = append(items, CompletionItem{Label: m.Name, Detail: m.Signature, Doc: m.Description, Category: "globalMacro"})
		}
	}
	return items
}

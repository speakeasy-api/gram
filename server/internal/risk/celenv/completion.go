package celenv

// Completion is type-directed: rather than guess what a dotted chain resolves to,
// the editor asks the engine. We isolate the receiver expression and the
// comprehension binds enclosing the cursor, type-check the receiver in an env
// where each bind is declared with the real element type of its list, and let
// the receiver's CEL type decide the offering.

import (
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

type CompletionItem struct {
	Label    string `json:"label"`
	Detail   string `json:"detail"`
	Doc      string `json:"doc"`
	Category string `json:"category"` // matcher | macro | globalMacro | field | variable | bind
}

type Completion struct {
	Context string           `json:"context"` // "member" (after a dot) | "name"
	Items   []CompletionItem `json:"items"`
}

// bind is a comprehension-bound variable and the list it iterates.
type bind struct {
	name string
	iter string
}

var (
	// A comprehension header `<list-expr>.<macro>(<bind>,`; list-expr is a dotted
	// identifier chain (parenthesised iterables fall back to no binding).
	bindRe = regexp.MustCompile(`([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\.\s*(?:exists|exists_one|all|filter|map)\s*\(\s*([A-Za-z_]\w*)\s*,`)
	// A trailing primary expression: an identifier chain that may include simple
	// call segments like `.get("path")`, anchored to the cursor.
	receiverRe = regexp.MustCompile(`[A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*(?:\s*\([^()]*\))?)*$`)
	trailingID = regexp.MustCompile(`[A-Za-z_]\w*$`)
)

// Complete returns the suggestions valid at the end of src (text up to the cursor).
func (e *Engine) Complete(src string) Completion {
	binds := scanBinds(src)

	frag := trailingID.FindString(src)
	before := strings.TrimRight(src[:len(src)-len(frag)], " \t")

	if strings.HasSuffix(before, ".") {
		recv := receiverRe.FindString(strings.TrimRight(before[:len(before)-1], " \t"))
		return Completion{Context: "member", Items: e.memberItems(e.receiverKind(recv, binds))}
	}
	return Completion{Context: "name", Items: e.nameItems(binds)}
}

// scanBinds returns the binds whose comprehension body encloses the cursor (its
// opening paren still unclosed), so a bind doesn't leak past its closing paren.
func scanBinds(src string) []bind {
	var binds []bind
	for _, m := range bindRe.FindAllStringSubmatchIndex(src, -1) {
		open := strings.IndexByte(src[m[3]:m[5]], '(')
		if open < 0 || parenDepth(src[m[3]+open:]) <= 0 {
			continue
		}
		binds = append(binds, bind{name: src[m[4]:m[5]], iter: strings.TrimSpace(src[m[2]:m[3]])})
	}
	return binds
}

func parenDepth(s string) int {
	d := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			d++
		case ')':
			d--
		}
	}
	return d
}

// receiverKind type-checks recv (with binds declared) and maps its CEL type to
// the offering vocabulary. A resolved non-field type yields "" (no members);
// only an unparseable fragment falls back to "field" to keep partial input live.
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
		return "field" // partial input: offer matchers for discoverability
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
		var items []CompletionItem
		for _, m := range ref.Macros {
			if strings.HasPrefix(m.Signature, "list.") {
				items = append(items, CompletionItem{Label: m.Name, Detail: m.Signature, Doc: m.Description, Category: "macro"})
			}
		}
		return items
	case "object":
		var items []CompletionItem
		for _, v := range ref.Variables {
			if v.Name != "tool_calls" {
				continue
			}
			for _, f := range v.Fields {
				items = append(items, CompletionItem{Label: f.Name, Detail: f.Type, Doc: f.Description, Category: "field"})
			}
		}
		return items
	case "field":
		items := make([]CompletionItem, 0, len(ref.Matchers))
		for _, f := range ref.Matchers {
			items = append(items, CompletionItem{Label: f.Name, Detail: f.Signature, Doc: f.Description, Category: "matcher"})
		}
		return items
	default:
		return nil // resolved non-field receiver (e.g. kind, a string): no members
	}
}

// nameItems completes at a name position: variables, in-scope binds, global macros.
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

package schema

import (
	"fmt"
	"strings"
)

// Render prints the compiled registry as a read-only text view, one block
// per type: its stored scopes, facts, guards and permissions.
// The view is documentation; nothing parses it.
func Render(r *Registry) string {
	var b strings.Builder
	b.WriteString("# Generated from the compiled authorization registry. Do not edit.\n")
	b.WriteString("# Grammar: a | b union, a - b minus, is(fact) the actor is the user a fact names,\n")
	b.WriteString("# via(rel).perm a permission on a related resource, when guard (expr) a guarded branch.\n")
	for _, t := range r.Types() {
		b.WriteString("\ntype " + t.Name + "\n")
		if t.Parent != "" {
			b.WriteString("  parent       " + t.Parent + "\n")
		}
		b.WriteString("  stored_kind  " + t.ResourceKind + "\n")

		var grants, blocks []ScopeDefinition
		surfaces := map[string]bool{}
		for _, sc := range r.Scopes() {
			switch {
			case sc.Type != t.Name:
				continue
			case sc.Block:
				blocks = append(blocks, sc)
			default:
				grants = append(grants, sc)
			}
			surfaces[sc.GrantSurface] = true
		}
		typeSurface := ""
		if len(surfaces) == 1 {
			for surface := range surfaces {
				typeSurface = surface
			}
		}
		if typeSurface != "" {
			b.WriteString("  surface      " + typeSurface + "\n")
		}
		if len(grants) > 0 {
			b.WriteString("\n")
		}
		for _, sc := range grants {
			b.WriteString("  grant " + sc.Name)
			if sc.Description != "" {
				fmt.Fprintf(&b, " %q", sc.Description)
			}
			b.WriteString(scopeAttributes(sc, typeSurface) + "\n")
		}
		if len(blocks) > 0 {
			b.WriteString("\n")
		}
		for _, sc := range blocks {
			b.WriteString("  block " + sc.Name)
			b.WriteString(list("implied_by", sc.SatisfiedBy))
			b.WriteString(scopeAttributes(sc, typeSurface) + "\n")
		}

		if len(t.Facts) > 0 || len(t.Guards) > 0 {
			b.WriteString("\n")
		}
		for _, f := range t.Facts {
			if f.DerivedFrom != "" {
				fmt.Fprintf(&b, "  attr %s: %s = derived(%s)\n", f.Name, f.Type, f.DerivedFrom)
				continue
			}
			fmt.Fprintf(&b, "  attr %s: %s\n", f.Name, f.Type)
		}
		for _, g := range t.Guards {
			cond, _ := r.Guard(t.Name, g)
			fmt.Fprintf(&b, "  guard %s = %s\n", g, renderCond(cond))
		}

		if len(t.Permissions) > 0 {
			b.WriteString("\n")
		}
		for _, name := range t.Permissions {
			p, _ := r.Permission(PermissionRef{Type: t.Name, Permission: name})
			fmt.Fprintf(&b, "  permission %s = %s", name, renderExpr(p.Expr, false))
			if p.AnswersScope != "" {
				b.WriteString("  answers " + p.AnswersScope)
			}
			if len(p.ExcludedBy) > 0 {
				b.WriteString("  excluded_by " + p.ExcludedBy[0])
			}
			b.WriteString("\n")
		}
		for _, name := range t.Alternatives {
			set, _ := r.Alternatives(PermissionRef{Type: t.Name, Permission: name})
			fmt.Fprintf(&b, "  permission %s = %s", name, renderExpr(set.Expr, false))
			if set.AnyBlockVetoes {
				b.WriteString("  any_block_vetoes")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// scopeAttributes prints a scope's catalogue and storage metadata in a fixed
// order, leaving out empty fields and the defaults: grants are user visible
// with permissive matching, blocks internal with strict matching, and every
// scope writes through the type's surface.
func scopeAttributes(sc ScopeDefinition, typeSurface string) string {
	var b strings.Builder
	defaultVisibility, defaultMatching := VisibilityUserVisible, MatchingPermissive
	if sc.Block {
		defaultVisibility, defaultMatching = VisibilityInternal, MatchingStrict
	}
	if sc.Visibility != defaultVisibility {
		b.WriteString("  " + string(sc.Visibility))
	}
	if sc.Sensitive {
		b.WriteString("  sensitive")
	}
	if sc.Matching != defaultMatching {
		b.WriteString("  matching: " + string(sc.Matching))
	}
	b.WriteString(list("default", sc.Roles))
	if len(sc.Anchors) > 1 || (len(sc.Anchors) == 1 && sc.Anchors[0] != Self) {
		anchors := make([]string, 0, len(sc.Anchors))
		for _, a := range sc.Anchors {
			anchors = append(anchors, strings.TrimPrefix(a, "$"))
		}
		b.WriteString(list("anchored_at", anchors))
	}
	b.WriteString(list("selectors", sc.SelectorKeys))
	b.WriteString(list("conditions", sc.Conditions))
	if sc.GrantSurface != typeSurface {
		b.WriteString("  surface: " + sc.GrantSurface)
	}
	if sc.DelegatableSince > 0 {
		fmt.Fprintf(&b, "  delegatable_since: %d", sc.DelegatableSince)
	}
	return b.String()
}

func list(label string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	return "  " + label + ": " + strings.Join(values, ", ")
}

func renderExpr(e Expr, nested bool) string {
	switch e.Op() {
	case OpRel:
		return e.Scope()
	case OpPerm:
		return e.Name()
	case OpIs:
		return "is(" + e.Name() + ")"
	case OpVia:
		return "via(" + e.Name() + ")." + e.Permission()
	case OpAnyOf:
		parts := make([]string, 0, len(e.Operands()))
		for _, o := range e.Operands() {
			parts = append(parts, renderExpr(o, false))
		}
		return "any_of(" + strings.Join(parts, ", ") + ")"
	case OpWhen:
		return "when " + e.Guard() + " (" + renderExpr(e.Operands()[0], false) + ")"
	case OpUnion, OpMinus:
		sep := " | "
		if e.Op() == OpMinus {
			sep = " - "
		}
		parts := make([]string, 0, len(e.Operands()))
		for _, o := range e.Operands() {
			parts = append(parts, renderExpr(o, true))
		}
		out := strings.Join(parts, sep)
		if nested {
			return "(" + out + ")"
		}
		return out
	default:
		return string(e.Op())
	}
}

func renderCond(c Cond) string {
	switch c.Op() {
	case CondAll:
		parts := make([]string, 0, len(c.Operands()))
		for _, o := range c.Operands() {
			parts = append(parts, renderCond(o))
		}
		return strings.Join(parts, " and ")
	case CondIsNull, CondIsFalse, CondNullOrEmpty, CondPresent:
		return string(c.Op()) + "(" + c.Fact() + ")"
	default:
		return string(c.Op())
	}
}

package authzgen

import (
	"cmp"
	"fmt"
	"go/format"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// table is one variable written into tables_gen.go. goType is its type as
// package authz spells it: the generator names authz's types, it never reads
// them.
type table struct {
	name   string
	doc    string
	goType string
	value  any
}

// tables lists, in file order, every table package authz reads from the
// registry. Each is computed here, at generation time, so package authz holds
// plain values and no derivation logic.
func tables(r registry.Registry) []table {
	kinds := make([]string, 0, len(r.Types))
	for _, t := range r.Types {
		kinds = append(kinds, string(t.Kind))
	}
	return []table{
		{
			name:   "scopeExpansions",
			goType: "map[Scope][]Scope",
			value:  r.DeriveScopeExpansions(),
			doc: `scopeExpansions maps a checked scope to the other scopes that also satisfy
it. For allow scopes, these are higher-privilege scopes. For blocklist
scopes, these are broader blocklist scopes. Expansion is non-transitive: the
table lists every satisfying scope directly, since Check.expand only walks
scopeExpansions[c.Scope] one step. Built from Implies and Exclusions; the
comments on each resource type in the registry say why its permissions
relate the way they do.`,
		},
		{
			name:   "scopeExclusions",
			goType: "map[Scope]Scope",
			value:  r.DeriveScopeExclusions(),
			doc: `scopeExclusions maps a checked base scope to the direct blocklist scope that
stores exception grants for it, or "" when it has none. Broader blocklist
scopes are handled by scopeExpansions on the blocklist scope itself.`,
		},
		{
			name:   "scopeGrantSource",
			goType: "map[Scope]GrantSource",
			value:  r.DeriveGrantSource(),
			doc: `scopeGrantSource maps every scope to where its grants are set. A block
is set where its permission is.`,
		},
		{
			name:   "scopeResourceKinds",
			goType: "map[Scope]string",
			value:  r.DeriveResourceKinds(),
			doc: `scopeResourceKinds maps every scope the registry declares to the resource
kind its checks and grant selectors name.`,
		},
		{
			name:   "resourceKinds",
			goType: "[]string",
			value:  kinds,
			doc:    `resourceKinds lists the resource kinds the registry declares, in declaration order.`,
		},
		{
			name:   "requiredCheckDimensions",
			goType: "map[string][]string",
			value:  r.DeriveRequiredCheckDimensions(),
			doc: `requiredCheckDimensions lists, per resource kind, the dimensions every check
of that kind must carry: the Required dimensions in the registry. Selector
matching skips grant keys a check omits, so a check without a dimension that
grants of its kind can be bounded by would pass such a grant whatever value
the grant names. Grants on a type inside a project can be bounded to one
({"resource_id":"*","project_id":P}), so every check on such a type must
name the project of the resource it targets.`,
		},
		{
			name:   "allowedSelectorKeys",
			goType: "map[string]map[string]bool",
			value:  r.DeriveAllowedSelectorKeys(),
			doc: `allowedSelectorKeys defines which extra keys (beyond resource_kind and
resource_id) are valid for each resource kind: the Dimensions each type
declares. Kinds not listed here allow no extra keys, except where
scopeAllowsSelectorKey admits one for a single scope.`,
		},
		{
			name:   "scopeSelectorKeys",
			goType: "map[Scope]map[string]bool",
			value:  r.DeriveScopeSelectorKeys(),
			doc: `scopeSelectorKeys holds the keys single permissions declare beyond their
type's.`,
		},
		{
			name:   "scopeOpenKeys",
			goType: "map[Scope]openKeys",
			value:  r.DeriveOpenKeys(),
			doc: `scopeOpenKeys lists, per scope, the grant keys a probe, a connection or a
risk policy audience question may leave out and still match a grant limited
on them: the Open keys each permission declares. Every other check, and
every block or exemption, must carry each key a grant names.`,
		},
		{
			name:   "validAuthClientTypes",
			goType: "map[string]bool",
			value:  r.DeriveDimensionValues("mcp_auth_client", registry.SelectorKeyClientType),
			doc:    `validAuthClientTypes is the closed value set of client_type.`,
		},
		{
			name:   "validDispositions",
			goType: "map[string]bool",
			value:  r.DeriveDimensionValues("mcp", registry.SelectorKeyDisposition),
			doc:    `validDispositions is the closed value set of disposition.`,
		},
		{
			name:   "systemRoleScopes",
			goType: "map[string][]Scope",
			value:  r.SystemRoles,
			doc:    `systemRoleScopes lists the scopes each built-in role is seeded with.`,
		},
		{
			name:   "agentEligibilityByScope",
			goType: "map[Scope]DerivedAgentEligibility",
			value:  r.DeriveAgentEligibility(),
			doc: `agentEligibilityByScope is the agent eligibility of every scope the registry
knows, retired tombstones included.`,
		},
		{
			name:   "scopeCatalog",
			goType: "[]ScopeEntry",
			value:  r.DeriveCatalog(),
			doc: `scopeCatalog lists every scope a role can hold, in the order the catalogue
presents them: by category, then as declared, each block after its
permission. ScopeRoot is deliberately absent: it is the engine's own bypass,
never a grant a person reads about.`,
		},
	}
}

// visibilityIdentifiers names the unexported constants scopeVisibilityByScope
// holds.
var visibilityIdentifiers = map[registry.Visibility]string{
	registry.VisibilityUserVisible: "scopeVisibilityUserVisible",
	registry.VisibilityInternal:    "scopeVisibilityInternal",
}

func renderTables(r registry.Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authz\n\n")
	b.WriteString("// The tables below are computed from the registry by authzgen, so the\n// evaluator reads plain values and holds no derivation logic.\n\n")
	for _, t := range tables(r) {
		writeDoc(&b, t.doc)
		fmt.Fprintf(&b, "var %s = ", t.name)
		if err := writeLiteral(&b, reflect.ValueOf(t.value), "", t.goType); err != nil {
			return nil, fmt.Errorf("%s: %w", t.name, err)
		}
		b.WriteString("\n\n")
	}

	writeDoc(&b, `scopeVisibilityByScope says whether a scope is exposed to clients as a
first-class permission or only used internally for storage and evaluation:
declared permissions are user-visible, generated blocks and root are internal.`)
	b.WriteString("var scopeVisibilityByScope = map[Scope]scopeVisibility{\n")
	visibility := r.DeriveScopeVisibility()
	for _, scope := range slices.Sorted(maps.Keys(visibility)) {
		ident, ok := visibilityIdentifiers[visibility[scope]]
		if !ok {
			return nil, fmt.Errorf("scope %q has unknown visibility %d", scope, visibility[scope])
		}
		fmt.Fprintf(&b, "\t%s: %s,\n", strconv.Quote(string(scope)), ident)
	}
	b.WriteString("}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format tables_gen.go: %w", err)
	}
	return formatted, nil
}

func writeDoc(b *strings.Builder, doc string) {
	for line := range strings.SplitSeq(doc, "\n") {
		b.WriteString("// " + line + "\n")
	}
}

// basic reports whether values of t print on one line.
func basic(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int:
		return true
	default:
		return false
	}
}

// writeLiteral writes v as a Go literal. Collections of basic values print on
// one line; others put each element on its own line. goType is written before
// the outermost composite literal; nested literals elide their type, so the
// value's own Go types never reach the output.
func writeLiteral(b *strings.Builder, v reflect.Value, indent string, goType string) error {
	t := v.Type()
	prefix := goType
	switch t.Kind() {
	case reflect.String:
		b.WriteString(strconv.Quote(v.String()))
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Map:
		if v.IsNil() {
			b.WriteString("nil")
			return nil
		}
		if t.Key().Kind() != reflect.String {
			return fmt.Errorf("unsupported map key %s", t.Key())
		}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, c reflect.Value) int { return cmp.Compare(a.String(), c.String()) })
		oneLine := basic(t.Elem())
		b.WriteString(prefix + "{")
		for i, k := range keys {
			if oneLine && len(keys) <= 4 {
				if i > 0 {
					b.WriteString(", ")
				}
			} else {
				b.WriteString("\n" + indent + "\t")
			}
			b.WriteString(strconv.Quote(k.String()) + ": ")
			if err := writeLiteral(b, v.MapIndex(k), indent+"\t", ""); err != nil {
				return err
			}
			if !oneLine || len(keys) > 4 {
				b.WriteString(",")
			}
		}
		if len(keys) > 0 && (!oneLine || len(keys) > 4) {
			b.WriteString("\n" + indent)
		}
		b.WriteString("}")
	case reflect.Slice:
		if v.IsNil() {
			b.WriteString("nil")
			return nil
		}
		b.WriteString(prefix + "{")
		if basic(t.Elem()) {
			for i := range v.Len() {
				if i > 0 {
					b.WriteString(", ")
				}
				if err := writeLiteral(b, v.Index(i), indent, ""); err != nil {
					return err
				}
			}
		} else {
			for i := range v.Len() {
				b.WriteString("\n" + indent + "\t")
				if err := writeLiteral(b, v.Index(i), indent+"\t", ""); err != nil {
					return err
				}
				b.WriteString(",")
			}
			if v.Len() > 0 {
				b.WriteString("\n" + indent)
			}
		}
		b.WriteString("}")
	case reflect.Struct:
		b.WriteString(prefix + "{")
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				return fmt.Errorf("%s has unexported field %s", t, f.Name)
			}
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(f.Name + ": ")
			// A field's composite literal cannot elide its type. Only
			// unnamed collections of basic values are written this way, so
			// their Go spelling is the same in package authz.
			fieldType := ""
			if k := f.Type.Kind(); (k == reflect.Slice || k == reflect.Map) && f.Type.Name() == "" && basic(f.Type.Elem()) {
				fieldType = f.Type.String()
			}
			if err := writeLiteral(b, v.Field(i), indent, fieldType); err != nil {
				return err
			}
		}
		b.WriteString("}")
	default:
		return fmt.Errorf("unsupported kind %s", t.Kind())
	}
	return nil
}

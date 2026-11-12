package authzgen

import (
	"cmp"
	"fmt"
	"go/format"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// table is one variable written into tables_gen.go.
type table struct {
	name  string
	doc   string
	value any
}

// tables lists, in file order, every table package authz reads from the
// registry. Each is computed here, at generation time, so package authz holds
// plain values and no derivation logic.
func tables(r registry.Registry) []table {
	declaredKinds := make(map[string]bool, len(r.Types))
	for _, t := range r.Types {
		declaredKinds[string(t.Kind)] = true
	}
	return []table{
		{"scopeExpansions", `scopeExpansions maps a checked scope to the other scopes that also satisfy
it. For allow scopes, these are higher-privilege scopes. For blocklist
scopes, these are broader blocklist scopes. Expansion is non-transitive: the
table lists every satisfying scope directly, since Check.expand only walks
scopeExpansions[c.Scope] one step. Built from Implies and Exclusions; the
comments on each resource type in the registry say why its permissions
relate the way they do.`, r.DeriveScopeExpansions()},
		{"scopeExclusions", `scopeExclusions maps a checked base scope to the direct blocklist scope that
stores exception grants for it, or "" when it has none. Broader blocklist
scopes are handled by scopeExpansions on the blocklist scope itself.`, r.DeriveScopeExclusions()},
		{"scopeGrantSource", `scopeGrantSource maps every scope to where its grants are set. A block
is set where its permission is.`, r.DeriveGrantSource()},
		{"scopeResourceKinds", `scopeResourceKinds maps every scope the registry declares to the resource
kind its checks and grant selectors name.`, r.DeriveResourceKinds()},
		{"declaredResourceKinds", `declaredResourceKinds is the set of resource kinds the registry declares.`, declaredKinds},
		{"requiredCheckDimensions", `requiredCheckDimensions lists, per resource kind, the dimensions every check
of that kind must carry: the Required dimensions in the registry. Selector
matching skips grant keys a check omits, so a check without a dimension that
grants of its kind can be bounded by would pass such a grant whatever value
the grant names. Grants on a type inside a project can be bounded to one
({"resource_id":"*","project_id":P}), so every check on such a type must
name the project of the resource it targets.`, r.DeriveRequiredCheckDimensions()},
		{"unknownableDimensions", `unknownableDimensions lists, per scope, the dimensions a check may carry as
SelectorValueUnknown.`, r.DeriveUnknownableDimensions()},
		{"allowedSelectorKeys", `allowedSelectorKeys defines which extra keys (beyond resource_kind and
resource_id) are valid for each resource kind: the Dimensions each type
declares. Kinds not listed here allow no extra keys, except where
scopeAllowsSelectorKey admits one for a single scope.`, r.DeriveAllowedSelectorKeys()},
		{"scopeSelectorKeys", `scopeSelectorKeys holds the keys single permissions declare beyond their
type's.`, r.DeriveScopeSelectorKeys()},
		{"validAuthClientTypes", `validAuthClientTypes is the closed value set of client_type.`, r.DeriveDimensionValues(authz.ResourceKindMCPAuthClient, authz.SelectorKeyClientType)},
		{"validDispositions", `validDispositions is the closed value set of disposition.`, r.DeriveDimensionValues(authz.ResourceKindMCP, authz.SelectorKeyDisposition)},
		{"systemRoleScopes", `systemRoleScopes lists the scopes each built-in role is seeded with.`, r.SystemRoles},
		{"agentEligibilityByScope", `agentEligibilityByScope is the agent eligibility of every scope the registry
knows, retired tombstones included.`, r.DeriveAgentEligibility()},
		{"scopeCatalog", `scopeCatalog lists every scope a role can hold, in the order the catalogue
presents them: by category, then as declared, each block after its
permission. ScopeRoot is deliberately absent: it is the engine's own bypass,
never a grant a person reads about.`, r.DeriveCatalog()},
	}
}

// visibilityIdentifiers names the unexported constants scopeVisibilityByScope
// holds.
var visibilityIdentifiers = map[string]string{
	authz.ScopeVisibilityUserVisible: "scopeVisibilityUserVisible",
	authz.ScopeVisibilityInternal:    "scopeVisibilityInternal",
}

func renderTables(r registry.Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authz\n\n")
	b.WriteString("// The tables below are computed from the registry by authzgen, so the\n// evaluator reads plain values and holds no derivation logic.\n\n")
	for _, t := range tables(r) {
		writeDoc(&b, t.doc)
		fmt.Fprintf(&b, "var %s = ", t.name)
		if err := writeLiteral(&b, reflect.ValueOf(t.value), "", true); err != nil {
			return nil, fmt.Errorf("%s: %w", t.name, err)
		}
		b.WriteString("\n\n")
	}

	writeDoc(&b, `scopeVisibilityByScope says whether a scope is exposed to clients as a
first-class permission or only used internally for storage and evaluation:
declared permissions are user-visible, generated blocks and root are internal.`)
	b.WriteString("var scopeVisibilityByScope = map[Scope]scopeVisibility{\n")
	visibility := r.DeriveScopeVisibility()
	for _, scope := range sortedKeys(visibility) {
		ident, ok := visibilityIdentifiers[visibility[scope]]
		if !ok {
			return nil, fmt.Errorf("scope %q has unknown visibility %q", scope, visibility[scope])
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

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// typeName spells a type as package authz writes it.
func typeName(t reflect.Type) string {
	return strings.ReplaceAll(t.String(), "authz.", "")
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
// one line; others put each element on its own line. typed writes the type
// before a composite literal, which nested literals elide.
func writeLiteral(b *strings.Builder, v reflect.Value, indent string, typed bool) error {
	t := v.Type()
	prefix := ""
	if typed {
		prefix = typeName(t)
	}
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
			if err := writeLiteral(b, v.MapIndex(k), indent+"\t", false); err != nil {
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
				if err := writeLiteral(b, v.Index(i), indent, false); err != nil {
					return err
				}
			}
		} else {
			for i := range v.Len() {
				b.WriteString("\n" + indent + "\t")
				if err := writeLiteral(b, v.Index(i), indent+"\t", false); err != nil {
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
			if err := writeLiteral(b, v.Field(i), indent, false); err != nil {
				return err
			}
		}
		b.WriteString("}")
	default:
		return fmt.Errorf("unsupported kind %s", t.Kind())
	}
	return nil
}

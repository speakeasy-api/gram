package relschema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	keyResourceKind = "resource_kind"
	keyResourceID   = "resource_id"
	wildcard        = "*"
	// representedUserMarker is bound per request by the legacy engine. No
	// declared relation takes it, so a row carrying it cannot be decoded.
	representedUserMarker = "$represented_user"
)

// Row is one stored grant row: principal_grants as the engine loaded it.
type Row struct {
	Principal string
	Scope     string
	Selector  map[string]string
}

// ObjectRef names one object.
type ObjectRef struct {
	Type string
	ID   string
}

func (o ObjectRef) String() string { return o.Type + ":" + o.ID }

// Relationship is a stored row as the schema understands it.
type Relationship struct {
	Type  string // the type declaring the relation
	Grant string
	// Anchor is the object the row attaches to: the object itself or one of
	// its ancestors.
	Anchor ObjectRef
	// Narrowing restricts a relationship to objects whose ancestor of the
	// given type has the given id.
	Narrowing map[string]string
	// Constraints are request facts the relationship requires, e.g. a tool.
	Constraints map[string]string
	// Legacy marks a row decoded at a legacy anchor.
	Legacy bool
	// Strict marks a row that only matches requests carrying every
	// constraint, as legacy API key grants do.
	Strict bool
	Source string
}

func (r Relationship) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s#%s.%s", r.Anchor, r.Type, r.Grant)
	for _, k := range slices.Sorted(maps.Keys(r.Narrowing)) {
		fmt.Fprintf(&b, "[%s=%s]", k, r.Narrowing[k])
	}
	for _, k := range slices.Sorted(maps.Keys(r.Constraints)) {
		fmt.Fprintf(&b, "{%s=%s}", k, r.Constraints[k])
	}
	if r.Legacy {
		b.WriteString(" (legacy broad grant)")
	}
	fmt.Fprintf(&b, " from %s", r.Source)
	return b.String()
}

// DecodeOptions carries per-row facts the schema does not store.
type DecodeOptions struct {
	// Strict marks the row as matching only requests that carry every
	// constraint it names.
	Strict bool
}

// Decode maps one stored row onto a relationship. mapped is false when the
// row's scope is declared out of schema, so another evaluator owns it. Any
// shape the schema cannot express fails rather than being dropped.
func (s *Schema) Decode(organizationID string, row Row, opts DecodeOptions) (rel Relationship, mapped bool, err error) {
	target, ok := s.byScope[row.Scope]
	if !ok {
		if _, out := s.outOfScope[row.Scope]; out {
			return Relationship{}, false, nil
		}
		return Relationship{}, false, fmt.Errorf("scope %q has no schema mapping", row.Scope)
	}
	t, g := target.typ, target.grant
	source := row.Principal + " " + row.Scope + " " + selectorString(row.Selector)
	fail := func(format string, args ...any) (Relationship, bool, error) {
		return Relationship{}, true, fmt.Errorf("grant %s: %s", source, fmt.Sprintf(format, args...))
	}
	if organizationID == "" {
		return fail("no organization to anchor at")
	}

	sel := maps.Clone(row.Selector)
	kind, hasKind := sel[keyResourceKind]
	id, hasID := sel[keyResourceID]
	if !hasKind || !hasID {
		return fail("selector must name resource_kind and resource_id")
	}
	if kind != g.kind(t) {
		return fail("resource_kind %q does not match %s.%s", kind, t.Name, g.Name)
	}
	if id == "" {
		return fail("empty resource_id")
	}
	delete(sel, keyResourceKind)
	delete(sel, keyResourceID)
	for k, v := range sel {
		if v == "" {
			return fail("empty selector value for %q", k)
		}
		if v == representedUserMarker {
			return fail("%q is not supported here", representedUserMarker)
		}
	}

	// Ancestor keys either anchor a wildcard row or narrow a concrete one.
	ancestors := map[string]string{}
	for key, typ := range s.AncestorKeys {
		v, has := sel[key]
		if !has {
			continue
		}
		delete(sel, key)
		if v == wildcard {
			continue
		}
		if !slices.Contains(t.chain, typ) {
			return fail("%q names %s, which does not contain %s", key, typ, t.Name)
		}
		ancestors[typ] = v
	}

	rel = Relationship{Type: t.Name, Grant: g.Name, Source: source, Strict: opts.Strict}
	anchor := Self
	switch {
	case id != wildcard:
		rel.Anchor = ObjectRef{Type: t.Name, ID: id}
	default:
		// Anchor at the nearest named ancestor, else the tenant root.
		anchor = s.Root
		for _, typ := range t.chain {
			if v, ok := ancestors[typ]; ok {
				anchor = typ
				rel.Anchor = ObjectRef{Type: typ, ID: v}
				delete(ancestors, typ)
				break
			}
		}
		if anchor == s.Root {
			if t.Name == s.Root {
				anchor = Self
			}
			rel.Anchor = ObjectRef{Type: s.Root, ID: organizationID}
		}
	}
	switch {
	case slices.Contains(g.Anchors, anchor) || (anchor == Self && slices.Contains(g.Anchors, t.Name)):
	case slices.Contains(g.LegacyAnchors, anchor):
		rel.Legacy = true
	default:
		return fail("%s.%s cannot be anchored at %s", t.Name, g.Name, anchor)
	}

	for typ, v := range ancestors {
		if !slices.Contains(g.Narrowable, typ) {
			return fail("%s.%s cannot be narrowed by %s", t.Name, g.Name, typ)
		}
		if rel.Narrowing == nil {
			rel.Narrowing = map[string]string{}
		}
		rel.Narrowing[typ] = v
	}

	for k, v := range sel {
		if !slices.Contains(g.Constraints, k) {
			return fail("unsupported selector key %q for %s.%s", k, t.Name, g.Name)
		}
		if rel.Constraints == nil {
			rel.Constraints = map[string]string{}
		}
		rel.Constraints[k] = v
	}
	return rel, true, nil
}

func selectorString(sel map[string]string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range slices.Sorted(maps.Keys(sel)) {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%s", k, sel[k])
	}
	b.WriteByte('}')
	return b.String()
}

package authz

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
)

// Term is one way an object can be visible. Path names the fact compared:
// "id", a parent type ("project"), an attribute ("author"), or a chain
// ("agent.owner"). Not lists terms that remove matches.
type Term struct {
	Path string
	All  bool
	In   []string
	And  []Term // every one must also match
	Not  []Term
}

// Filter is a predicate for list queries: AND over layers, OR within a layer.
// Repositories render it into SQL or ClickHouse, so pagination and totals are
// computed over authorized rows only.
type Filter struct {
	Layers [][]Term
}

type lookupCtx struct {
	sc    *schema.Schema
	layer *Snapshot
	facts *OrgFacts
	now   time.Time
}

func (c *lookupCtx) perm(typ, name string) []Term {
	t := c.sc.Types[typ]
	return c.expr(t, t.Perms[name].Expr)
}

func (c *lookupCtx) expr(t *schema.Type, e schema.Expr) []Term {
	switch x := e.(type) {
	case schema.RelExpr:
		return c.rel(t, t.Grants[x.Name])
	case schema.PermExpr:
		return c.expr(t, t.Perms[x.Name].Expr)
	case schema.UnionExpr:
		var out []Term
		for _, i := range x.Items {
			out = append(out, c.expr(t, i)...)
		}
		return out
	case schema.MinusExpr:
		base, sub := c.expr(t, x.Base), c.expr(t, x.Sub)
		for _, s := range sub {
			if s.All && s.Path == "" {
				return nil
			}
		}
		for i := range base {
			base[i].Not = append(base[i].Not, sub...)
		}
		return base
	case schema.IsExpr:
		if t.Attrs[x.Attr] != c.layer.Subject.Type {
			return nil
		}
		return []Term{{Path: x.Attr, In: []string{c.layer.Subject.ID}}}
	case schema.ViaExpr:
		target, ok := t.Attrs[x.Attr]
		if !ok {
			target = t.Parent
		}
		return prefix(c.perm(target, x.Perm), x.Attr)
	case schema.MarkedExpr:
		ids := c.facts.marked[relKey{t.Name, x.Marker}]
		if len(ids) == 0 {
			return nil
		}
		return []Term{{Path: "id", In: ids}}
	}
	return nil
}

func (c *lookupCtx) rel(t *schema.Type, g *schema.GrantDef) []Term {
	if len(g.SubjectAttrs) > 0 {
		return c.subjectScopedRel(t, g)
	}
	var out []Term
	for _, anchor := range g.Anchors {
		stored := schema.StoredRelation(t.Name, g.Name, anchor)
		anchorType := anchor
		if anchor == schema.Self {
			anchorType = t.Name
		}
		var ids []string
		for _, e := range c.layer.byRelation[relKey{anchorType, stored}] {
			if !e.expiresAt.IsZero() && !c.now.Before(e.expiresAt) {
				continue
			}
			if g.Block && e.cond != "" {
				continue // a conditional block never hides the whole object
			}
			ids = append(ids, e.objID)
		}
		if len(ids) == 0 {
			continue
		}
		switch {
		case anchor == schema.Self:
			out = append(out, Term{Path: "id", In: ids})
		case slices.Contains(t.MemberOf, anchor):
			var members []string
			for _, gid := range ids {
				members = append(members, c.facts.membersOf[gid]...)
			}
			out = append(out, Term{Path: "id", In: members})
		case anchor == "organization":
			out = append(out, Term{All: true})
		default:
			out = append(out, Term{Path: anchor, In: ids})
		}
	}
	return out
}

// subjectScopedRel emits one term per grant: where it is anchored AND whose
// activity it covers.
func (c *lookupCtx) subjectScopedRel(t *schema.Type, g *schema.GrantDef) []Term {
	var out []Term
	for _, anchor := range g.Anchors {
		anchorType := anchor
		if anchor == schema.Self {
			anchorType = t.Name
		}
		for _, e := range c.layer.byRelation[relKey{anchorType, schema.StoredRelation(t.Name, g.Name, anchor)}] {
			if !e.expiresAt.IsZero() && !c.now.Before(e.expiresAt) {
				continue
			}
			var where Term
			switch {
			case anchor == schema.Self:
				where = Term{Path: "id", In: []string{e.objID}}
			case anchor == "organization":
				where = Term{All: true}
			default:
				where = Term{Path: anchor, In: []string{e.objID}}
			}
			for _, attr := range g.SubjectAttrs {
				var who Term
				typ, id, _ := strings.Cut(e.subject, ":")
				switch {
				case e.subject == "" || e.subject == "*":
					who = Term{Path: attr, All: true}
				case typ == "role" && t.Attrs[attr] == "user":
					who = Term{Path: attr, In: c.facts.typedMembers[e.subject]}
				case typ == t.Attrs[attr]:
					who = Term{Path: attr, In: []string{id}}
				default:
					continue
				}
				w := where
				w.And = []Term{who}
				out = append(out, w)
			}
		}
	}
	return out
}

func prefix(terms []Term, attr string) []Term {
	out := make([]Term, len(terms))
	for i, t := range terms {
		p := attr
		if t.Path != "" && t.Path != "id" {
			p = attr + "." + t.Path
		}
		out[i] = Term{Path: p, All: t.All, In: t.In, And: prefix(t.And, attr), Not: prefix(t.Not, attr)}
	}
	return out
}

// Matches evaluates the filter in memory. Tests use it to prove Lookup agrees
// with Check; production renders it into the query instead.
func (f Filter) Matches(ref Ref) bool {
	for _, layer := range f.Layers {
		if !slices.ContainsFunc(layer, func(t Term) bool { return t.matches(ref) }) {
			return false
		}
	}
	return len(f.Layers) > 0
}

func (t Term) matches(ref Ref) bool {
	vals := resolve(ref, t.Path)
	ok := (t.All && len(vals) > 0) || slices.ContainsFunc(vals, func(v string) bool { return slices.Contains(t.In, v) })
	for _, a := range t.And {
		ok = ok && a.matches(ref)
	}
	return ok && !slices.ContainsFunc(t.Not, func(n Term) bool { return n.matches(ref) })
}

func resolve(ref Ref, path string) []string {
	if path == "" || path == "id" {
		return []string{ref.ID}
	}
	head, rest, _ := strings.Cut(path, ".")
	var targets []Ref
	if p, ok := ref.parent(head); ok {
		targets = []Ref{p}
	} else {
		targets = ref.Attrs[head]
	}
	var out []string
	for _, tr := range targets {
		if rest == "" {
			out = append(out, tr.ID)
		} else {
			out = append(out, resolve(tr, rest)...)
		}
	}
	return out
}

// SQL renders the filter as a WHERE fragment. cols maps each Path to a column;
// args receives positional parameters starting at $len(args)+1.
func (f Filter) SQL(cols map[string]string, args *[]any) (string, error) {
	if len(f.Layers) == 0 {
		return "FALSE", nil
	}
	var layers []string
	for _, layer := range f.Layers {
		if len(layer) == 0 {
			return "FALSE", nil
		}
		var ors []string
		for _, t := range layer {
			s, err := t.sql(cols, args)
			if err != nil {
				return "", err
			}
			ors = append(ors, s)
		}
		layers = append(layers, "("+strings.Join(ors, " OR ")+")")
	}
	return strings.Join(layers, " AND "), nil
}

func (t Term) sql(cols map[string]string, args *[]any) (string, error) {
	path := t.Path
	if path == "" {
		path = "id"
	}
	col, ok := cols[path]
	if !ok {
		return "", fmt.Errorf("no column for filter path %q", path)
	}
	var s string
	if t.All {
		s = col + " IS NOT NULL"
	} else {
		*args = append(*args, t.In)
		s = fmt.Sprintf("%s = ANY($%d)", col, len(*args))
	}
	for _, a := range t.And {
		as, err := a.sql(cols, args)
		if err != nil {
			return "", err
		}
		s += " AND " + as
	}
	for _, n := range t.Not {
		ns, err := n.sql(cols, args)
		if err != nil {
			return "", err
		}
		// COALESCE: a NULL fact (e.g. no author on an agent-only chat) must
		// not turn NOT into NULL and silently drop the row.
		s += " AND NOT COALESCE(" + ns + ", FALSE)"
	}
	return "(" + s + ")", nil
}

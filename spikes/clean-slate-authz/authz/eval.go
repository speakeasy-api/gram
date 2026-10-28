package authz

import (
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
)

// evalCtx evaluates one permission for one layer. It performs no I/O: object
// facts come from the Ref, target facts from OrgFacts, grants from the layer.
type evalCtx struct {
	sc       *schema.Schema
	layer    *Snapshot
	facts    *OrgFacts
	now      time.Time
	reqCtx   any
	eligible bool
	explain  bool
	path     []string
}

func (c *evalCtx) perm(ref Ref, name string) schema.Tri {
	t, ok := c.sc.Types[ref.Type]
	if !ok {
		return schema.Unknown
	}
	p, ok := t.Perms[name]
	if !ok {
		return schema.Unknown
	}
	return c.expr(t, ref, p.Expr)
}

func (c *evalCtx) expr(t *schema.Type, ref Ref, e schema.Expr) schema.Tri {
	switch x := e.(type) {
	case schema.RelExpr:
		return c.rel(t, ref, t.Grants[x.Name])
	case schema.PermExpr:
		return c.expr(t, ref, t.Perms[x.Name].Expr)
	case schema.UnionExpr:
		out := schema.False
		for _, item := range x.Items {
			switch c.expr(t, ref, item) {
			case schema.True:
				return schema.True
			case schema.Unknown:
				out = schema.Unknown
			}
		}
		return out
	case schema.MinusExpr:
		// Kleene "base and not sub": an undetermined exclusion never allows.
		base := c.expr(t, ref, x.Base)
		if base == schema.False {
			return schema.False
		}
		sub := c.expr(t, ref, x.Sub)
		switch {
		case sub == schema.True:
			return schema.False
		case base == schema.True && sub == schema.False:
			return schema.True
		default:
			return schema.Unknown
		}
	case schema.IsExpr:
		vals, ok := ref.Attrs[x.Attr]
		if !ok {
			return schema.Unknown // the handler did not supply the attribute
		}
		for _, v := range vals {
			if v.Type == c.layer.Subject.Type && v.ID == c.layer.Subject.ID {
				if c.explain {
					c.path = append(c.path, ref.Type+":"+ref.ID+" "+x.Attr+" is "+v.Type+":"+v.ID)
				}
				return schema.True
			}
		}
		return schema.False
	case schema.ViaExpr:
		targets, ok := ref.Attrs[x.Attr]
		if _, isAttr := t.Attrs[x.Attr]; !isAttr {
			p, found := ref.parent(x.Attr)
			targets, ok = []Ref{p}, found
		}
		if !ok {
			return schema.Unknown
		}
		out := schema.False
		for _, target := range targets {
			switch c.perm(target, x.Perm) {
			case schema.True:
				return schema.True
			case schema.Unknown:
				out = schema.Unknown
			}
		}
		return out
	case schema.MarkedExpr:
		if c.facts == nil {
			return schema.Unknown
		}
		return schema.Bool(c.facts.markers[anchorKey{t.Name, ref.ID, x.Marker}])
	}
	return schema.Unknown
}

// rel walks a grant's anchors: the object itself, its ancestors from the Ref,
// and its memberships from OrgFacts.
func (c *evalCtx) rel(t *schema.Type, ref Ref, g *schema.GrantDef) schema.Tri {
	best := schema.False
	var keys []anchorKey
	for _, anchor := range g.Anchors {
		stored := schema.StoredRelation(t.Name, g.Name, anchor)
		switch {
		case anchor == schema.Self:
			keys = append(keys, anchorKey{t.Name, ref.ID, g.Name})
		case slices.Contains(t.MemberOf, anchor):
			if c.facts == nil {
				best = max(best, schema.Unknown)
				continue
			}
			for _, gid := range c.facts.groupsOf[ref.ID] {
				keys = append(keys, anchorKey{anchor, gid, stored})
			}
		default:
			p, ok := ref.parent(anchor)
			if !ok {
				best = max(best, schema.Unknown) // missing parent fact
				continue
			}
			keys = append(keys, anchorKey{anchor, p.ID, stored})
		}
	}
	for _, k := range keys {
		for _, e := range c.layer.byAnchor[k] {
			if !e.expiresAt.IsZero() && !c.now.Before(e.expiresAt) {
				continue
			}
			v := c.cond(g, e)
			if v != schema.False && len(g.SubjectAttrs) > 0 {
				v = min(v, c.subjectMatch(ref, g, e)) // Kleene AND
			}
			switch v {
			case schema.True:
				if c.explain {
					c.path = append(c.path, e.rel+" (via "+e.via.String()+")")
				}
				return schema.True
			case schema.Unknown:
				best = schema.Unknown
			}
		}
	}
	return best
}

// subjectMatch checks a subject-scoped grant against the object's attributed
// subjects ("whose activity is this?"), including their role memberships.
func (c *evalCtx) subjectMatch(ref Ref, g *schema.GrantDef, e entry) schema.Tri {
	out := schema.False
	for _, attr := range g.SubjectAttrs {
		vals, ok := ref.Attrs[attr]
		if !ok {
			out = max(out, schema.Unknown) // attribution not supplied
			continue
		}
		for _, v := range vals {
			if e.subject == "" || e.subject == "*" || e.subject == v.Type+":"+v.ID {
				return schema.True
			}
			if strings.HasPrefix(e.subject, "role:") {
				if c.facts == nil {
					out = max(out, schema.Unknown)
					continue
				}
				if slices.Contains(c.facts.typedOf[v.ID], e.subject) {
					return schema.True
				}
			}
		}
	}
	return out
}

func (c *evalCtx) cond(g *schema.GrantDef, e entry) schema.Tri {
	if e.cond == "" {
		return schema.True
	}
	cd, ok := c.sc.Conditions[e.cond]
	if !ok {
		return schema.Unknown
	}
	if c.eligible {
		// "Could any context satisfy this?" A conditional block always leaves
		// some context unblocked, so it does not remove eligibility.
		if g.Block {
			return schema.False
		}
		return schema.Bool(cd.Satisfiable(e.params))
	}
	return cd.Eval(e.params, c.reqCtx)
}

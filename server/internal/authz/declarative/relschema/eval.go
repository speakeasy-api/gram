package relschema

import (
	"fmt"
	"slices"
)

// Object is a protected object with the trusted facts needed to evaluate it:
// its own id and its verified ancestors, nearest first, ending at the root.
type Object struct {
	Type      string
	ID        string
	Ancestors []ObjectRef
	// Attrs are the object's attribute values, loaded from trusted rows. An
	// absent attribute is no relationship, not an unknown one.
	Attrs map[string]ObjectRef
}

// Request carries request facts that constrained relations are matched
// against, e.g. the tool being called. A nil map is a resource-level request.
type Request struct {
	Facts map[string]string
	// Subject is the caller as an object, e.g. user:U. Empty when the caller
	// is not a person; Is then never holds.
	Subject ObjectRef
	// Objects are the related objects Via may traverse to, with their own
	// ancestors and attributes, loaded by the service for this request.
	Objects map[ObjectRef]Object
}

type Outcome uint8

const (
	Denied Outcome = iota
	Allowed
	// Undetermined means a required fact was missing or malformed. It never
	// allows.
	Undetermined
)

func (o Outcome) String() string {
	switch o {
	case Allowed:
		return "allowed"
	case Undetermined:
		return "undetermined"
	default:
		return "denied"
	}
}

// Decision is one permission's outcome for one policy, with the trace that
// produced it.
type Decision struct {
	Outcome Outcome
	Reason  string
	// Path lists the relationships that allowed, or the block that withdrew
	// the allow.
	Path []string
}

type tri uint8

const (
	triFalse tri = iota
	triUnknown
	triTrue
)

type evalCtx struct {
	s     *Schema
	org   string
	depth int
	rels  []Relationship
	req   Request
	path  []string
	// withdrawn records the block that removed an allow, for the trace.
	withdrawn []string
}

// Evaluate answers whether rels, all of one policy admitted for
// organizationID, grant perm on obj. It performs no I/O.
func (s *Schema) Evaluate(organizationID string, rels []Relationship, obj Object, perm string, req Request) Decision {
	t, ok := s.Types[obj.Type]
	if !ok {
		return Decision{Outcome: Undetermined, Reason: fmt.Sprintf("unknown type %q", obj.Type)}
	}
	p, ok := t.Perms[perm]
	if !ok {
		return Decision{Outcome: Undetermined, Reason: fmt.Sprintf("%s has no permission %q", obj.Type, perm)}
	}
	if reason := s.checkObject(t, obj); reason != "" {
		return Decision{Outcome: Undetermined, Reason: reason}
	}
	if organizationID == "" {
		return Decision{Outcome: Undetermined, Reason: "policy has no admitted organization"}
	}
	if tenant := obj.tenant(s.Root); tenant != organizationID {
		return Decision{Outcome: Denied, Reason: "object belongs to another organization"}
	}

	c := &evalCtx{s: s, org: organizationID, rels: rels, req: req}
	switch c.expr(t, obj, p.Expr) {
	case triTrue:
		return Decision{Outcome: Allowed, Reason: "allowed", Path: c.path}
	case triUnknown:
		return Decision{Outcome: Undetermined, Reason: "a required fact was missing"}
	default:
		if len(c.withdrawn) > 0 {
			return Decision{Outcome: Denied, Reason: "withdrawn by a matching block", Path: c.withdrawn}
		}
		return Decision{Outcome: Denied, Reason: "no relationship grants " + obj.Type + "." + perm}
	}
}

// checkObject verifies obj carries exactly its type's containment chain.
func (s *Schema) checkObject(t *Type, obj Object) string {
	if obj.ID == "" {
		return "object has no id"
	}
	if len(obj.Ancestors) != len(t.chain) {
		return fmt.Sprintf("%s needs ancestors %v", t.Name, t.chain)
	}
	for i, a := range obj.Ancestors {
		if a.Type != t.chain[i] || a.ID == "" {
			return fmt.Sprintf("%s needs ancestors %v", t.Name, t.chain)
		}
	}
	return ""
}

func (o Object) tenant(root string) string {
	if o.Type == root {
		return o.ID
	}
	if n := len(o.Ancestors); n > 0 && o.Ancestors[n-1].Type == root {
		return o.Ancestors[n-1].ID
	}
	return ""
}

func (o Object) ref() ObjectRef { return ObjectRef{Type: o.Type, ID: o.ID} }

// ancestor returns obj's ancestor of typ as an object of its own.
func (o Object) ancestor(typ string) (Object, bool) {
	if o.Type == typ {
		return o, true
	}
	for i, a := range o.Ancestors {
		if a.Type == typ {
			return Object{Type: a.Type, ID: a.ID, Ancestors: o.Ancestors[i+1:]}, true
		}
	}
	return Object{}, false
}

func (c *evalCtx) expr(t *Type, obj Object, e Expr) tri {
	switch x := e.(type) {
	case RelExpr:
		return c.rel(t, obj, t.Grants[x.Name])
	case AncestorRelExpr:
		holder, ok := obj.ancestor(x.Type)
		if !ok {
			return triUnknown
		}
		ht := c.s.Types[x.Type]
		return c.rel(ht, holder, ht.Grants[x.Name])
	case UnionExpr:
		out := triFalse
		for _, item := range x.Items {
			switch c.expr(t, obj, item) {
			case triTrue:
				return triTrue
			case triUnknown:
				out = triUnknown
			}
		}
		return out
	case MinusExpr:
		// Kleene "base and not sub": an undetermined block never allows.
		base := c.expr(t, obj, x.Base)
		if base == triFalse {
			return triFalse
		}
		allowPath := c.path
		c.path = nil
		sub := c.expr(t, obj, x.Sub)
		blockPath := c.path
		c.path = allowPath
		switch {
		case sub == triTrue:
			c.withdrawn = append(c.withdrawn, blockPath...)
			c.path = nil
			return triFalse
		case base == triTrue && sub == triFalse:
			return triTrue
		default:
			return triUnknown
		}
	case IsExpr:
		ref, ok := obj.Attrs[x.Attr]
		if !ok || c.req.Subject == (ObjectRef{}) || ref != c.req.Subject {
			return triFalse
		}
		c.path = append(c.path, fmt.Sprintf("%s is %s#%s", ref, obj.ref(), x.Attr))
		return triTrue
	case UnlessExpr:
		if _, has := obj.Attrs[x.Attr]; has {
			return triFalse
		}
		return c.expr(t, obj, x.Expr)
	case ViaExpr:
		return c.via(t, obj, x)
	}
	return triUnknown
}

// maxViaDepth bounds traversal so a cycle of attributes cannot recurse.
const maxViaDepth = 4

// via evaluates a permission on the object an attribute names. A related
// object the request did not supply, of the wrong type, or with malformed
// ancestors is undetermined; one in another tenant is no relationship.
func (c *evalCtx) via(t *Type, obj Object, x ViaExpr) tri {
	ref, ok := obj.Attrs[x.Attr]
	if !ok {
		return triFalse
	}
	target, ok := c.req.Objects[ref]
	if !ok || ref.Type != t.Attrs[x.Attr] || target.ref() != ref || c.depth >= maxViaDepth {
		return triUnknown
	}
	tt := c.s.Types[ref.Type]
	if c.s.checkObject(tt, target) != "" {
		return triUnknown
	}
	if target.tenant(c.s.Root) != c.org {
		return triFalse
	}
	c.depth++
	defer func() { c.depth-- }()
	out := c.expr(tt, target, tt.Perms[x.Perm].Expr)
	if out == triTrue {
		c.path = append(c.path, fmt.Sprintf("via %s#%s", obj.ref(), x.Attr))
	}
	return out
}

// rel reports whether any relationship confers g on obj: anchored at obj or
// one of its ancestors, within every narrowing, and with every constraint
// satisfied by the request.
func (c *evalCtx) rel(t *Type, obj Object, g *GrantDef) tri {
	reach := append([]ObjectRef{obj.ref()}, obj.Ancestors...)
	for _, r := range c.rels {
		if r.Type != t.Name || r.Grant != g.Name {
			continue
		}
		if !slices.Contains(reach, r.Anchor) {
			continue
		}
		if !narrowingHolds(obj, r.Narrowing) {
			continue
		}
		if !c.constraintsHold(g, r) {
			continue
		}
		c.path = append(c.path, r.String())
		return triTrue
	}
	return triFalse
}

func narrowingHolds(obj Object, narrowing map[string]string) bool {
	for typ, id := range narrowing {
		a, ok := obj.ancestor(typ)
		if !ok || a.ID != id {
			return false
		}
	}
	return true
}

// constraintsHold matches a relationship's constraints against request facts.
// A fact the request carries must equal the constraint. A fact it omits is a
// resource-level request: an ordinary allow still applies there, so a
// tool-limited grant admits its server before any tool is known, while a
// strict allow and any block apply only where every constraint is proven.
func (c *evalCtx) constraintsHold(g *GrantDef, r Relationship) bool {
	for key, want := range r.Constraints {
		got, has := c.req.Facts[key]
		if has {
			if want != wildcard && want != got {
				return false
			}
			continue
		}
		if g.Block || r.Strict {
			return false
		}
	}
	return true
}

package schema

import (
	"cmp"
	"slices"
	"strings"
)

// Matching is how a stored selector is compared with a check.
type Matching string

const (
	// MatchingPermissive skips grant keys the check does not carry.
	MatchingPermissive Matching = "permissive"
	// MatchingStrict requires every grant key to be present in the check.
	MatchingStrict Matching = "strict"
)

// Visibility says whether the catalogue exposes a scope.
type Visibility string

const (
	// VisibilityUserVisible scopes appear in the permission catalogue.
	VisibilityUserVisible Visibility = "user_visible"
	// VisibilityInternal scopes are stored and evaluated but not offered.
	VisibilityInternal Visibility = "internal"
)

// RuntimeLifecycle is a scope's state in the agent runtime projection.
type RuntimeLifecycle string

const (
	// RuntimeActive scopes may appear in new delegated policies.
	RuntimeActive RuntimeLifecycle = "active"
	// RuntimeRetired scopes are still decoded from stored policies but grant
	// nothing.
	RuntimeRetired RuntimeLifecycle = "retired"
)

// ScopeDefinition is the compiled metadata and projections of one stored scope.
type ScopeDefinition struct {
	// Name is the stored scope, such as "mcp:read".
	Name string

	// Type is the declared type that owns the scope.
	Type string

	// Relation is the Grant or Block name on Type.
	Relation string

	// ResourceKind is the selector resource_kind stored grants carry.
	ResourceKind string

	// Block reports a blocking scope.
	Block bool

	// Matching is how stored selectors for the scope match checks.
	Matching Matching

	// Visibility says whether the catalogue offers the scope.
	Visibility Visibility

	// Description is the catalogue text; empty for internal scopes.
	Description string

	// Sensitive marks scopes that expose sensitive content.
	Sensitive bool

	// WrittenBy is the grant surface that owns writes for the scope.
	WrittenBy string

	// Anchors lists where grants may be stored, in declaration order. A
	// relation without AnchoredAt is anchored at Self only.
	Anchors []string

	// Conditions lists the pure conditions every path through the scope
	// applies.
	Conditions []string

	// SelectorKeys lists the accepted selector keys beyond resource_kind and
	// resource_id, sorted.
	SelectorKeys []string

	// Roles lists the system roles that hold the scope by default, sorted.
	Roles []string

	// RuntimeSafeSince is the runtime registry version from which the scope is
	// safe in a delegated policy; 0 means never.
	RuntimeSafeSince int

	// SatisfiedBy lists the other stored scopes whose grants also satisfy an
	// authz.Check for this scope, one step and sorted.
	SatisfiedBy []string

	// Exclusion is the blocking scope that directly excludes an authz.Check
	// for this scope, or "".
	Exclusion string

	// Closure lists every scope whose check a grant of this scope satisfies,
	// including itself, sorted. It is the containment projection.
	Closure []string

	// SubScopes lists the user-visible scopes a grant of this user-visible
	// scope also satisfies, sorted. It is the catalogue projection.
	SubScopes []string
}

func (s ScopeDefinition) clone() ScopeDefinition {
	s.Anchors = slices.Clone(s.Anchors)
	s.Conditions = slices.Clone(s.Conditions)
	s.SelectorKeys = slices.Clone(s.SelectorKeys)
	s.Roles = slices.Clone(s.Roles)
	s.SatisfiedBy = slices.Clone(s.SatisfiedBy)
	s.Closure = slices.Clone(s.Closure)
	s.SubScopes = slices.Clone(s.SubScopes)
	return s
}

// RuntimeScope is one entry of the agent runtime projection.
type RuntimeScope struct {
	// Name is the stored scope.
	Name string

	// Lifecycle is whether the scope is active or retired.
	Lifecycle RuntimeLifecycle

	// SafeSince is the runtime registry version from which the scope is safe in
	// a delegated policy; 0 means never.
	SafeSince int
}

// Fact is a compiled resource fact.
type Fact struct {
	// Name is the Attr or Derived name.
	Name string

	// Type is the fact type: a built-in fact type or a declared type name.
	Type string

	// DerivedFrom is the fact a Derived projection reads, or "" for an Attr.
	DerivedFrom string
}

// PermissionDefinition is a compiled permission.
type PermissionDefinition struct {
	// Type is the declared type that owns the permission.
	Type string

	// Name is the permission name.
	Name string

	// Description is the optional catalogue text.
	Description string

	// Expr is the resolved expression.
	Expr Expr

	// ExcludedBy lists the blocking scopes applied with block precedence, in
	// declaration order.
	ExcludedBy []string

	// Checks is the stored scope whose authz.Check this permission answers, or
	// "".
	Checks string

	// Facts lists the type's own facts the permission may read, sorted.
	Facts []string

	// Conditions lists the pure conditions any reached relation applies,
	// sorted.
	Conditions []string

	// Scopes lists every stored scope the permission may read, including
	// blocks, sorted.
	Scopes []string

	// Traverses lists the permissions on other types reached through Via, in
	// first-reached order.
	Traverses []PermissionRef
}

func (p PermissionDefinition) clone() PermissionDefinition {
	p.ExcludedBy = slices.Clone(p.ExcludedBy)
	p.Facts = slices.Clone(p.Facts)
	p.Conditions = slices.Clone(p.Conditions)
	p.Scopes = slices.Clone(p.Scopes)
	p.Traverses = slices.Clone(p.Traverses)
	return p
}

// AlternativeSet is a compiled named list of complete permission checks.
type AlternativeSet struct {
	// Type is the declared type that owns the alternatives.
	Type string

	// Name is the alternatives name.
	Name string

	// Options lists the complete permissions in declaration order.
	Options []PermissionRef

	// VetoBlocks rejects the request when any option's block matches in any
	// policy.
	VetoBlocks bool
}

// TypeDefinition is a compiled resource type.
type TypeDefinition struct {
	// Name is the declared type name.
	Name string

	// Parent is the containing type, or "".
	Parent string

	// ResourceKind is the selector resource_kind of the type's stored grants.
	ResourceKind string

	// Facts lists the type's Attr and Derived facts in declaration order.
	Facts []Fact

	// Guards lists the type's guards in declaration order.
	Guards []string

	// Permissions lists the type's permissions in declaration order.
	Permissions []string

	// Alternatives lists the type's named alternatives in declaration order.
	Alternatives []string
}

func (t TypeDefinition) clone() TypeDefinition {
	t.Facts = slices.Clone(t.Facts)
	t.Guards = slices.Clone(t.Guards)
	t.Permissions = slices.Clone(t.Permissions)
	t.Alternatives = slices.Clone(t.Alternatives)
	return t
}

// Registry is the immutable result of Compile. Every accessor returns a copy.
type Registry struct {
	types        []TypeDefinition
	typeIndex    map[string]int
	guards       map[string]map[string]Cond
	scopes       []ScopeDefinition
	scopeIndex   map[string]int
	permissions  []PermissionDefinition
	permIndex    map[PermissionRef]int
	checks       map[string]PermissionRef
	alternatives map[PermissionRef]AlternativeSet
	runtime      []RuntimeScope
}

// Types returns every compiled type in declaration order.
func (r *Registry) Types() []TypeDefinition {
	out := make([]TypeDefinition, len(r.types))
	for i, t := range r.types {
		out[i] = t.clone()
	}
	return out
}

// Type returns one compiled type.
func (r *Registry) Type(name string) (TypeDefinition, bool) {
	i, ok := r.typeIndex[name]
	if !ok {
		var zero TypeDefinition
		return zero, false
	}
	return r.types[i].clone(), true
}

// Guard returns a type's named guard condition.
func (r *Registry) Guard(typeName, guard string) (Cond, bool) {
	cond, ok := r.guards[typeName][guard]
	return cond, ok
}

// Scopes returns every declared stored scope, sorted by name.
func (r *Registry) Scopes() []ScopeDefinition {
	out := make([]ScopeDefinition, len(r.scopes))
	for i, s := range r.scopes {
		out[i] = s.clone()
	}
	return out
}

// Scope returns one declared stored scope.
func (r *Registry) Scope(name string) (ScopeDefinition, bool) {
	i, ok := r.scopeIndex[name]
	if !ok {
		var zero ScopeDefinition
		return zero, false
	}
	return r.scopes[i].clone(), true
}

// Permission returns one compiled permission.
func (r *Registry) Permission(ref PermissionRef) (PermissionDefinition, bool) {
	i, ok := r.permIndex[ref]
	if !ok {
		var zero PermissionDefinition
		return zero, false
	}
	return r.permissions[i].clone(), true
}

// PermissionForCheck returns the permission that answers an authz.Check for
// a stored allow scope.
func (r *Registry) PermissionForCheck(scope string) (PermissionRef, bool) {
	ref, ok := r.checks[scope]
	return ref, ok
}

// Alternatives returns one compiled named alternatives set. ref.Type is the
// declaring type and ref.Permission the alternatives name.
func (r *Registry) Alternatives(ref PermissionRef) (AlternativeSet, bool) {
	a, ok := r.alternatives[ref]
	if !ok {
		var zero AlternativeSet
		return zero, false
	}
	a.Options = slices.Clone(a.Options)
	return a, true
}

// RoleDefaults returns the scopes a system role holds by default, sorted.
func (r *Registry) RoleDefaults(role string) []string {
	var out []string
	for _, s := range r.scopes {
		if slices.Contains(s.Roles, role) {
			out = append(out, s.Name)
		}
	}
	return out
}

// RuntimeScopes returns the agent runtime projection: every declared scope
// and every retired runtime scope, sorted by name.
func (r *Registry) RuntimeScopes() []RuntimeScope {
	return slices.Clone(r.runtime)
}

// build turns validated declarations into the immutable Registry and derives
// its projections.
func (c *compiler) build() *Registry {
	reg := &Registry{
		types:        nil,
		typeIndex:    make(map[string]int),
		guards:       make(map[string]map[string]Cond),
		scopes:       nil,
		scopeIndex:   make(map[string]int),
		permissions:  nil,
		permIndex:    make(map[PermissionRef]int),
		checks:       make(map[string]PermissionRef),
		alternatives: make(map[PermissionRef]AlternativeSet),
		runtime:      nil,
	}
	scopes := make(map[string]ScopeDefinition)
	for i := range c.types {
		t := &c.types[i]
		if c.byName[t.name] != t {
			continue
		}
		m := c.members(t)
		reg.typeIndex[t.name] = len(reg.types)
		reg.types = append(reg.types, compileType(t))
		reg.guards[t.name] = make(map[string]Cond, len(t.guards))
		for _, g := range t.guards {
			reg.guards[t.name][g.name] = g.cond
		}
		for _, r := range t.relations {
			scopes[r.storedAs] = compileScope(t, r)
		}
		for _, p := range t.permissions {
			ref := PermissionRef{Type: t.name, Permission: p.name}
			reg.permIndex[ref] = len(reg.permissions)
			reg.permissions = append(reg.permissions, c.compilePermission(t, m, p))
			if p.checks != "" {
				reg.checks[p.checks] = ref
			}
		}
		for _, a := range t.alternatives {
			reg.alternatives[PermissionRef{Type: t.name, Permission: a.name}] = AlternativeSet{
				Type:       t.name,
				Name:       a.name,
				Options:    slices.Clone(a.options),
				VetoBlocks: a.veto,
			}
		}
	}
	c.projectChecks(reg, scopes)
	projectClosures(scopes)
	names := make([]string, 0, len(scopes))
	for name := range scopes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		reg.scopeIndex[name] = len(reg.scopes)
		reg.scopes = append(reg.scopes, scopes[name])
	}
	reg.runtime = c.projectRuntime(reg.scopes)
	return reg
}

func compileType(t *TypeDecl) TypeDefinition {
	out := TypeDefinition{
		Name:         t.name,
		Parent:       t.parent,
		ResourceKind: resourceKind(t),
		Facts:        nil,
		Guards:       nil,
		Permissions:  nil,
		Alternatives: nil,
	}
	for _, a := range t.attrs {
		out.Facts = append(out.Facts, Fact{Name: a.name, Type: a.factType, DerivedFrom: ""})
	}
	for _, d := range t.derived {
		out.Facts = append(out.Facts, Fact{Name: d.name, Type: d.projection.principalType, DerivedFrom: d.projection.fact})
	}
	for _, g := range t.guards {
		out.Guards = append(out.Guards, g.name)
	}
	for _, p := range t.permissions {
		out.Permissions = append(out.Permissions, p.name)
	}
	for _, a := range t.alternatives {
		out.Alternatives = append(out.Alternatives, a.name)
	}
	return out
}

func compileScope(t *TypeDecl, r relationDecl) ScopeDefinition {
	keys := mergeKeys(t.selectorKeys, r.selectorKeys)
	if len(keys) == 0 {
		keys = nil
	}
	roles := slices.Clone(r.roles)
	slices.Sort(roles)
	roles = slices.Compact(roles)
	matching := MatchingPermissive
	if r.strict {
		matching = MatchingStrict
	}
	visibility := VisibilityUserVisible
	if r.internal {
		visibility = VisibilityInternal
	}
	anchors := slices.Clone(r.anchors)
	if len(anchors) == 0 {
		anchors = []string{Self}
	}
	return ScopeDefinition{
		Name:             r.storedAs,
		Type:             t.name,
		Relation:         r.name,
		ResourceKind:     resourceKind(t),
		Block:            r.kind == relationBlock,
		Matching:         matching,
		Visibility:       visibility,
		Description:      r.description,
		Sensitive:        r.sensitive,
		WrittenBy:        cmp.Or(r.writtenBy, t.writtenBy),
		Anchors:          anchors,
		Conditions:       slices.Clone(r.conditions),
		SelectorKeys:     keys,
		Roles:            roles,
		RuntimeSafeSince: r.runtimeSince,
		SatisfiedBy:      nil,
		Exclusion:        "",
		Closure:          nil,
		SubScopes:        nil,
	}
}

func (c *compiler) compilePermission(t *TypeDecl, m members, p permissionDecl) PermissionDefinition {
	out := PermissionDefinition{
		Type:        t.name,
		Name:        p.name,
		Description: p.description,
		Expr:        c.resolve(t, m, p.expr),
		ExcludedBy:  nil,
		Checks:      p.checks,
		Facts:       nil,
		Conditions:  nil,
		Scopes:      nil,
		Traverses:   nil,
	}
	w := prerequisiteWalker{c: c, t: t, m: m, out: &out, visiting: []string{p.name}}
	w.walk(p.expr)
	for _, name := range p.excludedBy {
		r := m.relations[name]
		out.ExcludedBy = append(out.ExcludedBy, r.storedAs)
		out.Scopes = append(out.Scopes, r.storedAs)
		out.Conditions = append(out.Conditions, r.conditions...)
	}
	out.Facts = sortedUnique(out.Facts)
	out.Conditions = sortedUnique(out.Conditions)
	out.Scopes = sortedUnique(out.Scopes)
	return out
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}

// resolve copies e, recording each Rel's stored scope and each Via's target
// type.
func (c *compiler) resolve(t *TypeDecl, m members, e Expr) Expr {
	out := e
	out.operands = nil
	switch e.op {
	case OpRel:
		out.scope = m.relations[e.name].storedAs
	case OpVia:
		out.target = m.attrs[e.name].factType
	case OpUnion, OpMinus, OpWhen:
		out.operands = make([]Expr, len(e.operands))
		for i, op := range e.operands {
			out.operands[i] = c.resolve(t, m, op)
		}
	case OpIs, OpPerm:
	}
	return out
}

// prerequisiteWalker collects the facts, conditions, scopes and traversals a
// permission may need, following same-type Perm references.
type prerequisiteWalker struct {
	c        *compiler
	t        *TypeDecl
	m        members
	out      *PermissionDefinition
	visiting []string
}

func (w *prerequisiteWalker) walk(e Expr) {
	switch e.op {
	case OpRel:
		r := w.m.relations[e.name]
		w.out.Scopes = append(w.out.Scopes, r.storedAs)
		w.out.Conditions = append(w.out.Conditions, r.conditions...)
	case OpIs:
		w.addFact(e.name)
	case OpVia:
		w.addFact(e.name)
		ref := PermissionRef{Type: w.m.attrs[e.name].factType, Permission: e.permission}
		if !slices.Contains(w.out.Traverses, ref) {
			w.out.Traverses = append(w.out.Traverses, ref)
		}
	case OpPerm:
		if slices.Contains(w.visiting, e.name) {
			return
		}
		p := w.m.perms[e.name]
		w.visiting = append(w.visiting, e.name)
		w.walk(p.expr)
		for _, name := range p.excludedBy {
			r := w.m.relations[name]
			w.out.Scopes = append(w.out.Scopes, r.storedAs)
			w.out.Conditions = append(w.out.Conditions, r.conditions...)
		}
	case OpWhen:
		w.addCondFacts(w.m.guards[e.guard].cond)
		for _, op := range e.operands {
			w.walk(op)
		}
	case OpUnion, OpMinus:
		for _, op := range e.operands {
			w.walk(op)
		}
	}
}

func (w *prerequisiteWalker) addFact(name string) {
	w.out.Facts = append(w.out.Facts, name)
	if d, ok := w.m.derived[name]; ok {
		w.out.Facts = append(w.out.Facts, d.projection.fact)
	}
}

func (w *prerequisiteWalker) addCondFacts(cond Cond) {
	if cond.op == CondAll {
		for _, op := range cond.operands {
			w.addCondFacts(op)
		}
		return
	}
	w.addFact(cond.fact)
}

// projectChecks derives the legacy authz.Check projection: which other scopes
// satisfy a check for each scope and which block directly excludes it.
func (c *compiler) projectChecks(reg *Registry, scopes map[string]ScopeDefinition) {
	broaderByBlock := make(map[string][]string)
	for _, p := range reg.permissions {
		if p.Checks == "" {
			continue
		}
		t := c.byName[p.Type]
		m := c.members(t)
		decl := t.permissions[slices.IndexFunc(t.permissions, func(d permissionDecl) bool { return d.name == p.Name })]

		s := scopes[p.Checks]
		var satisfied []string
		for _, name := range satisfyingRelations(m, decl.expr, []string{decl.name}) {
			if scope := m.relations[name].storedAs; scope != p.Checks {
				satisfied = append(satisfied, scope)
			}
		}
		s.SatisfiedBy = sortedUnique(satisfied)
		switch {
		case len(p.ExcludedBy) > 0:
			s.Exclusion = p.ExcludedBy[0]
		case decl.expr.op == OpMinus && decl.expr.operands[1].op == OpRel:
			s.Exclusion = m.relations[decl.expr.operands[1].name].storedAs
		}
		scopes[p.Checks] = s

		if len(p.ExcludedBy) == 0 {
			continue
		}
		direct := scopes[p.ExcludedBy[0]]
		broader := sortedUnique(p.ExcludedBy[1:])
		if previous, seen := broaderByBlock[direct.Name]; seen && !slices.Equal(previous, broader) {
			c.problemf("type %q: block %q directly excludes several permissions with different broader blocks (%v and %v)", p.Type, direct.Relation, previous, broader)
			continue
		}
		broaderByBlock[direct.Name] = broader
		direct.SatisfiedBy = broader
		scopes[p.ExcludedBy[0]] = direct
	}
}

// projectClosures derives each scope's containment closure and catalogue
// sub-scopes from the one-step check projection.
func projectClosures(scopes map[string]ScopeDefinition) {
	for name, s := range scopes {
		seen := map[string]bool{name: true}
		queue := []string{name}
		for len(queue) > 0 {
			granted := queue[0]
			queue = queue[1:]
			for checked, other := range scopes {
				if seen[checked] || !slices.Contains(other.SatisfiedBy, granted) {
					continue
				}
				seen[checked] = true
				queue = append(queue, checked)
			}
		}
		closure := make([]string, 0, len(seen))
		for implied := range seen {
			closure = append(closure, implied)
		}
		slices.Sort(closure)
		s.Closure = closure
		scopes[name] = s
	}
	for lower, s := range scopes {
		if s.Visibility != VisibilityUserVisible {
			continue
		}
		for _, higher := range s.SatisfiedBy {
			h := scopes[higher]
			if h.Visibility != VisibilityUserVisible {
				continue
			}
			h.SubScopes = append(h.SubScopes, lower)
			scopes[higher] = h
		}
	}
	for name, s := range scopes {
		s.SubScopes = sortedUnique(s.SubScopes)
		scopes[name] = s
	}
}

func (c *compiler) projectRuntime(scopes []ScopeDefinition) []RuntimeScope {
	out := make([]RuntimeScope, 0, len(scopes)+len(c.cfg.RetiredRuntimeScopes))
	for _, s := range scopes {
		out = append(out, RuntimeScope{Name: s.Name, Lifecycle: RuntimeActive, SafeSince: s.RuntimeSafeSince})
	}
	seen := make(map[string]bool, len(c.cfg.RetiredRuntimeScopes))
	for _, name := range c.cfg.RetiredRuntimeScopes {
		switch {
		case seen[name]:
			c.problemf("config: retired runtime scope %q listed twice", name)
			continue
		case slices.ContainsFunc(scopes, func(s ScopeDefinition) bool { return s.Name == name }):
			c.problemf("config: retired runtime scope %q is still declared", name)
			continue
		}
		seen[name] = true
		out = append(out, RuntimeScope{Name: name, Lifecycle: RuntimeRetired, SafeSince: 0})
	}
	slices.SortFunc(out, func(a, b RuntimeScope) int { return strings.Compare(a.Name, b.Name) })
	return out
}

package schema

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Built-in fact types accepted by Attr. Any declared type name is also a fact
// type: a reference Via can follow.
const (
	FactString            = "string"
	FactNullableString    = "nullable_string"
	FactBool              = "bool"
	FactNullableTimestamp = "nullable_timestamp"
	FactUser              = "user"
)

// Reserved selector keys every stored selector carries. SelectorKeys lists
// only the keys beyond these.
const (
	selectorKeyResourceKind = "resource_kind"
	selectorKeyResourceID   = "resource_id"
)

// blockedActionPrefix starts the action of every stored blocking scope, so
// blocks stay recognisable as blocked_* scopes in storage.
const blockedActionPrefix = "blocked_"

// Config supplies the closed vocabularies declarations may reference.
type Config struct {
	// Roles lists the system roles GrantSpec.DefaultFor may name.
	Roles []string

	// GrantSurfaces lists the grant surfaces GrantSurface may name.
	GrantSurfaces []string

	// Conditions lists the pure conditions a spec's Conditions may name.
	Conditions []Condition

	// RetiredRuntimeScopes lists stored scopes no type declares any more but
	// that delegated runtime policies may still carry. They project as retired
	// runtime scopes.
	RetiredRuntimeScopes []string
}

// Condition registers a pure condition relations can require.
type Condition struct {
	// Name is the name Conditions refers to.
	Name string

	// SelectorKeys lists the selector keys the condition reads. Every relation
	// that uses the condition must accept them.
	SelectorKeys []string
}

// CompileError lists every problem Compile found.
type CompileError struct {
	// Problems describes each invalid declaration, prefixed with its location.
	Problems []string
}

func (e *CompileError) Error() string {
	return "invalid authorization schema:\n  " + strings.Join(e.Problems, "\n  ")
}

type compiler struct {
	cfg        Config
	types      []TypeDecl
	byName     map[string]*TypeDecl
	conditions map[string]Condition
	scopeOwner map[string]string
	problems   []string
}

func (c *compiler) problemf(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// Compile validates the declarations and returns an immutable Registry. It
// reports every problem it finds in one *CompileError.
func Compile(cfg Config, types ...TypeDecl) (*Registry, error) {
	c := &compiler{
		cfg:        cfg,
		types:      types,
		byName:     make(map[string]*TypeDecl, len(types)),
		conditions: make(map[string]Condition, len(cfg.Conditions)),
		scopeOwner: make(map[string]string),
		problems:   nil,
	}
	c.validateConfig()
	c.types = slices.Clone(c.types)
	c.indexTypes()
	for i := range c.types {
		c.validateType(&c.types[i])
	}
	if len(c.problems) == 0 {
		c.validatePermissionCycles()
	}
	if len(c.problems) > 0 {
		return nil, &CompileError{Problems: c.problems}
	}
	c.expandExclusions()
	reg := c.build()
	if len(c.problems) > 0 {
		return nil, &CompileError{Problems: c.problems}
	}
	return reg, nil
}

func (c *compiler) validateConfig() {
	for _, cond := range c.cfg.Conditions {
		if cond.Name == "" {
			c.problemf("config: condition with empty name")
			continue
		}
		if _, dup := c.conditions[cond.Name]; dup {
			c.problemf("config: condition %q registered twice", cond.Name)
			continue
		}
		c.conditions[cond.Name] = cond
	}
}

func (c *compiler) indexTypes() {
	kinds := make(map[string]string, len(c.types))
	for i := range c.types {
		t := &c.types[i]
		switch {
		case t.name == "":
			c.problemf("type #%d: empty name", i+1)
			continue
		case t.name == Self || isBuiltinFactType(t.name):
			c.problemf("type %q: name is reserved", t.name)
			continue
		}
		if _, dup := c.byName[t.name]; dup {
			c.problemf("type %q: declared twice", t.name)
			continue
		}
		c.byName[t.name] = t
		kind := resourceKind(t)
		if other, dup := kinds[kind]; dup {
			c.problemf("type %q: resource kind %q is already used by type %q", t.name, kind, other)
			continue
		}
		kinds[kind] = t.name
	}
}

func resourceKind(t *TypeDecl) string {
	if t.storedKind != "" {
		return t.storedKind
	}
	return t.name
}

func isBuiltinFactType(name string) bool {
	switch name {
	case FactString, FactNullableString, FactBool, FactNullableTimestamp, FactUser:
		return true
	default:
		return false
	}
}

// members indexes one type's names, which share a single namespace.
type members struct {
	attrs     map[string]attrDecl
	derived   map[string]derivedDecl
	guards    map[string]guardDecl
	relations map[string]relationDecl
	perms     map[string]permissionDecl
}

func (c *compiler) members(t *TypeDecl) members {
	m := members{
		attrs:     make(map[string]attrDecl),
		derived:   make(map[string]derivedDecl),
		guards:    make(map[string]guardDecl),
		relations: make(map[string]relationDecl),
		perms:     make(map[string]permissionDecl),
	}
	seen := make(map[string]string)
	claim := func(kind, name string) bool {
		if name == "" {
			c.problemf("type %q: %s with empty name", t.name, kind)
			return false
		}
		if other, dup := seen[name]; dup {
			c.problemf("type %q: %s %q reuses the name of a %s", t.name, kind, name, other)
			return false
		}
		seen[name] = kind
		return true
	}
	for _, a := range t.attrs {
		if claim("attr", a.name) {
			m.attrs[a.name] = a
		}
	}
	for _, d := range t.derived {
		if claim("derived fact", d.name) {
			m.derived[d.name] = d
		}
	}
	for _, g := range t.guards {
		if claim("guard", g.name) {
			m.guards[g.name] = g
		}
	}
	for _, r := range t.relations {
		if claim(string(r.kind), r.name) {
			m.relations[r.name] = r
		}
	}
	for _, p := range t.permissions {
		if claim("permission", p.name) {
			m.perms[p.name] = p
		}
	}
	for _, a := range t.alternatives {
		claim("alternatives", a.name)
	}
	return m
}

func (m members) factType(name string) (string, bool) {
	if a, ok := m.attrs[name]; ok {
		return a.factType, true
	}
	if d, ok := m.derived[name]; ok {
		return d.projection.principalType, true
	}
	return "", false
}

func (c *compiler) validateType(t *TypeDecl) {
	if c.byName[t.name] != t {
		return
	}
	m := c.members(t)
	c.validateParent(t)
	c.validateFacts(t, m)
	c.validateRelations(t)
	c.validatePermissions(t, m)
	c.validateAlternatives(t, m)
}

func (c *compiler) validateParent(t *TypeDecl) {
	if t.parent == "" {
		return
	}
	if _, ok := c.byName[t.parent]; !ok {
		c.problemf("type %q: parent %q is not a declared type", t.name, t.parent)
		return
	}
	seen := map[string]bool{t.name: true}
	for p := t.parent; p != ""; {
		if seen[p] {
			c.problemf("type %q: parent chain forms a cycle through %q", t.name, p)
			return
		}
		seen[p] = true
		next, ok := c.byName[p]
		if !ok {
			return
		}
		p = next.parent
	}
}

func (c *compiler) ancestors(t *TypeDecl) []string {
	var out []string
	seen := map[string]bool{t.name: true}
	for p := t.parent; p != "" && !seen[p]; {
		seen[p] = true
		out = append(out, p)
		next, ok := c.byName[p]
		if !ok {
			break
		}
		p = next.parent
	}
	return out
}

func (c *compiler) validateFacts(t *TypeDecl, m members) {
	for _, a := range t.attrs {
		if !isBuiltinFactType(a.factType) {
			if _, ok := c.byName[a.factType]; !ok {
				c.problemf("type %q: attr %q has unknown fact type %q", t.name, a.name, a.factType)
			}
		}
	}
	for _, d := range t.derived {
		if d.projection.principalType != FactUser {
			c.problemf("type %q: derived fact %q projects to principal type %q; only %q is supported", t.name, d.name, d.projection.principalType, FactUser)
		}
		a, ok := m.attrs[d.projection.fact]
		switch {
		case !ok:
			c.problemf("type %q: derived fact %q reads unknown attr %q", t.name, d.name, d.projection.fact)
		case a.factType != FactString && a.factType != FactNullableString:
			c.problemf("type %q: derived fact %q reads attr %q of type %q; it needs a string", t.name, d.name, a.name, a.factType)
		}
	}
	for _, g := range t.guards {
		c.validateCond(t, m, fmt.Sprintf("guard %q", g.name), g.cond)
	}
}

func (c *compiler) validateCond(t *TypeDecl, m members, where string, cond Cond) {
	if cond.op == CondAll {
		if len(cond.operands) == 0 {
			c.problemf("type %q: %s: All needs at least one condition", t.name, where)
		}
		for _, op := range cond.operands {
			c.validateCond(t, m, where, op)
		}
		return
	}
	if cond.op == "" {
		c.problemf("type %q: %s: empty condition", t.name, where)
		return
	}
	factType, ok := m.factType(cond.fact)
	if !ok {
		c.problemf("type %q: %s: %s reads unknown fact %q", t.name, where, cond.op, cond.fact)
		return
	}
	var compatible bool
	switch cond.op {
	case CondIsNull:
		compatible = factType == FactNullableString || factType == FactNullableTimestamp
	case CondIsFalse:
		compatible = factType == FactBool
	case CondNullOrEmpty:
		compatible = factType == FactString || factType == FactNullableString
	case CondPresent:
		compatible = true
	case CondAll:
		compatible = true
	}
	if !compatible {
		c.problemf("type %q: %s: %s cannot read fact %q of type %q", t.name, where, cond.op, cond.fact, factType)
	}
}

func (c *compiler) validateRelations(t *TypeDecl) {
	typeKeys, ok := c.validateSelectorKeys(t.selectorKeys)
	if !ok {
		c.problemf("type %q: selector keys %v must be distinct and exclude %s and %s", t.name, t.selectorKeys, selectorKeyResourceKind, selectorKeyResourceID)
	}
	ancestors := c.ancestors(t)
	for _, r := range t.relations {
		where := fmt.Sprintf("type %q: %s %q", t.name, r.kind, r.name)
		c.validateStoredScope(where, r)
		relKeys, ok := c.validateSelectorKeys(r.selectorKeys)
		if !ok {
			c.problemf("%s: selector keys %v must be distinct and exclude %s and %s", where, r.selectorKeys, selectorKeyResourceKind, selectorKeyResourceID)
		}
		keys := mergeKeys(typeKeys, relKeys)
		seenAnchors := make(map[string]bool, len(r.anchors))
		for _, anchor := range r.anchors {
			switch {
			case seenAnchors[anchor]:
				c.problemf("%s: anchor %q listed twice", where, anchor)
			case anchor != Self && !slices.Contains(ancestors, anchor):
				c.problemf("%s: anchor %q is neither %s nor a type on the parent chain %v", where, anchor, Self, ancestors)
			}
			seenAnchors[anchor] = true
		}
		for _, name := range r.conditions {
			cond, ok := c.conditions[name]
			if !ok {
				c.problemf("%s: condition %q is not registered", where, name)
				continue
			}
			for _, key := range cond.SelectorKeys {
				if !slices.Contains(keys, key) {
					c.problemf("%s: condition %q reads selector key %q, which the relation does not accept", where, name, key)
				}
			}
		}
		surface := cmp.Or(r.grantSurface, t.grantSurface)
		switch {
		case surface == "":
			c.problemf("%s: no GrantSurface on the relation or its type", where)
		case !slices.Contains(c.cfg.GrantSurfaces, surface):
			c.problemf("%s: grant surface %q is not registered", where, surface)
		}
		for _, role := range r.roles {
			if !slices.Contains(c.cfg.Roles, role) {
				c.problemf("%s: role %q is not registered", where, role)
			}
		}
		if r.delegatable < 0 {
			c.problemf("%s: DelegatableSince version %d is negative", where, r.delegatable)
		}
		if r.kind == relationBlock {
			c.validateImpliedBy(t, where, r)
			continue
		}
		if !r.internal && r.description == "" {
			c.problemf("%s: user-visible grant needs a Description", where)
		}
	}
}

// validateImpliedBy checks that a block is implied only by other blocks of
// its own type, and that implications form no cycle.
func (c *compiler) validateImpliedBy(t *TypeDecl, where string, r relationDecl) {
	if dup := firstDuplicate(r.impliedBy); dup != "" {
		c.problemf("%s: ImpliedBy lists block %q twice", where, dup)
	}
	for _, name := range r.impliedBy {
		i := slices.IndexFunc(t.relations, func(other relationDecl) bool { return other.name == name })
		switch {
		case name == r.name:
			c.problemf("%s: ImpliedBy names the block itself", where)
		case i < 0:
			c.problemf("%s: ImpliedBy names unknown block %q", where, name)
		case t.relations[i].kind != relationBlock:
			c.problemf("%s: ImpliedBy names grant %q; it needs a Block", where, name)
		}
	}
	if slices.Contains(impliersOf(t, r.name), r.name) {
		c.problemf("%s: ImpliedBy forms a cycle back to %q", where, r.name)
	}
}

// impliersOf lists every block that implies block, directly or through other
// blocks, sorted. It stops at cycles, which validateImpliedBy reports.
func impliersOf(t *TypeDecl, block string) []string {
	seen := make(map[string]bool)
	queue := []string{block}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		i := slices.IndexFunc(t.relations, func(r relationDecl) bool { return r.name == name && r.kind == relationBlock })
		if i < 0 {
			continue
		}
		for _, implier := range t.relations[i].impliedBy {
			if !seen[implier] {
				seen[implier] = true
				queue = append(queue, implier)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// expandExclusions resolves each permission's direct block into the full set
// that excludes it: the direct block first, then every block implying it.
// It runs after validation, on the compiler's own copies of the declarations.
func (c *compiler) expandExclusions() {
	for i := range c.types {
		t := &c.types[i]
		t.permissions = slices.Clone(t.permissions)
		for j := range t.permissions {
			p := &t.permissions[j]
			p.excludedBy = nil
			for _, direct := range p.direct {
				p.excludedBy = append(p.excludedBy, direct)
				p.excludedBy = append(p.excludedBy, impliersOf(t, direct)...)
			}
		}
	}
}

func (c *compiler) validateStoredScope(where string, r relationDecl) {
	if r.storedAs == "" {
		c.problemf("%s: missing stored scope", where)
		return
	}
	resource, action, ok := strings.Cut(r.storedAs, ":")
	if !ok || resource == "" || action == "" || strings.Contains(action, ":") {
		c.problemf("%s: stored scope %q must have the form <resource>:<action>", where, r.storedAs)
		return
	}
	if owner, dup := c.scopeOwner[r.storedAs]; dup {
		c.problemf("%s: stored scope %q is already declared by %s", where, r.storedAs, owner)
		return
	}
	c.scopeOwner[r.storedAs] = where
	blocked := strings.HasPrefix(action, blockedActionPrefix)
	switch {
	case action == blockedActionPrefix:
		c.problemf("%s: stored scope %q names no action after %s", where, r.storedAs, blockedActionPrefix)
	case r.kind == relationBlock && !blocked:
		c.problemf("%s: stored scope %q must be a %s* scope", where, r.storedAs, blockedActionPrefix)
	case r.kind == relationGrant && blocked:
		c.problemf("%s: stored scope %q is a blocking scope; declare it with Block", where, r.storedAs)
	}
}

func (c *compiler) validateSelectorKeys(keys []string) ([]string, bool) {
	ok := true
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if key == "" || key == selectorKeyResourceKind || key == selectorKeyResourceID || seen[key] {
			ok = false
		}
		seen[key] = true
	}
	out := slices.Clone(keys)
	slices.Sort(out)
	return slices.Compact(out), ok
}

func mergeKeys(a, b []string) []string {
	out := append(slices.Clone(a), b...)
	slices.Sort(out)
	return slices.Compact(out)
}

func (c *compiler) validatePermissions(t *TypeDecl, m members) {
	checked := make(map[string]string)
	for _, p := range t.permissions {
		where := fmt.Sprintf("type %q: permission %q", t.name, p.name)
		c.validateExpr(t, m, where, p.expr)
		if len(p.direct) > 1 {
			c.problemf("%s: ExcludedBy is given more than once; declare broader blocks with BlockSpec.ImpliedBy", where)
		}
		if len(p.alsoGrantedBy) > 0 {
			c.problemf("%s: AlsoGrantedBy applies only to ScopePermission; write the Union in the expression", where)
		}
		if p.anyBlockVetoes {
			c.problemf("%s: AnyBlockVetoes applies only to an AnyOf permission", where)
		}
		for _, name := range p.direct {
			r, ok := m.relations[name]
			switch {
			case !ok:
				c.problemf("%s: ExcludedBy names unknown block %q", where, name)
			case r.kind != relationBlock:
				c.problemf("%s: ExcludedBy names grant %q; it needs a Block", where, name)
			}
		}
		if p.answers == "" {
			continue
		}
		if other, dup := checked[p.answers]; dup {
			c.problemf("%s: scope %q is already answered by permission %q", where, p.answers, other)
			continue
		}
		checked[p.answers] = p.name
		var relation string
		for _, r := range t.relations {
			if r.storedAs == p.answers && r.kind == relationGrant {
				relation = r.name
			}
		}
		if relation == "" {
			c.problemf("%s: AnswersScope names %q, which is not a grant of this type", where, p.answers)
			continue
		}
		if !grantOnly(m, p.expr, nil) {
			c.problemf("%s: a permission that answers a scope may use only Rel, Union, Minus and grant-only Perm", where)
			continue
		}
		if !slices.Contains(satisfyingRelations(m, p.expr, nil), relation) {
			c.problemf("%s: AnswersScope %q but no Rel(%q) satisfies it", where, p.answers, relation)
		}
	}
	for _, r := range t.relations {
		if r.kind != relationGrant || r.storedAs == "" {
			continue
		}
		if _, ok := checked[r.storedAs]; !ok {
			c.problemf("type %q: grant %q: no permission answers its scope %q", t.name, r.name, r.storedAs)
		}
	}
}

func firstDuplicate(values []string) string {
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return v
		}
		seen[v] = true
	}
	return ""
}

func (c *compiler) validateExpr(t *TypeDecl, m members, where string, e Expr) {
	switch e.op {
	case OpRel:
		r, ok := m.relations[e.name]
		switch {
		case !ok:
			c.problemf("%s: Rel names unknown relation %q", where, e.name)
		case r.kind == relationBlock:
			c.problemf("%s: Rel names block %q; apply blocks with ExcludedBy", where, e.name)
		}
	case OpUnion:
		if len(e.operands) == 0 {
			c.problemf("%s: Union needs at least one branch", where)
		}
		for _, op := range e.operands {
			c.validateExpr(t, m, where, op)
		}
	case OpMinus:
		for _, op := range e.operands {
			c.validateExpr(t, m, where, op)
		}
	case OpIs:
		factType, ok := m.factType(e.name)
		switch {
		case !ok:
			c.problemf("%s: Is names unknown fact %q", where, e.name)
		case factType != FactUser:
			c.problemf("%s: Is needs a %q fact, but %q is %q", where, FactUser, e.name, factType)
		}
	case OpVia:
		a, ok := m.attrs[e.name]
		if !ok {
			c.problemf("%s: Via names unknown attr %q", where, e.name)
			return
		}
		target, ok := c.byName[a.factType]
		if !ok {
			c.problemf("%s: Via attr %q has type %q, which is not a declared type", where, e.name, a.factType)
			return
		}
		if !slices.ContainsFunc(target.permissions, func(p permissionDecl) bool { return p.name == e.permission }) {
			c.problemf("%s: Via names permission %q, which type %q does not declare", where, e.permission, target.name)
		}
	case OpPerm:
		if _, ok := m.perms[e.name]; !ok {
			c.problemf("%s: Perm names unknown permission %q", where, e.name)
		}
	case OpAnyOf:
		c.problemf("%s: AnyOf must be the permission's whole expression", where)
	case OpWhen:
		if _, ok := m.guards[e.guard]; !ok {
			c.problemf("%s: When names unknown guard %q", where, e.guard)
		}
		for _, op := range e.operands {
			c.validateExpr(t, m, where, op)
		}
	default:
		c.problemf("%s: missing expression", where)
	}
}

// grantOnly reports whether e reads only grants, through Rel, Union, Minus and
// Perm references that are themselves grant-only.
func grantOnly(m members, e Expr, visiting []string) bool {
	switch e.op {
	case OpRel:
		return true
	case OpUnion, OpMinus:
		for _, op := range e.operands {
			if !grantOnly(m, op, visiting) {
				return false
			}
		}
		return true
	case OpPerm:
		p, ok := m.perms[e.name]
		if !ok || slices.Contains(visiting, e.name) {
			return false
		}
		return len(p.direct) == 0 && grantOnly(m, p.expr, append(visiting, e.name))
	case OpIs, OpVia, OpWhen:
		return false
	default:
		return false
	}
}

// satisfyingRelations lists the grants that satisfy e on their own: Rel
// leaves reached through Union, the base of Minus, and same-type Perm.
func satisfyingRelations(m members, e Expr, visiting []string) []string {
	switch e.op {
	case OpRel:
		return []string{e.name}
	case OpUnion:
		var out []string
		for _, op := range e.operands {
			out = append(out, satisfyingRelations(m, op, visiting)...)
		}
		return out
	case OpMinus:
		return satisfyingRelations(m, e.operands[0], visiting)
	case OpPerm:
		p, ok := m.perms[e.name]
		if !ok || slices.Contains(visiting, e.name) {
			return nil
		}
		return satisfyingRelations(m, p.expr, append(visiting, e.name))
	case OpIs, OpVia, OpWhen:
		return nil
	default:
		return nil
	}
}

func (c *compiler) validateAlternatives(t *TypeDecl, m members) {
	for _, a := range t.alternatives {
		where := fmt.Sprintf("type %q: permission %q", t.name, a.name)
		for _, option := range a.misused {
			c.problemf("%s: %s does not apply to AnyOf; put it on the branch's own permission", where, option)
		}
		if len(a.branches) < 2 {
			c.problemf("%s: AnyOf needs at least two branches", where)
		}
		anyBlock := false
		refs := make([]PermissionRef, 0, len(a.branches))
		for _, branch := range a.branches {
			ref, ok := c.anyOfBranch(t, m, where, branch)
			if !ok {
				continue
			}
			refs = append(refs, ref)
			target := c.byName[ref.Type]
			i := slices.IndexFunc(target.permissions, func(p permissionDecl) bool { return p.name == ref.Permission })
			if i < 0 {
				c.problemf("%s: type %q declares no permission %q", where, ref.Type, ref.Permission)
				continue
			}
			if len(target.permissions[i].direct) > 0 {
				anyBlock = true
			}
		}
		if dup := firstDuplicateBranch(a.branches); dup != "" {
			c.problemf("%s: AnyOf lists %s twice", where, dup)
		}
		if a.anyBlockVetoes && !anyBlock && len(refs) == len(a.branches) {
			c.problemf("%s: AnyBlockVetoes needs a branch with ExcludedBy", where)
		}
	}
}

// anyOfBranch resolves one AnyOf branch to the permission it asks for. Perm
// names a permission on t; Via names one on t's parent type, or on the type
// of an Attr of t, which may be t itself asked of another instance.
func (c *compiler) anyOfBranch(t *TypeDecl, m members, where string, branch Expr) (PermissionRef, bool) {
	switch branch.op {
	case OpPerm:
		return PermissionRef{Type: t.name, Permission: branch.name}, true
	case OpVia:
		if _, isAttr := m.attrs[branch.name]; isAttr && branch.name == t.parent {
			c.problemf("%s: Via names %q, which is both the parent type and an Attr; rename the Attr", where, branch.name)
			return PermissionRef{Type: "", Permission: ""}, false
		}
		if a, ok := m.attrs[branch.name]; ok {
			if _, declared := c.byName[a.factType]; !declared {
				c.problemf("%s: Via attr %q has type %q, which is not a declared type", where, branch.name, a.factType)
				return PermissionRef{Type: "", Permission: ""}, false
			}
			return PermissionRef{Type: a.factType, Permission: branch.permission}, true
		}
		if branch.name != "" && branch.name == t.parent {
			if _, declared := c.byName[t.parent]; !declared {
				c.problemf("%s: Via names parent type %q, which is not declared", where, t.parent)
				return PermissionRef{Type: "", Permission: ""}, false
			}
			return PermissionRef{Type: t.parent, Permission: branch.permission}, true
		}
		c.problemf("%s: Via names %q, which is neither the parent type nor an Attr", where, branch.name)
		return PermissionRef{Type: "", Permission: ""}, false
	default:
		c.problemf("%s: an AnyOf branch must be Perm or Via, not %s", where, branch.op)
		return PermissionRef{Type: "", Permission: ""}, false
	}
}

// firstDuplicateBranch names the first branch AnyOf lists twice. The same
// permission reached through different Attrs, or through Perm and an Attr, is
// asked of different instances and is not a duplicate.
func firstDuplicateBranch(branches []Expr) string {
	type branchKey struct {
		op         Op
		name       string
		permission string
	}
	seen := make(map[branchKey]bool, len(branches))
	for _, b := range branches {
		key := branchKey{op: b.op, name: b.name, permission: b.permission}
		if seen[key] {
			if b.op == OpPerm {
				return fmt.Sprintf("Perm(%q)", b.name)
			}
			return fmt.Sprintf("Via(%q, %q)", b.name, b.permission)
		}
		seen[key] = true
	}
	return ""
}

// validatePermissionCycles rejects permissions that reach themselves through
// Perm or Via.
func (c *compiler) validatePermissionCycles() {
	const (
		visiting = iota + 1
		done
	)
	state := make(map[PermissionRef]int)
	var visit func(ref PermissionRef, path []PermissionRef)
	visit = func(ref PermissionRef, path []PermissionRef) {
		switch state[ref] {
		case done:
			return
		case visiting:
			names := make([]string, 0, len(path)+1)
			start := slices.Index(path, ref)
			for _, p := range path[start:] {
				names = append(names, p.Type+"."+p.Permission)
			}
			names = append(names, ref.Type+"."+ref.Permission)
			c.problemf("permission cycle: %s", strings.Join(names, " -> "))
			return
		}
		state[ref] = visiting
		t := c.byName[ref.Type]
		i := slices.IndexFunc(t.permissions, func(p permissionDecl) bool { return p.name == ref.Permission })
		for _, next := range c.permissionEdges(t, t.permissions[i].expr) {
			visit(next, append(path, ref))
		}
		state[ref] = done
	}
	for i := range c.types {
		t := &c.types[i]
		if c.byName[t.name] != t {
			continue
		}
		for _, p := range t.permissions {
			visit(PermissionRef{Type: t.name, Permission: p.name}, nil)
		}
	}
}

func (c *compiler) permissionEdges(t *TypeDecl, e Expr) []PermissionRef {
	switch e.op {
	case OpPerm:
		return []PermissionRef{{Type: t.name, Permission: e.name}}
	case OpVia:
		for _, a := range t.attrs {
			if a.name == e.name {
				return []PermissionRef{{Type: a.factType, Permission: e.permission}}
			}
		}
		return nil
	case OpUnion, OpMinus, OpWhen:
		var out []PermissionRef
		for _, op := range e.operands {
			out = append(out, c.permissionEdges(t, op)...)
		}
		return out
	case OpRel, OpIs:
		return nil
	default:
		return nil
	}
}

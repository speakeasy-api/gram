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
	// Roles lists the system roles DefaultFor may name.
	Roles []string

	// WriteSurfaces lists the grant surfaces WrittenBy may name.
	WriteSurfaces []string

	// Conditions lists the pure conditions Conditions may name.
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
	c.validateAlternatives(t)
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
		if r.subjectScoped {
			c.problemf("%s: SubjectScoped is not supported by this foundation", where)
		}
		c.validateStoredAs(where, r)
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
		surface := cmp.Or(r.writtenBy, t.writtenBy)
		switch {
		case surface == "":
			c.problemf("%s: no WrittenBy surface on the relation or its type", where)
		case !slices.Contains(c.cfg.WriteSurfaces, surface):
			c.problemf("%s: write surface %q is not registered", where, surface)
		}
		for _, role := range r.roles {
			if !slices.Contains(c.cfg.Roles, role) {
				c.problemf("%s: role %q is not registered", where, role)
			}
		}
		if r.runtimeSince < 0 {
			c.problemf("%s: runtime safe-since version %d is negative", where, r.runtimeSince)
		}
		if r.kind == relationBlock {
			c.validateBlockOptions(where, r)
			continue
		}
		if !r.internal && r.description == "" {
			c.problemf("%s: user-visible grant needs a Describe", where)
		}
	}
}

func (c *compiler) validateBlockOptions(where string, r relationDecl) {
	if r.description != "" {
		c.problemf("%s: blocks are internal and take no Describe", where)
	}
	if r.sensitive {
		c.problemf("%s: blocks cannot be Sensitive", where)
	}
	if len(r.roles) > 0 {
		c.problemf("%s: blocks cannot be role defaults", where)
	}
}

func (c *compiler) validateStoredAs(where string, r relationDecl) {
	if r.storedAs == "" {
		c.problemf("%s: missing StoredAs", where)
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
		for _, name := range p.excludedBy {
			r, ok := m.relations[name]
			switch {
			case !ok:
				c.problemf("%s: ExcludedBy names unknown block %q", where, name)
			case r.kind != relationBlock:
				c.problemf("%s: ExcludedBy names grant %q; it needs a Block", where, name)
			}
		}
		if dup := firstDuplicate(p.excludedBy); dup != "" {
			c.problemf("%s: ExcludedBy lists block %q twice", where, dup)
		}
		if p.checks == "" {
			continue
		}
		if other, dup := checked[p.checks]; dup {
			c.problemf("%s: scope %q is already checked by permission %q", where, p.checks, other)
			continue
		}
		checked[p.checks] = p.name
		var relation string
		for _, r := range t.relations {
			if r.storedAs == p.checks && r.kind == relationGrant {
				relation = r.name
			}
		}
		if relation == "" {
			c.problemf("%s: Checks names %q, which is not a grant of this type", where, p.checks)
			continue
		}
		if !grantOnly(m, p.expr, nil) {
			c.problemf("%s: a permission that Checks a scope may use only Rel, Union, Minus and grant-only Perm", where)
			continue
		}
		if !slices.Contains(satisfyingRelations(m, p.expr, nil), relation) {
			c.problemf("%s: Checks %q but no Rel(%q) satisfies it", where, p.checks, relation)
		}
	}
	for _, r := range t.relations {
		if r.kind != relationGrant || r.storedAs == "" {
			continue
		}
		if _, ok := checked[r.storedAs]; !ok {
			c.problemf("type %q: grant %q: no permission Checks its scope %q", t.name, r.name, r.storedAs)
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
		return len(p.excludedBy) == 0 && grantOnly(m, p.expr, append(visiting, e.name))
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

func (c *compiler) validateAlternatives(t *TypeDecl) {
	for _, a := range t.alternatives {
		where := fmt.Sprintf("type %q: alternatives %q", t.name, a.name)
		if len(a.options) < 2 {
			c.problemf("%s: needs at least two alternatives", where)
		}
		anyBlock := false
		for _, ref := range a.options {
			target, ok := c.byName[ref.Type]
			if !ok {
				c.problemf("%s: alternative type %q is not declared", where, ref.Type)
				continue
			}
			i := slices.IndexFunc(target.permissions, func(p permissionDecl) bool { return p.name == ref.Permission })
			if i < 0 {
				c.problemf("%s: type %q declares no permission %q", where, ref.Type, ref.Permission)
				continue
			}
			if len(target.permissions[i].excludedBy) > 0 {
				anyBlock = true
			}
		}
		if dup := firstDuplicateRef(a.options); dup != nil {
			c.problemf("%s: alternative %s.%s listed twice", where, dup.Type, dup.Permission)
		}
		if a.veto && !anyBlock {
			c.problemf("%s: VetoBlocks needs an alternative with ExcludedBy", where)
		}
	}
}

func firstDuplicateRef(refs []PermissionRef) *PermissionRef {
	seen := make(map[PermissionRef]bool, len(refs))
	for _, ref := range refs {
		if seen[ref] {
			return &ref
		}
		seen[ref] = true
	}
	return nil
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

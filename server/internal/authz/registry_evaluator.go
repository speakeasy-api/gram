package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RegistryEvaluator answers compiled schema permissions. Evaluate decides one
// permission over one grant policy; Decide and DecideAlternatives (see
// permission_policies.go) compose several independent policies. Nothing here
// performs I/O, reads request context or writes logs: services load grants and
// facts and hand them over.
//
// Grant matching reuses the selector primitives Require uses today, so one
// stored grant means the same thing to both evaluators:
//
//   - A Rel matches a grant of its stored scope, or of root, whose selector
//     matches the request. Allows match permissively: a grant key the request
//     does not carry is skipped. Strict-matching scopes (auth clients),
//     StrictMatch requests and legacy API key grants match strictly.
//   - Root never satisfies an auth-client relation, a block or the subtracted
//     operand of a Minus.
//   - Blocks and subtracted operands match strictly, so a tool-scoped block
//     does not reach a dimensionless server-level request.
//
// Precedence follows the RFC rule for ExcludedBy:
//
//	allowed = ((matching_allows − inherited_blocks) ∪ direct_principal_exceptions)
//	          − own_principal_blocks
//
// An exception is a non-inferred grant made to one user by name that names a
// concrete resource (see IsDirectGrant). Wildcard, inferred, role, user:all,
// agent and API key grants never restore access, and neither do facts:
// blocking a union that includes authorship blocks authorship too. When the
// request names a tool, an exception must match it strictly. Raw Minus is
// plain set difference and restores nothing.
//
// Fact nodes compare the actor with loaded facts (Is), follow one verified
// relationship (Via) and skip paths whose guard is false (When). The actor is
// the authenticated principal in every policy, so a machine actor never
// matches a user fact: an owner's policy limits an agent without lending it
// the owner's authorship.
//
// Outcomes are three-valued: match, no match, or error when a needed fact is
// unresolved. A union matches if any branch matches, even when another
// branch could not be decided; otherwise an undecided branch makes it an
// error. Raw Minus needs both operands decided. Every combination is order
// independent, so reordering branches never turns an error into a decision
// or changes which instance a subtraction removes.
//
// A request evaluates exactly one operation instance: the resource and its
// dimensions. Set difference therefore removes access only when the
// subtracted operand proves that same instance.
type RegistryEvaluator struct {
	kinds       map[string]string
	facts       map[string]map[string]schema.Fact
	guards      map[string]map[string]schema.Cond
	scopes      map[string]evaluatorScope
	permissions map[schema.PermissionRef]evaluatorPermission
	alternates  map[schema.PermissionRef]schema.AlternativeSet
}

type evaluatorScope struct {
	strict       bool
	selectorKeys []string
}

type evaluatorPermission struct {
	def schema.PermissionDefinition
	// unsupported is the first node the evaluator cannot answer, found at
	// construction so the error does not depend on branch order.
	unsupported error
	// dimensions lists the request dimensions the permission's scopes accept.
	dimensions map[string]bool
	// readsFacts reports whether the permission, or one it reaches, reads
	// facts or traverses a relationship.
	readsFacts bool
}

// ErrUnsupportedExpression reports a permission node the evaluator does not
// know.
var ErrUnsupportedExpression = errors.New("permission expression is not supported by the evaluator")

// ErrUnknownPermission reports a request for a permission the registry does
// not declare.
var ErrUnknownPermission = errors.New("unknown permission")

// ErrMissingActor reports a fact-reading permission evaluated without an
// authenticated actor.
var ErrMissingActor = errors.New("authorization actor is missing")

// PermissionRequest is one operation instance to authorize.
type PermissionRequest struct {
	// Permission is the compiled permission to evaluate.
	Permission schema.PermissionRef

	// ResourceID is the concrete resource. Empty and "*" are invalid.
	ResourceID string

	// Dimensions narrow the instance, such as an MCP tool and disposition.
	// Every key must be accepted by a scope the permission reads.
	Dimensions map[string]string

	// StrictMatch makes allow grants match only when the request carries
	// every key of their selectors, as Check.WithStrictSelectorMatch does.
	StrictMatch bool

	// RepresentedUser binds $represented_user in auth-client selectors. Leave
	// it empty unless admission established the user the request acts for.
	// It never stands in for the actor.
	RepresentedUser string

	// Actor is the authenticated principal making the request. Permissions
	// that read facts require it.
	Actor urn.Principal

	// Facts are the resource's loaded Attr facts by name. Derived facts are
	// computed from them and must not be supplied.
	Facts map[string]Fact

	// RequiredFacts must resolve before anything is decided, even when a
	// grant branch would allow on its own. Use it for prerequisites such as a
	// tenant relationship, and to keep an existing handler's eager lookup
	// error.
	RequiredFacts []string
}

// PermissionReason says why a permission was or was not satisfied.
type PermissionReason string

const (
	// PermissionReasonMatched means an allow grant or fact path satisfied the
	// permission.
	PermissionReasonMatched PermissionReason = "matched"
	// PermissionReasonDirectException means a direct user grant restored
	// access that an inherited block removed.
	PermissionReasonDirectException PermissionReason = "direct_exception"
	// PermissionReasonNoGrant means nothing matched.
	PermissionReasonNoGrant PermissionReason = "no_grant"
	// PermissionReasonExcluded means an ExcludedBy block removed access.
	PermissionReasonExcluded PermissionReason = "excluded"
	// PermissionReasonSubtracted means the subtracted operand of a raw Minus
	// removed access.
	PermissionReasonSubtracted PermissionReason = "subtracted"
)

// PermissionDecision is the outcome of one evaluation. Grants are copies of
// the policy entries that decided it.
type PermissionDecision struct {
	// Allowed reports whether the permission is satisfied.
	Allowed bool

	// Reason explains the outcome.
	Reason PermissionReason

	// Grant is the allow that satisfied the permission, or that matched before
	// a block or subtraction removed it. It is nil when a fact path matched.
	Grant *Grant

	// Removed is the block or subtracted grant that removed access, if any.
	Removed *Grant
}

// NewRegistryEvaluator indexes a compiled registry for evaluation.
func NewRegistryEvaluator(registry *schema.Registry) (*RegistryEvaluator, error) {
	if registry == nil {
		return nil, errors.New("registry evaluator requires a compiled registry")
	}
	e := &RegistryEvaluator{
		kinds:       make(map[string]string),
		facts:       make(map[string]map[string]schema.Fact),
		guards:      make(map[string]map[string]schema.Cond),
		scopes:      make(map[string]evaluatorScope),
		permissions: make(map[schema.PermissionRef]evaluatorPermission),
		alternates:  make(map[schema.PermissionRef]schema.AlternativeSet),
	}
	for _, scope := range registry.Scopes() {
		e.scopes[scope.Name] = evaluatorScope{
			strict:       scope.Matching == schema.MatchingStrict,
			selectorKeys: scope.SelectorKeys,
		}
	}
	for _, t := range registry.Types() {
		e.kinds[t.Name] = t.ResourceKind
		e.facts[t.Name] = make(map[string]schema.Fact, len(t.Facts))
		for _, fact := range t.Facts {
			e.facts[t.Name][fact.Name] = fact
		}
		e.guards[t.Name] = make(map[string]schema.Cond, len(t.Guards))
		for _, guard := range t.Guards {
			cond, ok := registry.Guard(t.Name, guard)
			if !ok {
				return nil, fmt.Errorf("registry lists guard %s.%s without a condition", t.Name, guard)
			}
			e.guards[t.Name][guard] = cond
		}
		for _, name := range t.Permissions {
			ref := schema.PermissionRef{Type: t.Name, Permission: name}
			def, ok := registry.Permission(ref)
			if !ok {
				return nil, fmt.Errorf("registry lists permission %s.%s without a definition", ref.Type, ref.Permission)
			}
			e.permissions[ref] = evaluatorPermission{def: def, unsupported: nil, dimensions: nil, readsFacts: false}
		}
		for _, name := range t.Alternatives {
			ref := schema.PermissionRef{Type: t.Name, Permission: name}
			set, ok := registry.Alternatives(ref)
			if !ok {
				return nil, fmt.Errorf("registry lists alternatives %s.%s without a definition", ref.Type, ref.Permission)
			}
			e.alternates[ref] = set
		}
	}
	for ref, p := range e.permissions {
		p.unsupported = e.firstUnsupported(ref.Type, p.def.Expr, []string{ref.Permission})
		p.readsFacts = len(p.def.Facts) > 0 || len(p.def.Traverses) > 0
		p.dimensions = make(map[string]bool)
		for _, scope := range p.def.Scopes {
			for _, key := range e.scopes[scope].selectorKeys {
				p.dimensions[key] = true
			}
		}
		e.permissions[ref] = p
	}
	return e, nil
}

// firstUnsupported walks expr and the same-type permissions it reuses, and
// returns an error naming the first node the evaluator cannot answer.
func (e *RegistryEvaluator) firstUnsupported(typeName string, expr schema.Expr, visiting []string) error {
	switch expr.Op() {
	case schema.OpRel, schema.OpIs, schema.OpVia:
		return nil
	case schema.OpUnion, schema.OpMinus, schema.OpWhen:
		for _, operand := range expr.Operands() {
			if err := e.firstUnsupported(typeName, operand, visiting); err != nil {
				return err
			}
		}
		return nil
	case schema.OpPerm:
		if slices.Contains(visiting, expr.Name()) {
			return fmt.Errorf("permission cycle through %s.%s", typeName, expr.Name())
		}
		target, ok := e.permissions[schema.PermissionRef{Type: typeName, Permission: expr.Name()}]
		if !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownPermission, typeName, expr.Name())
		}
		return e.firstUnsupported(typeName, target.def.Expr, append(slices.Clone(visiting), expr.Name()))
	default:
		return fmt.Errorf("%w: unknown node %q", ErrUnsupportedExpression, expr.Op())
	}
}

// Evaluate answers req over one independent grant policy. It returns an
// error, never a decision, for an invalid request or when a needed fact is
// unresolved.
func (e *RegistryEvaluator) Evaluate(policy []Grant, req PermissionRequest) (PermissionDecision, error) {
	p, err := e.prepare(req)
	if err != nil {
		return PermissionDecision{}, err
	}
	out := e.run(policy, req, p)
	if out.err != nil {
		return PermissionDecision{}, fmt.Errorf("evaluate %s.%s: %w", req.Permission.Type, req.Permission.Permission, out.err)
	}
	return out.decision(), nil
}

// prepare validates req once, before any policy is evaluated.
func (e *RegistryEvaluator) prepare(req PermissionRequest) (evaluatorPermission, error) {
	p, ok := e.permissions[req.Permission]
	if !ok {
		return evaluatorPermission{}, fmt.Errorf("%w: %s.%s", ErrUnknownPermission, req.Permission.Type, req.Permission.Permission)
	}
	if p.unsupported != nil {
		return evaluatorPermission{}, fmt.Errorf("evaluate %s.%s: %w", req.Permission.Type, req.Permission.Permission, p.unsupported)
	}
	if err := e.validate(p, req); err != nil {
		return evaluatorPermission{}, err
	}
	return p, nil
}

func (e *RegistryEvaluator) run(policy []Grant, req PermissionRequest, p evaluatorPermission) outcome {
	run := e.evaluation(policy, req, req.Permission.Type, resourceInstance{id: req.ResourceID, dimensions: req.Dimensions, facts: req.Facts})
	return run.permission(p.def, run.allowMode())
}

func (e *RegistryEvaluator) evaluation(policy []Grant, req PermissionRequest, typeName string, res resourceInstance) evaluation {
	return evaluation{
		e:      e,
		typ:    typeName,
		kind:   e.kinds[typeName],
		policy: policy,
		req:    req,
		res:    res,
	}
}

func (e *RegistryEvaluator) validate(p evaluatorPermission, req PermissionRequest) error {
	scope := Scope("")
	if len(p.def.Scopes) > 0 {
		scope = Scope(p.def.Scopes[0])
	}
	if req.ResourceID == "" || req.ResourceID == WildcardResource {
		return InvalidCheck(scope, req.ResourceID)
	}
	for key := range req.Dimensions {
		if !p.dimensions[key] {
			return invalidCheckReason(scope, req.ResourceID, "dimension "+key+" is not accepted by "+req.Permission.Type+"."+req.Permission.Permission)
		}
	}
	if p.readsFacts && (req.Actor.IsZero() || req.Actor.ID == "") {
		return fmt.Errorf("evaluate %s.%s: %w", req.Permission.Type, req.Permission.Permission, ErrMissingActor)
	}
	if err := e.validateFacts(req.Permission.Type, req.Facts); err != nil {
		return err
	}
	for _, name := range req.RequiredFacts {
		if _, declared := e.facts[req.Permission.Type][name]; !declared {
			return fmt.Errorf("required fact %s is not declared on %s", name, req.Permission.Type)
		}
		fact := req.Facts[name]
		if !fact.resolved() {
			return fact.unresolved(name)
		}
	}
	return validateAuthClientCheck(Check{
		Scope:           scope,
		ResourceKind:    e.kinds[req.Permission.Type],
		ResourceID:      req.ResourceID,
		Dimensions:      req.Dimensions,
		selectorMatch:   selectorMatchStrict,
		representedUser: req.RepresentedUser,
	})
}

// validateFacts rejects supplied facts the type does not declare as Attrs,
// so a misspelt fact cannot read as unloaded.
func (e *RegistryEvaluator) validateFacts(typeName string, facts map[string]Fact) error {
	for name, fact := range facts {
		declared, ok := e.facts[typeName][name]
		switch {
		case !ok:
			return fmt.Errorf("fact %s is not declared on %s", name, typeName)
		case declared.DerivedFrom != "":
			return fmt.Errorf("fact %s on %s is derived; supply %s instead", name, typeName, declared.DerivedFrom)
		case fact.related != nil:
			if _, isType := e.kinds[declared.Type]; !isType {
				return fmt.Errorf("fact %s on %s is not a relationship", name, typeName)
			}
			if err := e.validateFacts(declared.Type, fact.related.Facts); err != nil {
				return fmt.Errorf("related %s: %w", name, err)
			}
		}
	}
	return nil
}

type resourceInstance struct {
	id         string
	dimensions map[string]string
	facts      map[string]Fact
}

// evaluation is one permission over one policy and resource. leafMode says
// which grants may prove a positive Rel and whether fact paths may match.
type evaluation struct {
	e      *RegistryEvaluator
	typ    string
	kind   string
	policy []Grant
	req    PermissionRequest
	res    resourceInstance
}

type leafMode struct {
	// evidence lists the grants that may prove a positive Rel. It narrows to
	// direct exceptions inside an ExcludedBy restoration.
	evidence []Grant
	// strict matches allows strictly.
	strict bool
	// root lets a root grant prove a Rel.
	root bool
	// grantsOnly makes Is and Via unable to match, for restorations, which
	// only grants may perform.
	grantsOnly bool
}

func (r evaluation) allowMode() leafMode {
	return leafMode{evidence: r.policy, strict: r.req.StrictMatch, root: true, grantsOnly: false}
}

// subtractMode is how the subtracted operand of a raw Minus matches: against
// the whole policy, strictly and without root, like a block.
func (r evaluation) subtractMode() leafMode {
	return leafMode{evidence: r.policy, strict: true, root: false, grantsOnly: false}
}

type outcome struct {
	allowed bool
	reason  PermissionReason
	grant   *Grant
	removed *Grant
	// err is set when the outcome could not be decided.
	err error
}

func noMatch() outcome {
	return outcome{allowed: false, reason: PermissionReasonNoGrant, grant: nil, removed: nil, err: nil}
}

func matched(grant *Grant) outcome {
	return outcome{allowed: true, reason: PermissionReasonMatched, grant: grant, removed: nil, err: nil}
}

func undecided(err error) outcome {
	return outcome{allowed: false, reason: PermissionReasonNoGrant, grant: nil, removed: nil, err: err}
}

func (o outcome) decision() PermissionDecision {
	return PermissionDecision{Allowed: o.allowed, Reason: o.reason, Grant: copyGrant(o.grant), Removed: copyGrant(o.removed)}
}

func copyGrant(g *Grant) *Grant {
	if g == nil {
		return nil
	}
	out := *g
	out.Selector = maps.Clone(g.Selector)
	return &out
}

// denialRank orders denial reasons so a union reports the most informative
// one: a removed allow says more than a missing one.
func denialRank(reason PermissionReason) int {
	switch reason {
	case PermissionReasonExcluded:
		return 2
	case PermissionReasonSubtracted:
		return 1
	default:
		return 0
	}
}

func (r evaluation) permission(def schema.PermissionDefinition, mode leafMode) outcome {
	base := r.expr(def.Expr, mode)
	if !base.allowed || len(def.ExcludedBy) == 0 {
		return base
	}
	stands, block, restored := r.blockStands(def, mode)
	switch {
	case block == nil:
		return base
	case restored.err != nil:
		return restored
	case stands:
		return outcome{allowed: false, reason: PermissionReasonExcluded, grant: base.grant, removed: block, err: nil}
	default:
		restored.reason = PermissionReasonDirectException
		return restored
	}
}

// blockStands reports whether a block of def excludes this instance. It
// returns the matching block (nil when none matches) and, when an inherited
// block met direct exceptions, their outcome.
func (r evaluation) blockStands(def schema.PermissionDefinition, mode leafMode) (bool, *Grant, outcome) {
	block, own := r.matchBlocks(def.ExcludedBy)
	switch {
	case block == nil:
		return false, nil, noMatch()
	case own != nil:
		// The principal's own block beats every grant, including its own
		// exceptions.
		return true, own, noMatch()
	}

	// Restore only what direct exceptions prove on their own. Subtractions
	// and nested blocks inside the expression still read the whole policy,
	// so an exception cannot undo another difference.
	exceptions := leafMode{
		evidence:   directExceptions(mode.evidence),
		strict:     mode.strict || r.namesTool(),
		root:       mode.root,
		grantsOnly: true,
	}
	if len(exceptions.evidence) == 0 {
		return true, block, noMatch()
	}
	restored := r.expr(def.Expr, exceptions)
	if restored.err != nil {
		return true, block, restored
	}
	return !restored.allowed, block, restored
}

func (r evaluation) expr(expr schema.Expr, mode leafMode) outcome {
	switch expr.Op() {
	case schema.OpRel:
		if grant := r.matchAllow(expr.Scope(), mode); grant != nil {
			return matched(grant)
		}
		return noMatch()
	case schema.OpUnion:
		denied := noMatch()
		var undecidedBranch *outcome
		for _, branch := range expr.Operands() {
			out := r.expr(branch, mode)
			switch {
			case out.allowed:
				return out
			case out.err != nil:
				if undecidedBranch == nil {
					undecidedBranch = &out
				}
			case denialRank(out.reason) > denialRank(denied.reason):
				denied = out
			}
		}
		if undecidedBranch != nil {
			return *undecidedBranch
		}
		return denied
	case schema.OpMinus:
		operands := expr.Operands()
		base := r.expr(operands[0], mode)
		subtracted := r.expr(operands[1], r.subtractMode())
		switch {
		case base.err != nil:
			return base
		case subtracted.err != nil:
			return subtracted
		case base.allowed && subtracted.allowed:
			return outcome{allowed: false, reason: PermissionReasonSubtracted, grant: base.grant, removed: subtracted.grant, err: nil}
		default:
			return base
		}
	case schema.OpPerm:
		// NewRegistryEvaluator resolved every reachable Perm, so the lookup
		// cannot miss for a supported permission.
		p := r.e.permissions[schema.PermissionRef{Type: r.typ, Permission: expr.Name()}]
		return r.permission(p.def, mode)
	case schema.OpIs:
		if mode.grantsOnly {
			return noMatch()
		}
		user, err := r.userFact(expr.Name())
		if err != nil {
			return undecided(err)
		}
		if actorIs(r.req.Actor, user) {
			return matched(nil)
		}
		return noMatch()
	case schema.OpVia:
		if mode.grantsOnly {
			return noMatch()
		}
		return r.via(expr, mode)
	case schema.OpWhen:
		cond := r.e.guards[r.typ][expr.Guard()]
		holds, err := guardResult(cond, r.res.facts)
		switch holds {
		case truthFalse:
			return noMatch()
		case truthUnknown:
			return undecided(fmt.Errorf("guard %s: %w", expr.Guard(), err))
		case truthTrue:
		}
		return r.expr(expr.Operands()[0], mode)
	default:
		return undecided(fmt.Errorf("%w: unknown node %q", ErrUnsupportedExpression, expr.Op()))
	}
}

// userFact returns a user fact, computing a Derived projection from its
// source: a non-empty string names a user, while NULL and "" name nobody.
func (r evaluation) userFact(name string) (Fact, error) {
	declared := r.e.facts[r.typ][name]
	if declared.DerivedFrom == "" {
		fact := r.res.facts[name]
		if !fact.resolved() {
			return Fact{}, fact.unresolved(name)
		}
		return fact, nil
	}
	source := r.res.facts[declared.DerivedFrom]
	if !source.resolved() {
		return Fact{}, source.unresolved(declared.DerivedFrom)
	}
	if source.state == FactAbsent || source.text == "" {
		return AbsentFact(), nil
	}
	return StringFact(source.text), nil
}

// via evaluates a permission on the related resource a fact names, in the
// same policy and for the same actor. Known absence matches nothing.
func (r evaluation) via(expr schema.Expr, mode leafMode) outcome {
	fact := r.res.facts[expr.Name()]
	switch {
	case !fact.resolved():
		return undecided(fact.unresolved(expr.Name()))
	case fact.state == FactAbsent:
		return noMatch()
	case fact.related == nil:
		return undecided(fmt.Errorf("%w: relationship %s has no loaded target", ErrFactUnresolved, expr.Name()))
	}
	target := schema.PermissionRef{Type: expr.TargetType(), Permission: expr.Permission()}
	p, ok := r.e.permissions[target]
	if !ok {
		return undecided(fmt.Errorf("%w: %s.%s", ErrUnknownPermission, target.Type, target.Permission))
	}
	related := r.e.evaluation(r.policy, r.req, target.Type, resourceInstance{id: fact.related.ID, dimensions: fact.related.Dimensions, facts: fact.related.Facts})
	if related.res.id == "" || related.res.id == WildcardResource {
		return undecided(fmt.Errorf("%w: relationship %s has no concrete target", ErrFactUnresolved, expr.Name()))
	}
	return related.permission(p.def, leafMode{evidence: mode.evidence, strict: mode.strict, root: mode.root, grantsOnly: mode.grantsOnly})
}

// check builds the legacy Check the selector primitives match grants with.
func (r evaluation) check(scope string, strict bool) Check {
	match := selectorMatchNormal
	if strict || r.e.scopes[scope].strict {
		match = selectorMatchStrict
	}
	return Check{
		Scope:           Scope(scope),
		ResourceKind:    r.kind,
		ResourceID:      r.res.id,
		Dimensions:      r.res.dimensions,
		selectorMatch:   match,
		representedUser: r.req.RepresentedUser,
	}
}

func (r evaluation) matchAllow(scope string, mode leafMode) *Grant {
	check := r.check(scope, mode.strict)
	// Ownership of a personal connection filters authority and never grants
	// it.
	if personalCheckUnbound(check) {
		return nil
	}
	checks := []Check{check}
	if mode.root && !check.isAuthClientCheck() {
		root := check
		root.Scope = ScopeRoot
		checks = append(checks, root)
	}
	grant, _ := matchingGrant(mode.evidence, checks)
	return grant
}

// matchBlocks returns a matching block from the whole policy, and the first
// matching block held by the user principal itself.
func (r evaluation) matchBlocks(blocks []string) (*Grant, *Grant) {
	checks := make([]Check, 0, len(blocks))
	for _, scope := range blocks {
		checks = append(checks, r.check(scope, true))
	}
	var first, own *Grant
	for i := range r.policy {
		grant := &r.policy[i]
		if grantMatchingCheck(grant, checks) == nil {
			continue
		}
		if first == nil {
			first = grant
		}
		if IsDirectGrant(*grant) {
			own = grant
			break
		}
	}
	return first, own
}

func (r evaluation) namesTool() bool {
	_, ok := r.res.dimensions[SelectorKeyTool]
	return ok
}

// directExceptions returns the allows in grants that may restore access an
// inherited block removed.
func directExceptions(grants []Grant) []Grant {
	var out []Grant
	for _, grant := range grants {
		if isDirectOverrideGrant(grant) && !IsBlocklistScope(grant.Scope) {
			out = append(out, grant)
		}
	}
	return out
}

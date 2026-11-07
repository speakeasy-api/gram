package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// RegistryEvaluator answers compiled schema permissions over one loaded grant
// policy. It is the pure grant-policy layer of the registry-backed client: it
// performs no I/O, reads no request context and writes no logs. Combining
// several independent policies, facts, ownership and named alternatives
// belongs to the layers above it.
//
// The evaluator consumes the compiled expression of a permission and supports
// the grant-only nodes: Rel, Union, Minus and Perm, plus ExcludedBy block
// precedence. Is, Via and When read resource facts and are rejected with
// ErrUnsupportedExpression until fact evaluation exists; a permission that
// reaches one of them errors whatever the order of its branches.
//
// Matching reuses the selector primitives Require uses today, so one stored
// grant means the same thing to both evaluators:
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
// agent and API key grants never restore access. When the request names a
// tool, an exception must match it strictly, so a disposition-scoped grant
// cannot reach an unannotated tool past a block. Raw Minus is plain set
// difference and restores nothing.
//
// A request evaluates exactly one operation instance: the resource and its
// dimensions. Set difference therefore removes access only when the
// subtracted operand proves that same instance; a bypass for another server
// subtracts nothing.
type RegistryEvaluator struct {
	kinds       map[string]string
	scopes      map[string]evaluatorScope
	permissions map[schema.PermissionRef]evaluatorPermission
}

type evaluatorScope struct {
	strict       bool
	selectorKeys []string
}

type evaluatorPermission struct {
	def schema.PermissionDefinition
	// unsupported is the first node the grant evaluator cannot answer, found
	// at construction so the error does not depend on branch order.
	unsupported error
	// dimensions lists the request dimensions the permission's scopes accept.
	dimensions map[string]bool
}

// ErrUnsupportedExpression reports a permission that reads facts the grant
// evaluator does not evaluate.
var ErrUnsupportedExpression = errors.New("permission expression is not supported by the grant evaluator")

// ErrUnknownPermission reports a request for a permission the registry does
// not declare.
var ErrUnknownPermission = errors.New("unknown permission")

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
	RepresentedUser string
}

// PermissionReason says why a permission was or was not satisfied.
type PermissionReason string

const (
	// PermissionReasonMatched means an allow grant satisfied the permission.
	PermissionReasonMatched PermissionReason = "matched"
	// PermissionReasonDirectException means a direct user grant restored
	// access that an inherited block removed.
	PermissionReasonDirectException PermissionReason = "direct_exception"
	// PermissionReasonNoGrant means no allow grant matched.
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
	// a block or subtraction removed it.
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
		scopes:      make(map[string]evaluatorScope),
		permissions: make(map[schema.PermissionRef]evaluatorPermission),
	}
	for _, scope := range registry.Scopes() {
		e.scopes[scope.Name] = evaluatorScope{
			strict:       scope.Matching == schema.MatchingStrict,
			selectorKeys: scope.SelectorKeys,
		}
	}
	for _, t := range registry.Types() {
		e.kinds[t.Name] = t.ResourceKind
		for _, name := range t.Permissions {
			ref := schema.PermissionRef{Type: t.Name, Permission: name}
			def, ok := registry.Permission(ref)
			if !ok {
				return nil, fmt.Errorf("registry lists permission %s.%s without a definition", ref.Type, ref.Permission)
			}
			e.permissions[ref] = evaluatorPermission{def: def, unsupported: nil, dimensions: nil}
		}
	}
	for ref, p := range e.permissions {
		p.unsupported = e.firstUnsupported(ref.Type, p.def.Expr, []string{ref.Permission})
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
// returns an error naming the first fact-reading node.
func (e *RegistryEvaluator) firstUnsupported(typeName string, expr schema.Expr, visiting []string) error {
	switch expr.Op() {
	case schema.OpRel:
		return nil
	case schema.OpUnion, schema.OpMinus:
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
	case schema.OpIs, schema.OpVia, schema.OpWhen:
		return fmt.Errorf("%w: %s reads resource facts", ErrUnsupportedExpression, expr.Op())
	default:
		return fmt.Errorf("%w: unknown node %q", ErrUnsupportedExpression, expr.Op())
	}
}

// Evaluate answers req over policy, one independent grant policy. It returns
// an error, never a decision, for an invalid request or a permission the grant
// evaluator cannot answer.
func (e *RegistryEvaluator) Evaluate(policy []Grant, req PermissionRequest) (PermissionDecision, error) {
	p, ok := e.permissions[req.Permission]
	if !ok {
		return PermissionDecision{}, fmt.Errorf("%w: %s.%s", ErrUnknownPermission, req.Permission.Type, req.Permission.Permission)
	}
	if p.unsupported != nil {
		return PermissionDecision{}, fmt.Errorf("evaluate %s.%s: %w", req.Permission.Type, req.Permission.Permission, p.unsupported)
	}
	if err := e.validate(p, req); err != nil {
		return PermissionDecision{}, err
	}

	run := evaluation{
		e:      e,
		kind:   e.kinds[req.Permission.Type],
		typ:    req.Permission.Type,
		policy: policy,
		req:    req,
	}
	out := run.permission(p.def, run.allowMode())
	return out.decision(), nil
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
	return validateAuthClientCheck(Check{
		Scope:           scope,
		ResourceKind:    e.kinds[req.Permission.Type],
		ResourceID:      req.ResourceID,
		Dimensions:      req.Dimensions,
		selectorMatch:   selectorMatchStrict,
		representedUser: req.RepresentedUser,
	})
}

// evaluation is one Evaluate call. Every operand reads the same request and
// policy; leafMode says which grants may prove a positive Rel and how.
type evaluation struct {
	e      *RegistryEvaluator
	kind   string
	typ    string
	policy []Grant
	req    PermissionRequest
}

type leafMode struct {
	// evidence lists the grants that may prove a positive Rel. It narrows to
	// direct exceptions inside an ExcludedBy restoration.
	evidence []Grant
	// strict matches allows strictly.
	strict bool
	// root lets a root grant prove a Rel.
	root bool
}

func (r evaluation) allowMode() leafMode {
	return leafMode{evidence: r.policy, strict: r.req.StrictMatch, root: true}
}

// subtractMode is how the subtracted operand of a raw Minus matches: against
// the whole policy, strictly and without root, like a block.
func (r evaluation) subtractMode() leafMode {
	return leafMode{evidence: r.policy, strict: true, root: false}
}

type outcome struct {
	allowed bool
	reason  PermissionReason
	grant   *Grant
	removed *Grant
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

	block, own := r.matchBlocks(def.ExcludedBy)
	if block == nil {
		return base
	}
	excluded := outcome{allowed: false, reason: PermissionReasonExcluded, grant: base.grant, removed: block}
	if own != nil {
		// The principal's own block beats every grant, including its own
		// exceptions.
		excluded.removed = own
		return excluded
	}

	// Restore only what direct exceptions prove on their own. Subtractions
	// and nested blocks inside the expression still read the whole policy,
	// so an exception cannot undo another difference.
	exceptions := leafMode{
		evidence: directExceptions(mode.evidence),
		strict:   mode.strict || r.namesTool(),
		root:     mode.root,
	}
	if len(exceptions.evidence) == 0 {
		return excluded
	}
	restored := r.expr(def.Expr, exceptions)
	if !restored.allowed {
		return excluded
	}
	restored.reason = PermissionReasonDirectException
	return restored
}

func (r evaluation) expr(expr schema.Expr, mode leafMode) outcome {
	switch expr.Op() {
	case schema.OpRel:
		if grant := r.matchAllow(expr.Scope(), mode); grant != nil {
			return outcome{allowed: true, reason: PermissionReasonMatched, grant: grant, removed: nil}
		}
		return outcome{allowed: false, reason: PermissionReasonNoGrant, grant: nil, removed: nil}
	case schema.OpUnion:
		denied := outcome{allowed: false, reason: PermissionReasonNoGrant, grant: nil, removed: nil}
		for _, branch := range expr.Operands() {
			out := r.expr(branch, mode)
			if out.allowed {
				return out
			}
			if denialRank(out.reason) > denialRank(denied.reason) {
				denied = out
			}
		}
		return denied
	case schema.OpMinus:
		operands := expr.Operands()
		base := r.expr(operands[0], mode)
		if !base.allowed {
			return base
		}
		subtracted := r.expr(operands[1], r.subtractMode())
		if subtracted.allowed {
			return outcome{allowed: false, reason: PermissionReasonSubtracted, grant: base.grant, removed: subtracted.grant}
		}
		return base
	case schema.OpPerm:
		// NewRegistryEvaluator resolved every reachable Perm, so the lookup
		// cannot miss for a supported permission.
		p := r.e.permissions[schema.PermissionRef{Type: r.typ, Permission: expr.Name()}]
		return r.permission(p.def, mode)
	case schema.OpIs, schema.OpVia, schema.OpWhen:
		// Rejected before evaluation starts; deny if reached regardless.
		return outcome{allowed: false, reason: PermissionReasonNoGrant, grant: nil, removed: nil}
	default:
		return outcome{allowed: false, reason: PermissionReasonNoGrant, grant: nil, removed: nil}
	}
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
		ResourceID:      r.req.ResourceID,
		Dimensions:      r.req.Dimensions,
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
	_, ok := r.req.Dimensions[SelectorKeyTool]
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

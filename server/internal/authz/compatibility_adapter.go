package authz

import (
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// The compatibility adapter translates between typed operations and the
// engine's Check form. It lets the client record challenges in the shape
// existing diagnostics read, and lets the engine and the client answer the
// same request side by side while services migrate. It never decides
// anything itself.

// legacyForm is how the engine answers the same request as an operation.
type legacyForm struct {
	// checks are the engine checks, one per permission or option.
	checks []Check
	// anyUnblocked marks alternatives, which the engine answers with
	// RequireAnyUnblocked when they veto blocks and RequireAny otherwise.
	anyUnblocked bool
	alternatives bool
}

// legacyCheck is the engine check equivalent to a single-permission
// operation. A permission that answers no stored scope, such as one built
// only from ownership facts, has no equivalent and returns false.
func (c *Client) legacyCheck(op Operation) (Check, bool) {
	def, ok := c.grantPart(op.permission)
	if !ok || def.AnswersScope == "" {
		return Check{Scope: "", ResourceKind: c.registryKind(op.permission.Type), ResourceID: op.instance.resourceID, Dimensions: maps.Clone(op.instance.dimensions), selectorMatch: selectorMatchNormal, representedUser: ""}, false
	}
	check := Check{
		Scope:        Scope(def.AnswersScope),
		ResourceKind: c.registryKind(op.permission.Type),
		ResourceID:   op.instance.resourceID,
		Dimensions:   maps.Clone(op.instance.dimensions),

		selectorMatch:   selectorMatchNormal,
		representedUser: "",
	}
	if op.instance.strict {
		check = check.WithStrictSelectorMatch()
	}
	return check, true
}

// legacyFormFor returns the engine form of op, or false when some permission
// it names has no engine equivalent.
func (c *Client) legacyFormFor(op Operation) (legacyForm, bool) {
	if !op.alternatives {
		check, ok := c.legacyCheck(op)
		return legacyForm{checks: []Check{check}, anyUnblocked: false, alternatives: false}, ok
	}
	set, ok := c.registry.Alternatives(op.permission)
	if !ok {
		return legacyForm{checks: nil, anyUnblocked: false, alternatives: false}, false
	}
	form := legacyForm{checks: make([]Check, 0, len(op.options)), anyUnblocked: set.AnyBlockVetoes, alternatives: true}
	for _, option := range op.options {
		check, ok := c.legacyCheck(option)
		if !ok {
			return legacyForm{checks: nil, anyUnblocked: false, alternatives: false}, false
		}
		form.checks = append(form.checks, check)
	}
	return form, true
}

// challengeChecks are the engine checks a call records: every requested
// check, alternatives' options included, and each operation's own checks for
// choosing the focus.
type challengeChecks struct {
	requested []Check
	perOp     [][]Check
}

// legacyChecksFor returns the checks a challenge records for ops.
func (c *Client) legacyChecksFor(ops []Operation) challengeChecks {
	out := challengeChecks{requested: make([]Check, 0, len(ops)), perOp: make([][]Check, len(ops))}
	for i, op := range ops {
		parts := []Operation{op}
		if op.alternatives && len(op.options) > 0 {
			parts = op.options
		}
		for _, part := range parts {
			check, _ := c.legacyCheck(part)
			out.perOp[i] = append(out.perOp[i], check)
			out.requested = append(out.requested, check)
		}
	}
	return out
}

// focus is the check a decision on operation i is attributed to: the
// alternative that decided it when the decision names one, otherwise the
// operation's first check.
func (cc challengeChecks) focus(i, option int) *Check {
	checks := cc.perOp[i]
	if option < 0 || option >= len(checks) {
		option = 0
	}
	return &checks[option]
}

// operationForCheck is the typed operation that answers an engine check, for
// comparing the two on the same request. It fails when no registered
// permission answers the check's scope.
func operationForCheck(registry *schema.Registry, check Check) (Operation, error) {
	ref, ok := registry.PermissionForCheck(string(check.Scope))
	if !ok {
		return Operation{}, fmt.Errorf("%w: no registered permission answers %s", ErrUnknownPermission, check.Scope)
	}
	t, ok := registry.Type(ref.Type)
	if !ok {
		return Operation{}, fmt.Errorf("%w: type %s", ErrUnknownPermission, ref.Type)
	}
	if check.ResourceKind != "" && check.ResourceKind != t.ResourceKind {
		return Operation{}, fmt.Errorf("check for %s names resource kind %s, but %s.%s is on %s", check.Scope, check.ResourceKind, ref.Type, ref.Permission, t.ResourceKind)
	}
	op := newOperation(ref.Type, ref.Permission, check.ResourceID, check.Dimensions)
	if check.selectorMatch == selectorMatchStrict {
		op = op.WithStrictMatch()
	}
	return op, nil
}

// grantPart returns the permission whose stored scope the engine checks for
// ref. That is ref itself when it answers a scope. A permission that answers
// none but is a union of exactly one scope-answering permission and attribute
// branches, such as chat.read = read_grant | is(author) | ..., is checked as
// that permission: the attribute branches are answered before the engine is
// asked (see allowedByAttributesOld).
func (c *Client) grantPart(ref schema.PermissionRef) (schema.PermissionDefinition, bool) {
	def, ok := c.registry.Permission(ref)
	if !ok || def.AnswersScope != "" || def.Expr.Op() != schema.OpUnion {
		return def, ok
	}
	var grant schema.PermissionDefinition
	found := false
	for _, branch := range def.Expr.Operands() {
		if branch.Op() == schema.OpPerm {
			target, ok := c.registry.Permission(schema.PermissionRef{Type: ref.Type, Permission: branch.Name()})
			if !ok || target.AnswersScope == "" || found {
				return def, true
			}
			grant, found = target, true
			continue
		}
		if c.readsGrants(ref.Type, branch) {
			return def, true
		}
	}
	if !found {
		return def, true
	}
	return grant, true
}

// readsGrants reports whether expr can be satisfied by a stored grant: a
// relation, a same-type permission, or a related resource's permission that
// reads any stored scope.
func (c *Client) readsGrants(typeName string, expr schema.Expr) bool {
	switch expr.Op() {
	case schema.OpRel, schema.OpPerm:
		return true
	case schema.OpIs:
		return false
	case schema.OpVia:
		target, ok := c.registry.Permission(schema.PermissionRef{Type: expr.TargetType(), Permission: expr.Permission()})
		return !ok || len(target.Scopes) > 0
	default:
		return slices.ContainsFunc(expr.Operands(), func(operand schema.Expr) bool { return c.readsGrants(typeName, operand) })
	}
}

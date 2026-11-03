package authz

import (
	"fmt"
	"maps"

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
	def, ok := c.registry.Permission(op.permission)
	if !ok || def.Checks == "" {
		return Check{Scope: "", ResourceKind: c.registryKind(op.permission.Type), ResourceID: op.instance.resourceID, Dimensions: maps.Clone(op.instance.dimensions), selectorMatch: selectorMatchNormal, representedUser: ""}, false
	}
	check := Check{
		Scope:        Scope(def.Checks),
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
	form := legacyForm{checks: make([]Check, 0, len(op.options)), anyUnblocked: set.VetoBlocks, alternatives: true}
	for _, option := range op.options {
		check, ok := c.legacyCheck(option)
		if !ok {
			return legacyForm{checks: nil, anyUnblocked: false, alternatives: false}, false
		}
		form.checks = append(form.checks, check)
	}
	return form, true
}

// legacyChecksFor returns one check per operation for challenge records: the
// operation's own check, or its first option's for alternatives.
func (c *Client) legacyChecksFor(ops []Operation) []Check {
	checks := make([]Check, len(ops))
	for i, op := range ops {
		if op.alternatives && len(op.options) > 0 {
			op = op.options[0]
		}
		checks[i], _ = c.legacyCheck(op)
	}
	return checks
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

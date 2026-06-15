package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Grant expressions model derived permissions over the grants already loaded
// for a request.
//
// A plain Check answers "does any grant match this scope/resource/dimensions?".
// A GrantExpression answers the richer question "which permission instances are
// proven by those matching grants, and which of those instances are removed by
// exceptions?". This is the piece we need for rules such as:
//
//	risk_policy applies = risk_policy:evaluate - risk_policy:bypass
//
// The expression layer deliberately reuses Check for all grant matching. That
// keeps selector semantics in one place: resource_kind/resource_id, wildcards,
// strict versus normal selector matching, scope expansion, and runtime
// dimensions such as server_url, server_identity, tool, disposition, and
// project_id.
//
// The only new concept here is the expression "instance": the set member a
// matched check proves. Base and exception expressions subtract only when they
// prove the same instance key.

// GrantExpression evaluates whether a loaded grant set satisfies a domain
// permission. Operands are grounded in Check values so selector matching,
// dimensions, wildcards, strict matching, and scope expansion remain centralized
// in the existing authz primitives.
type GrantExpression interface {
	// Evaluate is the public API for callers. It hides set details and reports
	// whether the final expression result is non-empty.
	Evaluate(grants []Grant) (GrantExpressionResult, error)

	// grantSet is intentionally unexported. It lets authz expression types
	// compose internally without exposing set-key mechanics as public API.
	grantSet(grants []Grant) (grantSet, error)
}

type GrantExpressionReason string

const (
	GrantExpressionReasonMatched          GrantExpressionReason = "matched"
	GrantExpressionReasonMissingBase      GrantExpressionReason = "missing_base"
	GrantExpressionReasonExceptionMatched GrantExpressionReason = "exception_matched"
	GrantExpressionReasonError            GrantExpressionReason = "error"
)

// GrantExpressionResult reports whether an expression was satisfied and why.
type GrantExpressionResult struct {
	Satisfied bool
	Reason    GrantExpressionReason
}

// grantSet is the set of permission instances proven by a grant expression.
// Keys are canonical selector encodings; values are kept for debugging or
// future proof details.
type grantSet map[string]grantSetMember

type grantSetMember struct {
	Selector Selector
}

// GrantCheck is the primitive grant expression.
//
// Check defines what grant must match. Instance identifies the domain
// permission instance that matching grant proves. If Instance is empty, the
// check selector is used as the set member key.
//
// This distinction matters when the matching grant is broader than the runtime
// permission instance. For risk policy application, a generic
// risk_policy:evaluate grant can prove "policy_123 applies to server_url=abc".
// A bypass grant subtracts only if it proves that same instance.
type GrantCheck struct {
	Check    Check
	Instance Selector
}

// GrantDifference evaluates both operands as sets and returns Base - Exception.
// It is satisfied when at least one base instance remains after removing all
// matching exception instances.
type GrantDifference struct {
	Base      GrantExpression
	Exception GrantExpression
}

func (g GrantCheck) Evaluate(grants []Grant) (GrantExpressionResult, error) {
	set, err := g.grantSet(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, err
	}
	if len(set) == 0 {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonMissingBase}, nil
	}
	return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
}

func (g GrantCheck) grantSet(grants []Grant) (grantSet, error) {
	if err := validateInput(g.Check); err != nil {
		return nil, err
	}

	// matchingAllowGrant already applies Check expansion and the Check's
	// selector match mode. Grant expressions do not reimplement selector logic.
	grant, _ := matchingAllowGrant(grants, g.Check.expand())
	if grant == nil {
		return grantSet{}, nil
	}

	set := grantSet{}
	set.add(grantSetMember{Selector: g.instanceSelector()})
	return set, nil
}

func (g GrantCheck) instanceSelector() Selector {
	if len(g.Instance) > 0 {
		return maps.Clone(g.Instance)
	}
	// Defaulting to the check selector is correct when "what matched" and "what
	// was proven" are the same permission instance.
	return g.Check.selector()
}

func (g GrantDifference) Evaluate(grants []Grant) (GrantExpressionResult, error) {
	if g.Base == nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, errors.New("grant difference requires a base expression")
	}
	if g.Exception == nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, errors.New("grant difference requires an exception expression")
	}

	baseResult, err := g.Base.Evaluate(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate base expression: %w", err)
	}
	if !baseResult.Satisfied {
		return baseResult, nil
	}

	base, err := g.Base.grantSet(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate base expression set: %w", err)
	}
	exception, err := g.Exception.grantSet(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate exception expression set: %w", err)
	}

	base.subtract(exception)
	if len(base) == 0 {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonExceptionMatched}, nil
	}

	return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
}

func (g GrantDifference) grantSet(grants []Grant) (grantSet, error) {
	if g.Base == nil {
		return nil, errors.New("grant difference requires a base expression")
	}
	if g.Exception == nil {
		return nil, errors.New("grant difference requires an exception expression")
	}

	base, err := g.Base.grantSet(grants)
	if err != nil {
		return nil, fmt.Errorf("evaluate base expression: %w", err)
	}
	exception, err := g.Exception.grantSet(grants)
	if err != nil {
		return nil, fmt.Errorf("evaluate exception expression: %w", err)
	}
	base.subtract(exception)
	return base, nil
}

func (s grantSet) add(member grantSetMember) {
	s[grantSetKey(member.Selector)] = member
}

// subtract removes every instance from s that is also present in other.
// This is the actual set-difference operation: exception instances remove only
// base instances with the same canonical selector key. Other base instances
// stay in the set, so an exception does not automatically make the whole
// expression false.
func (s grantSet) subtract(other grantSet) {
	for key := range other {
		delete(s, key)
	}
}

func grantSetKey(selector Selector) string {
	keys := make([]string, 0, len(selector))
	for key := range selector {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var b strings.Builder
	for _, key := range keys {
		writeGrantSetKeyPart(&b, key)
		writeGrantSetKeyPart(&b, selector[key])
	}
	return b.String()
}

func writeGrantSetKeyPart(b *strings.Builder, value string) {
	// Length-prefixing prevents ambiguous concatenation between adjacent parts.
	// For example ["ab", "c"] and ["a", "bc"] encode differently.
	b.WriteString(strconv.Itoa(len(value)))
	b.WriteByte(':')
	b.WriteString(value)
	b.WriteByte('|')
}

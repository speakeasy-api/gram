package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Grant expressions are reusable authorization rules built from normal Checks.
//
// Use a Check when one grant is enough to answer the question:
//
//	"does this user have mcp:connect for this server?"
//
// Use a GrantExpression when the rule also has exclusions or other grant-based
// conditions:
//
//	"does this user have mcp:connect for this server, unless they also have
//	 a matching mcp:block grant?"
//
//	"does this risk policy apply to this request, unless the user also has
//	 a matching risk_policy:bypass grant?"
//
// The expression still uses Check for matching grants. That keeps scope
// expansion, selector matching, wildcards, strict matching, and dimensions in
// the same code path as Require/RequireAny. The expression layer only decides
// how matching grants combine.
//
// The main operation today is difference:
//
//	base permission - exclusion permission
//
// For risk policy evaluation this means:
//
//	risk_policy:evaluate(policy_123, server_url=abc)
//	  - risk_policy:bypass(policy_123, server_url=abc)
//
// A broad evaluate grant can prove that policy_123 applies to server_url=abc.
// A bypass grant removes that decision only when it proves the same policy and
// runtime dimensions. A bypass for server_url=abc does not remove policy
// evaluation for server_url=cde.

// GrantExpression evaluates whether a loaded grant set satisfies a domain
// permission.
//
// Callers should normally use Evaluate. The unexported grantSet method exists
// so expressions can compose with each other without exposing the internal set
// representation outside this package.
type GrantExpression interface {
	// Evaluate answers the user-facing authorization question: did the loaded
	// grants satisfy this expression?
	//
	// Implementations may build intermediate sets, but callers only need the
	// boolean result and the high-level reason.
	Evaluate(grants []Grant) (GrantExpressionResult, error)

	// grantSet returns the concrete permission instances proven by this
	// expression. It is deliberately private because the set shape is an
	// implementation detail of expression composition, not public API.
	grantSet(grants []Grant) (grantSet, error)
}

type GrantExpressionReason string

const (
	GrantExpressionReasonMatched          GrantExpressionReason = "matched"
	GrantExpressionReasonMissingBase      GrantExpressionReason = "missing_base"
	GrantExpressionReasonExclusionMatched GrantExpressionReason = "exclusion_matched"
	GrantExpressionReasonError            GrantExpressionReason = "error"
)

// GrantExpressionResult reports whether an expression was satisfied and why.
type GrantExpressionResult struct {
	Satisfied bool
	Reason    GrantExpressionReason
}

// grantSet is the internal result of evaluating an expression as "which concrete
// permission instances did these grants prove?".
//
// A permission instance is represented by a Selector, for example:
//
//	{resource_kind:"risk_policy", resource_id:"policy_123", server_url:"abc"}
//
// The map key is a stable encoding of that selector so difference can delete
// matching instances quickly and exactly. The selector is kept as the value so
// future diagnostics can explain which instance survived or was removed.
type grantSet map[string]grantSetMember

type grantSetMember struct {
	Selector Selector
}

// GrantCheck turns one Check into a GrantExpression.
//
// Check is the grant-matching rule. It answers "does any loaded grant satisfy
// this scope/resource/dimensions?" using the same matching behavior as normal
// RBAC checks.
//
// Instance is the concrete permission instance that a matching grant proves. If
// Instance is empty, the check selector is used.
//
// The two are separate because the grant that matches can be broader than the
// runtime decision being made. Example: a user may have a generic
// risk_policy:evaluate grant for policy_123, but the runtime decision is
// "policy_123 applies to server_url=abc". In that case Check is generic, while
// Instance includes server_url=abc so a bypass can subtract only that exact
// runtime decision.
type GrantCheck struct {
	Check    Check
	Instance Selector
}

func (g GrantCheck) Evaluate(grants []Grant) (GrantExpressionResult, error) {
	if err := validateInput(g.Check); err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, err
	}

	grant, _ := matchingAllowGrant(grants, g.Check.expand())
	if grant == nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonMissingBase}, nil
	}

	return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
}

func (g GrantCheck) grantSet(grants []Grant) (grantSet, error) {
	if err := validateInput(g.Check); err != nil {
		return nil, err
	}

	checks := g.Check.expand()
	if err := rejectDenyGrantsForExpressionScope(grants, g.Check.Scope); err != nil {
		return nil, err
	}

	// matchingAllowGrant already applies Check expansion and the Check's
	// selector match mode. Grant expressions do not reimplement selector logic.
	grant, _ := matchingAllowGrant(grants, checks)
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

func (g GrantCheck) instanceSelectorView() Selector {
	if len(g.Instance) > 0 {
		return g.Instance
	}
	return g.Check.selector()
}

// GrantUnion represents a set union of grant expressions. It is mostly useful
// as the exclusion side of a GrantDifference when several grants can subtract
// the same base permission.
type GrantUnion struct {
	Expressions []GrantExpression
}

func (g GrantUnion) Evaluate(grants []Grant) (GrantExpressionResult, error) {
	for _, expression := range g.Expressions {
		if expression == nil {
			return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, errors.New("grant union requires non-nil expressions")
		}

		result, err := expression.Evaluate(grants)
		if err != nil {
			return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate union expression: %w", err)
		}
		if result.Satisfied {
			return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
		}
	}

	return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonMissingBase}, nil
}

func (g GrantUnion) grantSet(grants []Grant) (grantSet, error) {
	union := grantSet{}
	for _, expression := range g.Expressions {
		if expression == nil {
			return nil, errors.New("grant union requires non-nil expressions")
		}

		set, err := expression.grantSet(grants)
		if err != nil {
			return nil, fmt.Errorf("evaluate union expression: %w", err)
		}
		union.addAll(set)
	}

	return union, nil
}

// GrantDifference represents "Base is allowed unless Exclusion also applies".
//
// It evaluates Base and Exclusion into sets of concrete permission instances,
// removes every exclusion instance from the base set, and is satisfied when at
// least one base instance remains.
//
// Examples:
//
//	risk_policy:evaluate - risk_policy:bypass
//	mcp:connect - mcp:block
//
// The scopes on the two sides do not have to be the same. What matters is that
// both sides produce the same Instance selector when the exclusion should
// remove the base permission.
type GrantDifference struct {
	Base      GrantExpression
	Exclusion GrantExpression
}

// Evaluate answers whether Base still proves access after subtracting
// Exclusion. See ../../../docs/rbac.md#grant-expressions-and-set-difference for
// concrete risk-policy and MCP examples.
//
// The direct GrantCheck - GrantCheck case skips grantSet construction because
// most runtime rules prove exactly one concrete permission instance. Complex or
// nested expressions still use the generic grantSet path below.
func (g GrantDifference) Evaluate(grants []Grant) (GrantExpressionResult, error) {
	if g.Base == nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, errors.New("grant difference requires a base expression")
	}
	if g.Exclusion == nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, errors.New("grant difference requires an exclusion expression")
	}

	baseCheck, baseIsCheck := g.Base.(GrantCheck)
	exclusionCheck, exclusionIsCheck := g.Exclusion.(GrantCheck)
	if baseIsCheck && exclusionIsCheck {
		return evaluateGrantCheckDifference(grants, baseCheck, exclusionCheck)
	}

	baseResult, err := g.Base.Evaluate(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate base expression: %w", err)
	}
	exclusion, err := g.Exclusion.grantSet(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate exclusion expression set: %w", err)
	}
	if !baseResult.Satisfied {
		return baseResult, nil
	}

	// Evaluate above gives us the best reason to return when the base is not
	// satisfied. From here on we need the actual base and exclusion sets so the
	// exclusion removes only matching permission instances.
	base, err := g.Base.grantSet(grants)
	if err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, fmt.Errorf("evaluate base expression set: %w", err)
	}

	base.subtract(exclusion)
	if len(base) == 0 {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonExclusionMatched}, nil
	}

	return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
}

func (g GrantDifference) grantSet(grants []Grant) (grantSet, error) {
	if g.Base == nil {
		return nil, errors.New("grant difference requires a base expression")
	}
	if g.Exclusion == nil {
		return nil, errors.New("grant difference requires an exclusion expression")
	}

	base, err := g.Base.grantSet(grants)
	if err != nil {
		return nil, fmt.Errorf("evaluate base expression: %w", err)
	}
	exclusion, err := g.Exclusion.grantSet(grants)
	if err != nil {
		return nil, fmt.Errorf("evaluate exclusion expression: %w", err)
	}
	base.subtract(exclusion)
	return base, nil
}

func (s grantSet) add(member grantSetMember) {
	s[grantSetKey(member.Selector)] = member
}

func (s grantSet) addAll(other grantSet) {
	maps.Copy(s, other)
}

// subtract removes every permission instance from s that is also present in
// other.
//
// This is the "unless" part of a GrantDifference. If the base set contains:
//
//	{risk_policy policy_123 server_url=abc}
//	{risk_policy policy_123 server_url=cde}
//
// and the exclusion set contains:
//
//	{risk_policy policy_123 server_url=abc}
//
// then only the abc instance is removed. The cde instance remains, so the
// expression is still satisfied for cde. We delete by canonical selector key
// instead of comparing maps directly because Go maps are not comparable.
func (s grantSet) subtract(other grantSet) {
	for key := range other {
		delete(s, key)
	}
}

// rejectDenyGrantsForExpressionScope prevents legacy deny grants from being
// interpreted inside set-expression evaluation. Deny grants have their own
// deny-wins semantics in Require/RequireAny; grant expressions use explicit
// set subtraction instead. Mixing both for the same referenced scope would make
// the result ambiguous, so the expression fails fast even if the deny selector
// would not match this specific runtime instance.
func rejectDenyGrantsForExpressionScope(grants []Grant, scope Scope) error {
	for _, grant := range grants {
		if grant.Effect != PolicyEffectDeny {
			continue
		}
		if grant.Scope != scope {
			continue
		}
		return fmt.Errorf("%w: deny grant with scope %q cannot be evaluated by grant expressions", ErrUnsupportedMixedGrantSemantics, grant.Scope)
	}
	return nil
}

func evaluateGrantCheckDifference(grants []Grant, base GrantCheck, exclusion GrantCheck) (GrantExpressionResult, error) {
	if err := validateInput(base.Check); err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, err
	}
	if err := validateInput(exclusion.Check); err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, err
	}
	if err := rejectDenyGrantsForExpressionScope(grants, base.Check.Scope); err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, err
	}
	if err := rejectDenyGrantsForExpressionScope(grants, exclusion.Check.Scope); err != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonError}, err
	}

	baseGrant, _ := matchingAllowGrant(grants, base.Check.expand())
	if baseGrant == nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonMissingBase}, nil
	}

	if !selectorsEqual(base.instanceSelectorView(), exclusion.instanceSelectorView()) {
		return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
	}

	exclusionGrant, _ := matchingAllowGrant(grants, exclusion.Check.expand())
	if exclusionGrant != nil {
		return GrantExpressionResult{Satisfied: false, Reason: GrantExpressionReasonExclusionMatched}, nil
	}

	return GrantExpressionResult{Satisfied: true, Reason: GrantExpressionReasonMatched}, nil
}

func selectorsEqual(left Selector, right Selector) bool {
	if len(left) != len(right) {
		return false
	}

	for key, leftValue := range left {
		rightValue, ok := right[key]
		if !ok || rightValue != leftValue {
			return false
		}
	}

	return true
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

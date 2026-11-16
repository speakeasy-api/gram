package authz

import (
	"maps"
	"slices"
)

// Check describes a single authorization requirement. ResourceID identifies the
// resource. ResourceKind overrides the kind derived from the scope — leave empty
// to derive automatically. Dimensions carry optional narrowing attributes (tool,
// disposition, collection) for multi-dimensional checks.
type Check struct {
	Scope        Scope
	ResourceKind string
	ResourceID   string
	Dimensions   map[string]string
	// selectorMatch controls how allow grant selectors are matched. The zero
	// value lets a check leave a grant key out only in the cases mayOmit
	// names; selectorMatchStrict allows none of them.
	selectorMatch selectorMatchMode
	// representedUser binds $represented_user; only the engine sets it.
	representedUser string
	// probe marks a check that asks whether the caller could act on some
	// resource matching it, for discovery. Can and Filter accept it; Require
	// refuses it. Set it with Probe.
	probe bool
	// anyOf holds the alternatives of a check built by AnyOf. Only Require and
	// Can accept such a check, and only on its own.
	anyOf []Check
	// operation says what the check asks to do. Constructors set it; the
	// matcher reads it instead of guessing from which dimensions are present.
	operation operation
}

// operation is what a check asks to do. The zero value is an ordinary action
// on one resource.
type operation int

const (
	operationDefault operation = iota
	// operationToolCall is calling one tool on an MCP server. It always
	// carries the tool and its disposition.
	operationToolCall
	// operationConnection is connecting to an MCP server before any tool is
	// called. It names no tool, so a grant limited to some tools still lets
	// the caller connect.
	operationConnection
	// operationPolicyAudience asks whether a risk policy's audience includes
	// the caller. It names no server, so an audience grant limited to some
	// servers still places the caller under the policy.
	operationPolicyAudience
)

// asToolCall marks c as a tool call. Only the tool-call constructors use it.
func (c Check) asToolCall() Check {
	c.operation = operationToolCall
	return c
}

// isToolCall reports whether the check was built as a tool call.
func (c Check) isToolCall() bool {
	return c.operation == operationToolCall
}

// asConnection marks c as a connection. Only the connect constructors use it.
func (c Check) asConnection() Check {
	c.operation = operationConnection
	return c
}

type selectorMatchMode int

const (
	selectorMatchNormal selectorMatchMode = iota
	selectorMatchStrict
)

// WithStrictSelectorMatch returns a copy that only accepts grants whose every
// selector constraint is represented by this check, with none of the named
// omissions of mayOmit. Use it for blocks, and when proving that requested
// authority is no broader than a live grant.
func (c Check) WithStrictSelectorMatch() Check {
	c.selectorMatch = selectorMatchStrict
	return c
}

// Probe marks the check as a discovery question: could the caller act on
// some resource matching it. A grant key the check leaves out matches any
// value the grant names. Can and Filter accept a probe; Require refuses one,
// because gating an action needs the concrete resource.
func (c Check) Probe() Check {
	c.probe = true
	return c
}

// AnyOf requests any one of the alternatives, the way RequireAnyUnblocked
// does: a block matching any alternative refuses the request, unless a
// direct grant on the concrete resource outranks it, and otherwise one
// alternative must be allowed. Use it when a project-wide grant and a grant
// on the resource both authorize the operation, so a broad allow cannot
// bypass a block on the actual resource.
//
// Only Require and Can accept the result, and only as their single check.
func AnyOf(alternatives ...Check) Check {
	var first Check
	if len(alternatives) > 0 {
		first = alternatives[0]
	}
	return Check{
		Scope:           first.Scope,
		ResourceKind:    first.ResourceKind,
		ResourceID:      first.ResourceID,
		Dimensions:      nil,
		selectorMatch:   selectorMatchNormal,
		representedUser: "",
		probe:           false,
		anyOf:           append([]Check{}, alternatives...),
		operation:       operationDefault,
	}
}

// matchesAllowSelector reports whether an allow grant's selector covers the
// check. Every key the grant names must be carried by the check with a
// matching value. A key the check does not carry never matches, except in the
// cases mayOmit names: absence is not permission.
func (c Check) matchesAllowSelector(grant Selector) bool {
	_, matched := c.keysRefusingGrant(grant)
	return matched
}

// keysRefusingGrant compares an allow grant's selector with the check. It
// returns matched when the grant covers the check. Otherwise missing lists the
// grant keys the check does not carry, when those alone are why the grant was
// refused; it is empty when a carried value differs.
func (c Check) keysRefusingGrant(grant Selector) (missing []string, matched bool) {
	carried := c.selector()
	for key, grantVal := range grant {
		checkVal, ok := carried[key]
		if !ok {
			if !c.mayOmit(key) {
				missing = append(missing, key)
			}
			continue
		}
		if !selectorValueMatches(key, grantVal, checkVal, c.representedUser) {
			return nil, false
		}
	}
	return missing, len(missing) == 0
}

// mayOmit reports whether the check may leave out a key that a grant names
// and still match the grant. These are the only cases:
//
//   - a probe, which asks about some resource and not a concrete one;
//   - a connection, which names no tool, against a tool or disposition limit;
//   - a risk policy audience question, which names no server, against a
//     server limit. Matching there places the caller under the policy.
//
// A strictly matched check and an auth-client check may omit nothing.
func (c Check) mayOmit(key string) bool {
	if c.selectorMatch == selectorMatchStrict || c.isAuthClientCheck() {
		return false
	}
	if c.probe {
		return true
	}
	switch c.operation {
	case operationConnection:
		return key == SelectorKeyTool || key == SelectorKeyDisposition
	case operationPolicyAudience:
		return key == SelectorKeyServerURL || key == SelectorKeyServerIdentity
	case operationDefault, operationToolCall:
		return false
	}
	return false
}

// isAuthClientCheck reports whether c targets an auth client. Those checks
// may omit nothing, even as a probe.
func (c Check) isAuthClientCheck() bool {
	return c.ResourceKind == ResourceKindMCPAuthClient || ResourceKindForScope(c.Scope) == ResourceKindMCPAuthClient
}

// resourceKind is the check's resource kind: ResourceKind when set, otherwise
// the kind derived from the scope.
func (c Check) resourceKind() string {
	if c.ResourceKind != "" {
		return c.ResourceKind
	}
	return ResourceKindForScope(c.Scope)
}

// selector converts the check into a Selector for matching against grant selectors.
func (c Check) selector() Selector {
	s := Selector{
		SelectorKeyResourceKind: c.resourceKind(),
		SelectorKeyResourceID:   c.ResourceID,
	}
	maps.Copy(s, c.Dimensions)
	return s
}

// GrantsContainSelector reports allow-grant containment for a delegation set,
// including wildcard resource IDs. Unlike runtime checks this does not require
// a concrete resource. Every parent dimension must be retained by the candidate.
// This is allow-only: callers must separately reject overlapping exclusions.
func GrantsContainSelector(grants []Grant, scope Scope, selector Selector) bool {
	return GrantsContainSelectorFor("", grants, scope, selector)
}

// GrantsContainSelectorFor is GrantsContainSelector acting for representedUser,
// so a $represented_user grant covers that user's own connection.
func GrantsContainSelectorFor(representedUser string, grants []Grant, scope Scope, selector Selector) bool {
	// The candidate may be narrowed from a stored grant, so a limit saved
	// before limits moved to connect is still accepted. Callers that take a
	// selector from a request validate it with ValidateSelector first.
	if selector[SelectorKeyResourceID] == "" || ValidateStoredSelector(scope, selector) != nil {
		return false
	}
	dimensions := maps.Clone(selector)
	delete(dimensions, SelectorKeyResourceKind)
	delete(dimensions, SelectorKeyResourceID)
	check := Check{Scope: scope, ResourceKind: selector[SelectorKeyResourceKind], ResourceID: selector[SelectorKeyResourceID], Dimensions: dimensions, selectorMatch: selectorMatchStrict, representedUser: representedUser, probe: false, anyOf: nil, operation: operationDefault}
	grant, _ := matchingGrant(grants, check.expand())
	return grant != nil
}

// expand returns all scope variants that would satisfy this check: the check's
// own scope, any scopes configured as expansions for it, and ScopeRoot.
// Selector matching handles wildcard grants natively ({"resource_id":"*"}
// matches any check), so we only need one entry per scope level.
func (c Check) expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch, representedUser: c.representedUser, probe: c.probe, anyOf: nil, operation: c.operation},
		{Scope: c.Scope, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch, representedUser: c.representedUser, probe: c.probe, anyOf: nil, operation: c.operation},
	}
	// Additional scopes that also satisfy this check.
	for _, s := range scopeExpansions[c.Scope] {
		checks = append(checks, Check{Scope: s, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch, representedUser: c.representedUser, probe: c.probe, anyOf: nil, operation: c.operation})
	}
	// Root administers everything but uses no auth client.
	if c.isAuthClientCheck() {
		checks = slices.DeleteFunc(checks, func(e Check) bool { return e.Scope == ScopeRoot })
	}
	return checks
}

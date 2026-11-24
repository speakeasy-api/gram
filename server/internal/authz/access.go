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
	// exact refuses every grant key the check leaves out, even one its
	// scope opens. Blocks, exemptions, containment, direct overrides of a
	// tool call, auth client checks and risk policy bypass checks are matched
	// exactly. One exception: a block limited to a tool or a disposition
	// still matches a tool call that leaves that key out, because the missing
	// value may be the one blocked (blockCoversMissingValue). Only authz
	// constructors and engine paths set it.
	exact bool
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
		exact:           false,
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
			if c.blockCoversMissingValue(key) {
				continue
			}
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

// blockCoversMissingValue reports whether a block's limit on key applies to a
// tool call that does not carry key. Allows never match a value the call does
// not carry (absence is not permission), so a missing value is refused either
// way; this only decides whether a block still takes the call away.
//
// For example, an administrator blocks destructive tools on server X
// (mcp:blocked_connect on X with disposition=destructive). A user calls a tool
// on X whose annotations could not be read, so the call carries no
// disposition. Under exact matching the block would not match, and the
// user's allow on X would let a possibly destructive call through. Here the
// block applies: a value we do not know may be the one blocked. The same holds
// for a block on a named tool and a call that names no tool.
func (c Check) blockCoversMissingValue(key string) bool {
	return (key == SelectorKeyDisposition || key == SelectorKeyTool) && c.isToolCall() && IsBlocklistScope(c.Scope)
}

// mayOmit reports whether the check may leave out a key that a grant of its
// scope names and still match the grant. The registry declares these open
// keys per permission (scopeOpenKeys), for three kinds of check only:
//
//   - a probe, which asks about some resource and not a concrete one;
//   - a connection, which names no tool, against a tool or disposition limit;
//   - a risk policy audience question, which names no server, against a
//     server limit. Matching there places the caller under the policy.
//
// An exact check may omit nothing, and neither may any check of another
// kind.
func (c Check) mayOmit(key string) bool {
	if c.exact {
		return false
	}
	open := scopeOpenKeys[c.Scope]
	if c.probe && slices.Contains(open.Probe, key) {
		return true
	}
	switch c.operation {
	case operationConnection:
		return slices.Contains(open.Connection, key)
	case operationDefault, operationToolCall:
		return false
	}
	return false
}

// isAuthClientCheck reports whether c targets an auth client.
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
	if selector[SelectorKeyResourceID] == "" || ValidateSelector(scope, selector) != nil {
		return false
	}
	dimensions := maps.Clone(selector)
	delete(dimensions, SelectorKeyResourceKind)
	delete(dimensions, SelectorKeyResourceID)
	check := Check{Scope: scope, ResourceKind: selector[SelectorKeyResourceKind], ResourceID: selector[SelectorKeyResourceID], Dimensions: dimensions, exact: true, representedUser: representedUser, probe: false, anyOf: nil, operation: operationDefault}
	grant, _ := matchingGrant(grants, check.expand())
	return grant != nil
}

// expand returns all scope variants that would satisfy this check: the check's
// own scope, any scopes configured as expansions for it, and ScopeRoot.
// Selector matching handles wildcard grants natively ({"resource_id":"*"}
// matches any check), so we only need one entry per scope level.
func (c Check) expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, exact: c.exact, representedUser: c.representedUser, probe: c.probe, anyOf: nil, operation: c.operation},
		{Scope: c.Scope, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, exact: c.exact, representedUser: c.representedUser, probe: c.probe, anyOf: nil, operation: c.operation},
	}
	// Additional scopes that also satisfy this check.
	for _, s := range scopeExpansions[c.Scope] {
		checks = append(checks, Check{Scope: s, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, exact: c.exact, representedUser: c.representedUser, probe: c.probe, anyOf: nil, operation: c.operation})
	}
	// Root administers everything but uses no auth client.
	if c.isAuthClientCheck() {
		checks = slices.DeleteFunc(checks, func(e Check) bool { return e.Scope == ScopeRoot })
	}
	return checks
}

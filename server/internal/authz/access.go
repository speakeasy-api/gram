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
	// value preserves the default permissive dimension matching.
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
	// organization is the organization the caller says the resource belongs
	// to, set with InOrganization from the row the handler loaded. A check on
	// an org scope needs none: its resource is the organization.
	organization Option[OrgID]
}

// InOrganization states the organization the check's resource belongs to.
// Take it from the row the handler loaded, not from the session: the engine
// compares it with the organization the caller is acting in, so a resource
// loaded from another organization is caught even when its id is well formed.
// On an AnyOf it applies to every alternative.
func (c Check) InOrganization(organization OrgID) Check {
	c.organization = Some(organization)
	if c.anyOf != nil {
		alternatives := make([]Check, len(c.anyOf))
		for i, alternative := range c.anyOf {
			alternative.organization = Some(organization)
			alternatives[i] = alternative
		}
		c.anyOf = alternatives
	}
	return c
}

// statedOrganization is the organization a check names: the resource of an
// org check, otherwise the one given to InOrganization. The second result is
// false when the check names none, or states one that is absent, which
// validation refuses.
func (c Check) statedOrganization() (string, bool) {
	if c.resourceKind() == ResourceKindOrg {
		return c.ResourceID, true
	}
	organization, ok := c.organization.Get()
	if !ok || organization.String() == "" {
		return "", false
	}
	return organization.String(), true
}

// operation is what a check asks to do. The zero value is an ordinary action
// on one resource, or a connection when the scope is the connect permission.
type operation int

const (
	operationDefault operation = iota
	// operationToolCall is calling one tool on an MCP server. It always
	// carries the tool and its disposition.
	operationToolCall
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

type selectorMatchMode int

const (
	selectorMatchNormal selectorMatchMode = iota
	selectorMatchStrict
	// selectorMatchBlock matches block grants: strictly, with an unknown
	// check value matched by any constraint (see SelectorValueUnknown).
	selectorMatchBlock
)

// WithStrictSelectorMatch returns a copy that only accepts grants whose every
// selector constraint is represented by this check. Use it when proving that
// requested authority is no broader than a live grant.
func (c Check) WithStrictSelectorMatch() Check {
	c.selectorMatch = selectorMatchStrict
	return c
}

// Probe marks the check as a discovery question: could the caller act on
// some resource matching it. A dimension the check leaves out matches any
// value a grant names. Can and Filter accept a probe; Require refuses one,
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
		organization:    None[OrgID](),
	}
}

func (c Check) matchesAllowSelector(selector Selector) bool {
	if c.selectorMatch == selectorMatchBlock {
		return selector.blockMatches(c.selector(), c.representedUser)
	}
	if c.selectorMatch == selectorMatchStrict || c.isAuthClientCheck() {
		return selector.strictMatches(c.selector(), c.representedUser)
	}
	return selector.matches(c.selector(), c.representedUser)
}

// exclusionCheck is the check that looks for exclusion grants against c: the
// same resource and dimensions under the exclusion scope, matched strictly. A
// block also matches values c could not determine; an exemption such as
// risk_policy:bypass never does, so an unknown value cannot exempt anyone.
func (c Check) exclusionCheck(exclusion Scope) Check {
	mode := selectorMatchStrict
	if IsBlocklistScope(exclusion) {
		mode = selectorMatchBlock
	}
	return Check{Scope: exclusion, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: mode, representedUser: c.representedUser, probe: false, anyOf: nil, operation: c.operation, organization: c.organization}
}

// isAuthClientCheck reports whether c targets an auth client. Those checks
// always match strictly, so a grant constraint the check omits never matches.
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
	check := Check{Scope: scope, ResourceKind: selector[SelectorKeyResourceKind], ResourceID: selector[SelectorKeyResourceID], Dimensions: dimensions, selectorMatch: selectorMatchStrict, representedUser: representedUser, probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
	grant, _ := matchingGrant(grants, check.expand())
	return grant != nil
}

// expand returns all scope variants that would satisfy this check: the check's
// own scope, any scopes configured as expansions for it, and ScopeRoot.
// Selector matching handles wildcard grants natively ({"resource_id":"*"}
// matches any check), so we only need one entry per scope level.
func (c Check) expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch, representedUser: c.representedUser, probe: false, anyOf: nil, operation: c.operation, organization: c.organization},
		{Scope: c.Scope, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch, representedUser: c.representedUser, probe: false, anyOf: nil, operation: c.operation, organization: c.organization},
	}
	// Additional scopes that also satisfy this check.
	for _, s := range scopeExpansions[c.Scope] {
		checks = append(checks, Check{Scope: s, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch, representedUser: c.representedUser, probe: false, anyOf: nil, operation: c.operation, organization: c.organization})
	}
	// Root administers everything but uses no auth client.
	if c.isAuthClientCheck() {
		checks = slices.DeleteFunc(checks, func(e Check) bool { return e.Scope == ScopeRoot })
	}
	return checks
}

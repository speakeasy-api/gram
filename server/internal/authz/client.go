package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ResourceRef names one resource and the organization that owns it.
type ResourceRef struct {
	OrganizationID string
	Kind           string
	ID             string
}

// Target is a concrete resource plus its actual containing resources. Services
// build it from tenant-scoped records they loaded, never from request input
// alone.
type Target struct {
	Resource  ResourceRef
	Ancestors []ResourceRef
}

// Request asks whether the caller holds Permission on Target.
type Request struct {
	Permission Scope
	Target     Target
}

// Outcome is the result of deciding a set of requests.
type Outcome int

const (
	OutcomeDenied Outcome = iota
	OutcomeAllowed
	// OutcomeUnresolved means a fact the decision depends on was missing. It
	// never allows.
	OutcomeUnresolved
)

// DecisionReason explains an outcome.
type DecisionReason string

const (
	ReasonAllowed     DecisionReason = "allowed"
	ReasonNotEnforced DecisionReason = "not_enforced"
	ReasonNoGrant     DecisionReason = "no_grant"
	ReasonBlocked     DecisionReason = "blocked"
	ReasonCrossTenant DecisionReason = "cross_tenant"
	ReasonMissingFact DecisionReason = "missing_fact"
)

// DecisionBasis qualifies an allow.
type DecisionBasis string

const (
	BasisNone DecisionBasis = ""
	// BasisLegacyBroadGrant marks an allow that relied on a stored grant
	// anchored more broadly than its permission now allows, such as a wildcard
	// mcp:connect. It still allows until the grant is migrated.
	BasisLegacyBroadGrant DecisionBasis = "legacy_broad_grant"
)

// Decision is the trace of one Authorize call.
type Decision struct {
	Outcome Outcome
	Reason  DecisionReason
	Basis   DecisionBasis
	// Request is the first request that was not allowed, or the zero value.
	Request Request
	// Grants are the grants that allowed each request, in request order. Empty
	// when RBAC is not enforced for the caller.
	Grants []Grant
}

// Allowed reports whether every request was allowed.
func (d Decision) Allowed() bool { return d.Outcome == OutcomeAllowed }

// Backend evaluates checks for a Client. It is selected once, in wiring.
type Backend interface {
	evaluate(ctx context.Context, checks []Check) ([]backendDecision, error)
	enforce(ctx context.Context, checks []Check) error
	reject(ctx context.Context, err error) error
}

type backendDecision struct {
	allowed bool
	blocked bool
	// grant is the grant that allowed the check, nil when it was denied or
	// when RBAC is not enforced.
	grant *Grant
}

// Client answers typed authorization requests. It validates every request
// against the permission definitions and the caller's organization, then
// delegates evaluation to its Backend. It never evaluates grants itself.
type Client struct {
	backend     Backend
	definitions map[Scope]PermissionDefinition
}

// NewClient returns a Client over backend for the given definitions.
func NewClient(backend Backend, definitions []PermissionDefinition) (*Client, error) {
	if backend == nil {
		return nil, errors.New("authz client requires a backend")
	}
	if err := validateDefinitions(definitions); err != nil {
		return nil, fmt.Errorf("validate permission definitions: %w", err)
	}
	byName := make(map[Scope]PermissionDefinition, len(definitions))
	for _, def := range definitions {
		byName[def.Name] = def.clone()
	}
	return &Client{backend: backend, definitions: byName}, nil
}

// NewEngineClient returns a Client over the engine for every registered
// definition.
func NewEngineClient(engine *Engine) *Client {
	client, err := NewClient(NewEngineBackend(engine), AllDefinitions())
	if err != nil {
		panic(err)
	}
	return client
}

// Authorize decides requests without recording a challenge. Every request
// must be allowed for the decision to allow. Use it for capability responses;
// it reaches the same decision Require enforces.
func (c *Client) Authorize(ctx context.Context, requests ...Request) (Decision, error) {
	checks, decision, err := c.prepare(ctx, requests)
	if err != nil {
		return Decision{}, c.backend.reject(ctx, err)
	}
	if decision.Outcome != OutcomeAllowed {
		return decision, nil
	}
	results, err := c.backend.evaluate(ctx, checks)
	if err != nil {
		return Decision{}, err
	}
	return c.decide(requests, results), nil
}

// Require enforces requests: every one must be allowed. A cross-tenant target
// or a missing fact is forbidden before any grant is read. The backend records
// the challenge; the returned decision carries the basis of the allow, such as
// a legacy broad grant.
func (c *Client) Require(ctx context.Context, requests ...Request) (Decision, error) {
	checks, decision, err := c.prepare(ctx, requests)
	if err != nil {
		return Decision{}, c.backend.reject(ctx, err)
	}
	if decision.Outcome != OutcomeAllowed {
		return decision, c.backend.reject(ctx, Denied(decision.Request.Permission, nil))
	}
	if err := c.backend.enforce(ctx, checks); err != nil {
		return Decision{}, err
	}
	results, err := c.backend.evaluate(ctx, checks)
	if err != nil {
		return Decision{}, err
	}
	decision = c.decide(requests, results)
	if !decision.Allowed() {
		return decision, c.backend.reject(ctx, errors.New("authz backend enforced a request it does not allow"))
	}
	return decision, nil
}

// prepare validates requests and builds their checks. An error means a
// malformed request, a programming mistake. A non-allowing decision means a
// well-formed request that is refused before grants are consulted.
func (c *Client) prepare(ctx context.Context, requests []Request) ([]Check, Decision, error) {
	if len(requests) == 0 {
		return nil, Decision{}, ErrNoChecks
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, Decision{}, oops.C(oops.CodeUnauthorized)
	}
	checks := make([]Check, 0, len(requests))
	for _, request := range requests {
		def, defined := c.definitions[request.Permission]
		if !defined {
			return nil, Decision{}, invalidCheckReason(request.Permission, request.Target.Resource.ID, "permission has no definition")
		}
		resource := request.Target.Resource
		if resource.Kind != def.Resource || resource.ID == "" || resource.ID == WildcardResource {
			return nil, Decision{}, invalidCheckReason(request.Permission, resource.ID, "target must be one concrete "+def.Resource)
		}
		if resource.OrganizationID != authCtx.ActiveOrganizationID {
			return nil, refused(request, ReasonCrossTenant), nil
		}
		dimensions := map[string]string{}
		for _, kind := range def.RequiredAncestors {
			ancestor, reason := requiredAncestor(request.Target, kind)
			if reason != "" {
				return nil, refused(request, reason), nil
			}
			if kind == ResourceKindProject {
				dimensions[SelectorKeyProjectID] = ancestor.ID
			}
		}
		checks = append(checks, Check{Scope: def.Name, ResourceKind: def.Resource, ResourceID: resource.ID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: ""})
	}
	return checks, Decision{Outcome: OutcomeAllowed, Reason: ReasonAllowed, Basis: BasisNone, Request: Request{}, Grants: nil}, nil
}

// requiredAncestor returns the target's one ancestor of kind. Every ancestor
// must belong to the target's organization, and an organization ancestor must
// be that organization.
func requiredAncestor(target Target, kind string) (ResourceRef, DecisionReason) {
	var found []ResourceRef
	for _, ancestor := range target.Ancestors {
		if ancestor.Kind == kind {
			found = append(found, ancestor)
		}
	}
	if len(found) != 1 || found[0].ID == "" || found[0].ID == WildcardResource {
		return ResourceRef{}, ReasonMissingFact
	}
	ancestor := found[0]
	if ancestor.OrganizationID != target.Resource.OrganizationID {
		return ResourceRef{}, ReasonCrossTenant
	}
	if kind == ResourceKindOrg && ancestor.ID != target.Resource.OrganizationID {
		return ResourceRef{}, ReasonCrossTenant
	}
	return ancestor, ""
}

func refused(request Request, reason DecisionReason) Decision {
	outcome := OutcomeDenied
	if reason == ReasonMissingFact {
		outcome = OutcomeUnresolved
	}
	return Decision{Outcome: outcome, Reason: reason, Basis: BasisNone, Request: request, Grants: nil}
}

func (c *Client) decide(requests []Request, results []backendDecision) Decision {
	decision := Decision{Outcome: OutcomeAllowed, Reason: ReasonAllowed, Basis: BasisNone, Request: Request{}, Grants: nil}
	for i, result := range results {
		if !result.allowed {
			reason := ReasonNoGrant
			if result.blocked {
				reason = ReasonBlocked
			}
			return Decision{Outcome: OutcomeDenied, Reason: reason, Basis: BasisNone, Request: requests[i], Grants: nil}
		}
		if result.grant == nil {
			decision.Reason = ReasonNotEnforced
			continue
		}
		decision.Grants = append(decision.Grants, *result.grant)
		if result.grant.Scope != ScopeRoot && !c.definitions[requests[i].Permission].anchoredWithin(result.grant.Selector) {
			decision.Basis = BasisLegacyBroadGrant
		}
	}
	return decision
}

// engineBackend evaluates with the Engine's prepared grants and policies, so
// the Client sees exactly what Require and Evaluate see.
type engineBackend struct {
	engine *Engine
}

// NewEngineBackend adapts the existing engine and stored grants.
func NewEngineBackend(engine *Engine) Backend {
	return engineBackend{engine: engine}
}

func (b engineBackend) evaluate(ctx context.Context, checks []Check) ([]backendDecision, error) {
	e := b.engine
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]backendDecision, len(checks))
	if !enforce {
		for i, check := range checks {
			results[i] = backendDecision{allowed: unenforcedAllows(check), blocked: false, grant: nil}
		}
		return results, nil
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return nil, e.mapError(ctx, ErrMissingGrants)
	}
	for i, check := range checks {
		if err := validateInput(check); err != nil {
			return nil, e.mapError(ctx, err)
		}
		evaluation, err := authorization.evaluate(check)
		if err != nil {
			return nil, e.mapError(ctx, err)
		}
		results[i] = backendDecision{allowed: evaluation.Grant != nil, blocked: evaluation.Denied, grant: evaluation.Grant}
	}
	return results, nil
}

func (b engineBackend) enforce(ctx context.Context, checks []Check) error {
	return b.engine.Require(ctx, checks...)
}

func (b engineBackend) reject(ctx context.Context, err error) error {
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) {
		return err
	}
	return b.engine.mapError(ctx, err)
}

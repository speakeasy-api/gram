package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/authz"
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
	Permission authz.Scope
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

// Reason explains an outcome.
type Reason string

const (
	ReasonAllowed     Reason = "allowed"
	ReasonNotEnforced Reason = "not_enforced"
	ReasonNoGrant     Reason = "no_grant"
	ReasonBlocked     Reason = "blocked"
	ReasonCrossTenant Reason = "cross_tenant"
	ReasonMissingFact Reason = "missing_fact"
)

// Basis qualifies an allow.
type Basis string

const (
	BasisNone Basis = ""
	// BasisLegacyBroadGrant marks an allow that relied on a stored grant
	// anchored more broadly than its permission now allows, such as a wildcard
	// mcp:connect. It still allows until the grant is migrated.
	BasisLegacyBroadGrant Basis = "legacy_broad_grant"
)

// Decision is the trace of one Authorize or Require call.
type Decision struct {
	Outcome Outcome
	Reason  Reason
	Basis   Basis
	// Request is the first request that was not allowed, or the zero value.
	Request Request
	// Grants are the grants that allowed each request, in request order. Empty
	// when RBAC is not enforced for the caller.
	Grants []authz.Grant
}

// Allowed reports whether every request was allowed.
func (d Decision) Allowed() bool { return d.Outcome == OutcomeAllowed }

// Client answers typed requests. It validates every request against its
// Definition and the caller's organization, then delegates evaluation to its
// Backend. It never evaluates grants itself.
type Client struct {
	backend     Backend
	definitions map[authz.Scope]Definition
}

// NewClient returns a Client over backend for the given definitions.
func NewClient(backend Backend, defs []Definition) (*Client, error) {
	if backend == nil {
		return nil, errors.New("catalog client requires a backend")
	}
	if err := validate(defs); err != nil {
		return nil, fmt.Errorf("validate permission definitions: %w", err)
	}
	byName := make(map[authz.Scope]Definition, len(defs))
	for _, def := range defs {
		byName[def.Name] = def.clone()
	}
	return &Client{backend: backend, definitions: byName}, nil
}

// NewEngineClient returns a Client over the engine for every registered
// definition.
func NewEngineClient(engine *authz.Engine) *Client {
	client, err := NewClient(NewEngineBackend(engine), Definitions())
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
	if !decision.Allowed() {
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
// the challenge; the returned decision carries the basis of the allow.
func (c *Client) Require(ctx context.Context, requests ...Request) (Decision, error) {
	checks, decision, err := c.prepare(ctx, requests)
	if err != nil {
		return Decision{}, c.backend.reject(ctx, err)
	}
	if !decision.Allowed() {
		return decision, c.backend.reject(ctx, authz.Denied(decision.Request.Permission, nil))
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
// well-formed request refused before grants are consulted.
func (c *Client) prepare(ctx context.Context, requests []Request) ([]authz.Check, Decision, error) {
	if len(requests) == 0 {
		return nil, Decision{}, authz.ErrNoChecks
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, Decision{}, oops.C(oops.CodeUnauthorized)
	}
	checks := make([]authz.Check, 0, len(requests))
	for _, request := range requests {
		resource := request.Target.Resource
		def, defined := c.definitions[request.Permission]
		if !defined {
			return nil, Decision{}, fmt.Errorf("permission has no definition: %w", authz.InvalidCheck(request.Permission, resource.ID))
		}
		if resource.Kind != def.Resource || resource.ID == "" || resource.ID == authz.WildcardResource {
			return nil, Decision{}, fmt.Errorf("target must be one concrete %s: %w", def.Resource, authz.InvalidCheck(request.Permission, resource.ID))
		}
		if resource.OrganizationID != authCtx.ActiveOrganizationID {
			return nil, refused(request, ReasonCrossTenant), nil
		}
		projectID := ""
		for _, kind := range def.RequiredAncestors {
			ancestor, reason := requiredAncestor(request.Target, kind)
			if reason != "" {
				return nil, refused(request, reason), nil
			}
			if kind == authz.ResourceKindProject {
				projectID = ancestor.ID
			}
		}
		checks = append(checks, authz.MCPCheck(def.Name, resource.ID, projectID))
	}
	return checks, Decision{Outcome: OutcomeAllowed, Reason: ReasonAllowed, Basis: BasisNone, Request: Request{}, Grants: nil}, nil
}

// requiredAncestor returns the target's one ancestor of kind. Every ancestor
// must belong to the target's organization, and an organization ancestor must
// be that organization.
func requiredAncestor(target Target, kind string) (ResourceRef, Reason) {
	var found []ResourceRef
	for _, ancestor := range target.Ancestors {
		if ancestor.Kind == kind {
			found = append(found, ancestor)
		}
	}
	if len(found) != 1 || found[0].ID == "" || found[0].ID == authz.WildcardResource {
		return ResourceRef{}, ReasonMissingFact
	}
	ancestor := found[0]
	if ancestor.OrganizationID != target.Resource.OrganizationID {
		return ResourceRef{}, ReasonCrossTenant
	}
	if kind == authz.ResourceKindOrg && ancestor.ID != target.Resource.OrganizationID {
		return ResourceRef{}, ReasonCrossTenant
	}
	return ancestor, ""
}

func refused(request Request, reason Reason) Decision {
	outcome := OutcomeDenied
	if reason == ReasonMissingFact {
		outcome = OutcomeUnresolved
	}
	return Decision{Outcome: outcome, Reason: reason, Basis: BasisNone, Request: request, Grants: nil}
}

func (c *Client) decide(requests []Request, results []authz.CheckResult) Decision {
	decision := Decision{Outcome: OutcomeAllowed, Reason: ReasonAllowed, Basis: BasisNone, Request: Request{}, Grants: nil}
	for i, result := range results {
		if !result.Allowed {
			reason := ReasonNoGrant
			if result.Blocked {
				reason = ReasonBlocked
			}
			return Decision{Outcome: OutcomeDenied, Reason: reason, Basis: BasisNone, Request: requests[i], Grants: nil}
		}
		if result.Grant == nil {
			decision.Reason = ReasonNotEnforced
			continue
		}
		decision.Grants = append(decision.Grants, *result.Grant)
		if result.Grant.Scope != authz.ScopeRoot && !c.definitions[requests[i].Permission].anchoredWithin(result.Grant.Selector) {
			decision.Basis = BasisLegacyBroadGrant
		}
	}
	return decision
}

package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// EvaluateGrants reports whether one already-loaded grant set allows op,
// without reading the request's own policies, recording a challenge or
// enforcing. It answers questions about another principal's access, such as
// whether a workload's owner may still read a project, with the same routing
// as Evaluate: the unit's mode chooses the engine's grant matching, the
// registry evaluator, or both with a comparison. The grants form one policy,
// so an empty set denies. Alternatives are rejected; ask for each option.
// Errors are mapped as the client's other methods map them.
func (c *Client) EvaluateGrants(ctx context.Context, grants []Grant, op Operation) (bool, error) {
	allowed, err := c.evaluateGrants(ctx, grants, op)
	if err != nil {
		return false, c.engine.mapError(ctx, err)
	}
	return allowed, nil
}

func (c *Client) evaluateGrants(ctx context.Context, grants []Grant, op Operation) (bool, error) {
	if err := c.validate([]Operation{op}); err != nil {
		return false, err
	}
	if op.alternatives {
		return false, fmt.Errorf("%w: alternatives %s cannot be evaluated against loaded grants", ErrInvalidCheck, op)
	}
	ops := []Operation{op}
	rt := c.router.routeFor(ctx, ops)
	switch rt.mode {
	case MigrationModeNew:
		return c.evaluateGrantsNew(grants, op)
	case MigrationModeShadow:
		allowed, err := c.evaluateGrantsOld(grants, op)
		c.shadow.compare(ctx, "evaluate_grants", rt, ops, shadowAnswer{allowed: []bool{allowed}, err: err}, func() shadowAnswer {
			got, err := c.evaluateGrantsNew(grants, op)
			return shadowAnswer{allowed: []bool{got}, err: err}
		})
		return allowed, err
	default:
		return c.evaluateGrantsOld(grants, op)
	}
}

func (c *Client) evaluateGrantsOld(grants []Grant, op Operation) (bool, error) {
	if allowed, ok, err := legacyGrantDecision(grants, op); ok {
		return allowed, err
	}
	check, ok := c.legacyCheck(op)
	if !ok {
		return false, fmt.Errorf("%w: operation %s has no engine form", ErrInvalidCheck, op)
	}
	return GrantsAuthorize(grants, check)
}

func (c *Client) evaluateGrantsNew(grants []Grant, op Operation) (bool, error) {
	// The grants answer for a principal that is not the request's actor, so
	// no ownership fact can match them.
	set := PolicySet{
		Actor:           urn.NewPrincipal(urn.PrincipalTypeSystem, "loaded_grants"),
		Policies:        []Policy{{Principal: "loaded", Grants: grants}},
		Captured:        nil,
		RepresentedUser: "",
		Represented:     nil,
	}
	decision, err := c.decide(set, op)
	if err != nil {
		return false, err
	}
	return decision.Allowed, nil
}

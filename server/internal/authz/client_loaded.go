package authz

import (
	"context"
	"fmt"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadedPolicies are grants a service loaded itself for one user, rather
// than the policies the request was admitted with. Services use them where a
// decision must read someone's current authority inside their own
// transaction, such as the live grants of the human managing an agent, or of
// the user an assistant acts as. Every policy must allow.
type LoadedPolicies struct {
	// OrganizationID is the organization the grants were loaded in. It
	// routes the decision when the request itself was never admitted.
	OrganizationID string
	// User is the user whose grants these are. Ownership facts match this
	// user; an empty user matches none.
	User string
	// Policies are the loaded grant sets. Each must allow the operation.
	Policies [][]Grant
}

// RequireLoaded enforces every operation against policies, recording a
// challenge per policy as Engine.EvaluateLoadedGrants does. It ignores the
// request's own grants and its enforcement profile: the decision is about the
// loaded user, whatever transport carried the request.
func (c *Client) RequireLoaded(ctx context.Context, policies LoadedPolicies, ops ...Operation) error {
	if err := c.loadedAdmissible(ctx); err != nil {
		return err
	}
	if err := c.validateLoaded(ops); err != nil {
		return c.engine.mapError(ctx, err)
	}
	rt := c.router.routeForLoaded(ctx, ops, policies.OrganizationID)
	switch rt.mode {
	case MigrationModeNew:
		return c.requireLoadedNew(ctx, policies, ops)
	case MigrationModeShadow:
		err := c.requireLoadedOld(ctx, policies, ops)
		c.shadow.compare(ctx, "require_loaded", rt, ops, requireAnswer(err), func() shadowAnswer { return c.answerLoadedNew(policies, ops) })
		return err
	default:
		return c.requireLoadedOld(ctx, policies, ops)
	}
}

// EvaluateLoaded reports whether policies allow every operation, without
// recording a challenge. It errors only when an operation cannot be decided.
func (c *Client) EvaluateLoaded(ctx context.Context, policies LoadedPolicies, ops ...Operation) (bool, error) {
	if err := c.loadedAdmissible(ctx); err != nil {
		return false, err
	}
	if err := c.validateLoaded(ops); err != nil {
		return false, c.engine.mapError(ctx, err)
	}
	rt := c.router.routeForLoaded(ctx, ops, policies.OrganizationID)
	switch rt.mode {
	case MigrationModeNew:
		answer := c.answerLoadedNew(policies, ops)
		if answer.err != nil {
			return false, c.engine.mapError(ctx, answer.err)
		}
		return answer.allowed[0], nil
	case MigrationModeShadow:
		allowed, err := c.evaluateLoadedOld(policies, ops)
		c.shadow.compare(ctx, "evaluate_loaded", rt, ops, shadowAnswer{allowed: []bool{allowed}, err: err}, func() shadowAnswer { return c.answerLoadedNew(policies, ops) })
		if err != nil {
			return false, c.engine.mapError(ctx, err)
		}
		return allowed, nil
	default:
		allowed, err := c.evaluateLoadedOld(policies, ops)
		if err != nil {
			return false, c.engine.mapError(ctx, err)
		}
		return allowed, nil
	}
}

// loadedAdmissible refuses callers whose own credential carries a policy that
// loaded grants would bypass, as Engine.EvaluateLoadedGrants does.
func (c *Client) loadedAdmissible(ctx context.Context) error {
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return oops.C(oops.CodeForbidden)
	}
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return oops.C(oops.CodeForbidden)
	}
	return nil
}

// validateLoaded rejects calls the loaded paths cannot answer: invalid
// operations, an empty call, and alternatives, which no loaded-grant caller
// asks for.
func (c *Client) validateLoaded(ops []Operation) error {
	if len(ops) == 0 {
		return ErrNoChecks
	}
	if err := c.validate(ops); err != nil {
		return err
	}
	for _, op := range ops {
		if op.alternatives {
			return fmt.Errorf("%w: alternatives %s cannot be decided against loaded grants", ErrInvalidCheck, op)
		}
	}
	return nil
}

func (c *Client) requireLoadedOld(ctx context.Context, policies LoadedPolicies, ops []Operation) error {
	checks, err := c.loadedChecks(ops)
	if err != nil {
		return c.engine.mapError(ctx, err)
	}
	if len(policies.Policies) == 0 {
		return c.engine.EvaluateLoadedGrants(ctx, nil, checks...)
	}
	for _, grants := range policies.Policies {
		if err := c.engine.EvaluateLoadedGrants(ctx, grants, checks...); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) evaluateLoadedOld(policies LoadedPolicies, ops []Operation) (bool, error) {
	checks, err := c.loadedChecks(ops)
	if err != nil {
		return false, err
	}
	if len(policies.Policies) == 0 {
		return false, nil
	}
	for _, grants := range policies.Policies {
		for _, check := range checks {
			allowed, err := GrantsAuthorize(grants, check)
			if err != nil || !allowed {
				return false, err
			}
		}
	}
	return true, nil
}

func (c *Client) loadedChecks(ops []Operation) ([]Check, error) {
	checks := make([]Check, 0, len(ops))
	for _, op := range ops {
		check, ok := c.legacyCheck(op)
		if !ok {
			return nil, fmt.Errorf("%w: operation %s has no engine form", ErrInvalidCheck, op)
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func (c *Client) requireLoadedNew(ctx context.Context, policies LoadedPolicies, ops []Operation) error {
	set := loadedPolicySet(policies)
	checks := c.legacyChecksFor(ops)
	matches := make([]grantMatch, 0, len(ops))
	for i, op := range ops {
		decision, err := c.decide(set, op)
		if err != nil {
			c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck, set, checks.requested, checks.focus(i, -1), nil)
			return c.engine.mapError(ctx, err)
		}
		if !decision.Allowed {
			focus := checks.focus(i, decision.Option)
			c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeDeny, denialReason(decision, set), set, checks.requested, focus, nil)
			return c.engine.mapError(ctx, Denied(focus.Scope, focus.selector()))
		}
		if decision.Grant != nil {
			matches = append(matches, grantMatch{Grant: *decision.Grant, ViaCheck: *checks.focus(i, decision.Option)})
		}
	}
	c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeAllow, authzrepo.ReasonGrantMatched, set, checks.requested, checks.focus(0, -1), matches)
	return nil
}

// answerLoadedNew is the registry evaluator's answer over loaded policies.
func (c *Client) answerLoadedNew(policies LoadedPolicies, ops []Operation) shadowAnswer {
	set := loadedPolicySet(policies)
	for _, op := range ops {
		decision, err := c.decide(set, op)
		if err != nil {
			return shadowAnswer{allowed: nil, err: err}
		}
		if !decision.Allowed {
			return shadowAnswer{allowed: []bool{false}, err: nil}
		}
	}
	return shadowAnswer{allowed: []bool{true}, err: nil}
}

// loadedPolicySet composes loaded policies. The loaded user is the actor, so
// ownership facts compare against the user whose authority was loaded.
func loadedPolicySet(policies LoadedPolicies) PolicySet {
	actor := urn.Principal{Type: "", ID: ""}
	if policies.User != "" {
		actor = urn.NewPrincipal(urn.PrincipalTypeUser, policies.User)
	}
	return PolicySet{
		Actor:           actor,
		Policies:        labelPolicies("loaded", policies.Policies),
		Captured:        nil,
		RepresentedUser: "",
		Represented:     nil,
	}
}

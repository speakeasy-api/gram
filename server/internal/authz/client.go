package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Client answers typed operations against the compiled registry. It reuses
// the Engine's admission, grant loading, enforcement profile, challenge
// logging and error mapping, and replaces only how a request is decided:
// every admitted policy must allow the operation's complete permission.
//
// Services migrate to the Client one at a time. A service that has not
// migrated keeps calling the Engine, which stays authoritative for it.
type Client struct {
	engine    *Engine
	registry  *schema.Registry
	evaluator *RegistryEvaluator
}

// NewClient builds a client over engine and a compiled registry. It fails
// when the registry does not declare every operation the typed constructors
// build, so a missing declaration stops startup instead of erroring per
// request.
func NewClient(engine *Engine, registry *schema.Registry) (*Client, error) {
	if engine == nil {
		return nil, errors.New("authorization client requires an engine")
	}
	evaluator, err := NewRegistryEvaluator(registry)
	if err != nil {
		return nil, fmt.Errorf("index authorization registry: %w", err)
	}
	client := &Client{engine: engine, registry: registry, evaluator: evaluator}
	for _, op := range typedOperations() {
		if err := client.checkDeclared(op); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// NewDefaultClient builds a client over the default registry.
func NewDefaultClient(engine *Engine) (*Client, error) {
	registry, err := DefaultRegistry()
	if err != nil {
		return nil, err
	}
	return NewClient(engine, registry)
}

func (c *Client) checkDeclared(op Operation) error {
	if !op.alternatives {
		if _, ok := c.registry.Permission(op.permission); !ok {
			return fmt.Errorf("%w: operation %s is not declared", ErrUnknownPermission, op)
		}
		return nil
	}
	set, ok := c.registry.Alternatives(op.permission)
	if !ok {
		return fmt.Errorf("%w: alternatives %s are not declared", ErrUnknownPermission, op)
	}
	if len(set.Options) != len(op.options) {
		return fmt.Errorf("alternatives %s declare %d options, the operation builds %d", op, len(set.Options), len(op.options))
	}
	for i, option := range op.options {
		if option.permission != set.Options[i] {
			return fmt.Errorf("alternatives %s option %d is %s.%s, the operation builds %s", op, i, set.Options[i].Type, set.Options[i].Permission, option)
		}
		if err := c.checkDeclared(option); err != nil {
			return err
		}
	}
	return nil
}

// PrepareContext admits the caller and loads its policies, exactly as
// Engine.PrepareContext does.
func (c *Client) PrepareContext(ctx context.Context) (context.Context, error) {
	return c.engine.PrepareContext(ctx)
}

// RefreshContext reloads authority for a new operation in a long-lived
// request. A captured admission boundary keeps limiting the refreshed
// policies.
func (c *Client) RefreshContext(ctx context.Context) (context.Context, error) {
	return c.engine.RefreshContext(ctx)
}

// CaptureAdmissionBoundary captures the prepared policies for a job, so later
// refreshes can narrow its authority but never widen it.
func (c *Client) CaptureAdmissionBoundary(ctx context.Context) (AdmissionBoundary, error) {
	return c.engine.CaptureAdmissionBoundary(ctx)
}

// Require enforces every operation, recording one challenge as Engine.Require
// does. A denial maps to forbidden; an invalid operation or missing prepared
// policies map to unexpected.
func (c *Client) Require(ctx context.Context, ops ...Operation) error {
	enforce, err := c.engine.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		if !slices.ContainsFunc(ops, c.deniedUnenforced) {
			return nil
		}
		return c.engine.mapError(ctx, Denied(ScopeMCPAuth, nil))
	}
	if len(ops) == 0 {
		return c.engine.mapError(ctx, ErrNoChecks)
	}
	set, ok := c.policySet(ctx)
	if !ok {
		return c.engine.mapError(ctx, ErrMissingGrants)
	}

	checks := c.legacyChecksFor(ops)
	matches := make([]grantMatch, 0, len(ops))
	for i, op := range ops {
		decision, err := c.decide(set, op)
		if err != nil {
			c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck, set, checks, &checks[i], nil)
			return c.engine.mapError(ctx, err)
		}
		if !decision.Allowed {
			c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeDeny, denialReason(decision, set), set, checks, &checks[i], nil)
			return c.engine.mapError(ctx, Denied(checks[i].Scope, checks[i].selector()))
		}
		if decision.Grant != nil {
			matches = append(matches, grantMatch{Grant: *decision.Grant, ViaCheck: checks[i]})
		}
	}
	c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeAllow, authzrepo.ReasonGrantMatched, set, checks, &checks[0], matches)
	return nil
}

// Evaluate reports whether every operation is allowed without recording a
// challenge. Use it where a negative answer is a routine branch rather than a
// denial. It errors only when an operation cannot be decided.
func (c *Client) Evaluate(ctx context.Context, ops ...Operation) (bool, error) {
	enforce, err := c.engine.ShouldEnforce(ctx)
	if err != nil {
		return false, err
	}
	if !enforce {
		return !slices.ContainsFunc(ops, c.deniedUnenforced), nil
	}
	if len(ops) == 0 {
		return false, c.engine.mapError(ctx, ErrNoChecks)
	}
	set, ok := c.policySet(ctx)
	if !ok {
		return false, c.engine.mapError(ctx, ErrMissingGrants)
	}
	for _, op := range ops {
		decision, err := c.decide(set, op)
		if err != nil {
			return false, c.engine.mapError(ctx, err)
		}
		if !decision.Allowed {
			return false, nil
		}
	}
	return true, nil
}

// MatchMany decides each operation independently and returns the results in
// input order. Repeated resource IDs are separate inputs, so operations that
// differ only in dimensions, such as two tools on one server, keep their own
// answers. One challenge is recorded for the batch.
func (c *Client) MatchMany(ctx context.Context, ops []Operation) ([]bool, error) {
	enforce, err := c.engine.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	matched := make([]bool, len(ops))
	if !enforce {
		for i, op := range ops {
			matched[i] = !c.deniedUnenforced(op)
		}
		return matched, nil
	}
	set, ok := c.policySet(ctx)
	if !ok {
		return nil, c.engine.mapError(ctx, ErrMissingGrants)
	}

	checks := c.legacyChecksFor(ops)
	matches := make([]grantMatch, 0, len(ops))
	allowed := 0
	anyExcluded := false
	for i, op := range ops {
		decision, err := c.decide(set, op)
		if err != nil {
			c.logBatch(ctx, authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck, set, checks, &checks[i], nil, 0)
			return nil, c.engine.mapError(ctx, err)
		}
		if decision.Reason == PermissionReasonExcluded {
			anyExcluded = true
		}
		if decision.Allowed {
			matched[i] = true
			allowed++
			if decision.Grant != nil {
				matches = append(matches, grantMatch{Grant: *decision.Grant, ViaCheck: checks[i]})
			}
		}
	}
	if len(ops) > 0 {
		outcome, reason := authzrepo.OutcomeDeny, authzrepo.ReasonScopeUnsatisfied
		switch {
		case allowed > 0:
			outcome, reason = authzrepo.OutcomeAllow, authzrepo.ReasonGrantMatched
		case anyExcluded:
			reason = authzrepo.ReasonDenyGrant
		case set.grantCount() == 0:
			reason = authzrepo.ReasonNoGrants
		}
		c.logBatch(ctx, outcome, reason, set, checks, nil, matches, allowed)
	}
	return matched, nil
}

// deniedUnenforced reports whether op is refused where RBAC is not enforced.
// Skipping evaluation never grants auth-client use: a single operation on an
// auth client is refused, and alternatives are refused only when every
// option is.
func (c *Client) deniedUnenforced(op Operation) bool {
	if !op.alternatives {
		return c.registryKind(op.permission.Type) == ResourceKindMCPAuthClient
	}
	return !slices.ContainsFunc(op.options, func(option Operation) bool { return !c.deniedUnenforced(option) })
}

func (c *Client) registryKind(typeName string) string {
	t, ok := c.registry.Type(typeName)
	if !ok {
		return ""
	}
	return t.ResourceKind
}

// decide answers one operation in every policy of set.
func (c *Client) decide(set PolicySet, op Operation) (PolicyDecision, error) {
	if op.permission.Type == "" {
		return PolicyDecision{}, fmt.Errorf("%w: empty operation", ErrInvalidCheck)
	}
	if len(set.Policies) == 0 {
		// An admission that produced no policies denies, as the engine does;
		// a missing admission never reaches here.
		return PolicyDecision{Allowed: false, Reason: PermissionReasonNoGrant, Policy: "", Option: -1, Grant: nil}, nil
	}
	if !op.alternatives {
		return c.evaluator.Decide(set, op.request())
	}
	requests := make([]PermissionRequest, len(op.options))
	for i, option := range op.options {
		requests[i] = option.request()
	}
	return c.evaluator.DecideAlternatives(set, op.permission, requests)
}

func denialReason(decision PolicyDecision, set PolicySet) authzrepo.Reason {
	switch {
	case decision.Reason == PermissionReasonExcluded:
		return authzrepo.ReasonDenyGrant
	case set.grantCount() == 0:
		return authzrepo.ReasonNoGrants
	default:
		return authzrepo.ReasonScopeUnsatisfied
	}
}

// policySet builds the composed policies of a prepared request: the live
// policies, a captured admission boundary, and the represented user's own
// policies when the actor acts for someone else.
func (c *Client) policySet(ctx context.Context) (PolicySet, bool) {
	current, ok := currentGrantAuthorizationFromContext(ctx)
	if !ok {
		return PolicySet{Actor: urn.Principal{Type: "", ID: ""}, Policies: nil, Captured: nil, RepresentedUser: "", Represented: nil}, false
	}
	set := PolicySet{
		Actor:           requestActor(ctx),
		Policies:        labelPolicies("live", current.policies),
		Captured:        nil,
		RepresentedUser: current.representedUser,
		Represented:     labelPolicies("represented", current.representedPolicies),
	}
	if boundary, bounded := ctx.Value(admissionBoundaryKey{}).(grantAuthorization); bounded {
		set.Captured = labelPolicies("captured", boundary.policies)
		set.Represented = append(set.Represented, labelPolicies("captured_represented", boundary.representedPolicies)...)
	}
	return set, true
}

func labelPolicies(label string, grants [][]Grant) []Policy {
	if len(grants) == 0 {
		return nil
	}
	policies := make([]Policy, len(grants))
	for i, policy := range grants {
		policies[i] = Policy{Principal: fmt.Sprintf("%s[%d]", label, i), Grants: policy}
	}
	return policies
}

// requestActor is the principal making the request. Admission records it for
// sessions, credentials and workloads. Callers without one, such as
// assistant tokens and embedded chat sessions, act as an unattributed system
// principal: their attribution never makes them a user, so ownership facts
// never match them.
func requestActor(ctx context.Context) urn.Principal {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		return actor
	}
	if user := loadedGrantsUser(ctx); user != "" {
		return urn.NewPrincipal(urn.PrincipalTypeUser, user)
	}
	return urn.NewPrincipal(urn.PrincipalTypeSystem, "unattributed")
}

func (set PolicySet) grantCount() int {
	total := 0
	for _, policy := range set.Policies {
		total += len(policy.Grants)
	}
	return total
}

func (c *Client) log(ctx context.Context, operation authzrepo.Operation, outcome authzrepo.Outcome, reason authzrepo.Reason, set PolicySet, checks []Check, focus *Check, matches []grantMatch) {
	challengeLogger{
		Operation:            operation,
		Outcome:              outcome,
		Reason:               reason,
		Checks:               checks,
		Focus:                focus,
		Matches:              matches,
		EvaluatedGrantCount:  uint32(set.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, c.engine.db, c.engine.logger, c.engine.challengeLoggingEnabled)
}

func (c *Client) logBatch(ctx context.Context, outcome authzrepo.Outcome, reason authzrepo.Reason, set PolicySet, checks []Check, focus *Check, matches []grantMatch, allowed int) {
	challengeLogger{
		Operation:            authzrepo.OperationFilter,
		Outcome:              outcome,
		Reason:               reason,
		Checks:               checks,
		Focus:                focus,
		Matches:              matches,
		EvaluatedGrantCount:  uint32(set.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: uint32(len(checks)),      //nolint:gosec // candidate count is small
		FilterAllowedCount:   uint32(allowed),          //nolint:gosec // allowed count is small
	}.Log(ctx, c.engine.db, c.engine.logger, c.engine.challengeLoggingEnabled)
}

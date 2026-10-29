package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// The hooks in this file let an evaluator outside the engine, such as the
// declarative package, decide checks from the same admitted policies and
// report them through the same challenge log and response errors.

// MCPServerSchemaActivation reports whether organizationID evaluates MCP
// server permissions through the declarative schema rather than the legacy
// scope checks. Organizations keep the legacy rules until their migration
// takes effect.
type MCPServerSchemaActivation func(ctx context.Context, organizationID string) (bool, error)

// PolicySet is the caller's authorization as an outside evaluator reads it.
type PolicySet struct {
	// Enforced is false where RBAC does not apply to the caller, who is then
	// allowed exactly as Require allows a non-auth-client check.
	Enforced bool
	// OrganizationID is the organization every policy was loaded or admitted
	// for. Empty when the binding is unknown or contradicts the request.
	OrganizationID string
	// Policies are the grant sets the caller acts under. Every one must allow.
	Policies [][]Grant
}

// PolicySet returns the caller's admitted policies.
func (e *Engine) PolicySet(ctx context.Context) (PolicySet, error) {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return PolicySet{Enforced: false, OrganizationID: "", Policies: nil}, err
	}
	if !enforce {
		return PolicySet{Enforced: false, OrganizationID: "", Policies: nil}, nil
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return PolicySet{Enforced: true, OrganizationID: "", Policies: nil}, ErrMissingGrants
	}
	return PolicySet{Enforced: true, OrganizationID: authorization.organizationID, Policies: authorization.policies}, nil
}

// MCPServerSchemaActive reports whether the caller's organization evaluates
// MCP servers through the declarative schema. Activation follows the
// authenticated organization; whether the loaded policies belong to it is
// for the evaluation to decide.
func (e *Engine) MCPServerSchemaActive(ctx context.Context) (bool, error) {
	if e.mcpServerSchemaActive == nil {
		return false, nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return false, nil
	}
	active, err := e.mcpServerSchemaActive(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return false, fmt.Errorf("check mcp server schema activation: %w", err)
	}
	return active, nil
}

// IsAPIKeyGrant reports whether grant belongs to a legacy API key, whose
// grants match strictly.
func IsAPIKeyGrant(grant *Grant) bool { return isAPIKeyGrant(grant) }

// ExternalDecision is a decision an outside evaluator made for the legacy
// check it stands in for.
type ExternalDecision struct {
	Allowed bool
	// DenyReason labels a denial in the challenge log.
	DenyReason authzrepo.Reason
	// Trace is the evaluator's request-to-decision trace, logged at debug.
	Trace string
	// Err is set when no decision could be made. It fails the request.
	Err error
}

// RecordDecision logs decision in the challenge log under check and returns
// the response error Require would return for it.
func (e *Engine) RecordDecision(ctx context.Context, check Check, decision ExternalDecision) error {
	if decision.Trace != "" {
		e.logger.DebugContext(ctx, "external authz decision", attr.SlogResourceID(check.ResourceID), attr.SlogReason(decision.Trace))
	}
	outcome, reason := authzrepo.OutcomeAllow, authzrepo.ReasonGrantMatched
	switch {
	case decision.Err != nil:
		outcome, reason = authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck
	case !decision.Allowed:
		outcome, reason = authzrepo.OutcomeDeny, decision.DenyReason
	}
	challengeLogger{
		Operation:            authzrepo.OperationRequire,
		Outcome:              outcome,
		Reason:               reason,
		Checks:               []Check{check},
		Focus:                &check,
		Matches:              nil,
		EvaluatedGrantCount:  0,
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
	switch {
	case decision.Err != nil:
		return e.mapError(ctx, decision.Err)
	case !decision.Allowed:
		return e.mapError(ctx, Denied(check.Scope, check.selector()))
	default:
		return nil
	}
}

// MapError maps an evaluation error to the response error Require returns.
func (e *Engine) MapError(ctx context.Context, err error) error { return e.mapError(ctx, err) }

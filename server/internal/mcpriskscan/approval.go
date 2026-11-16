package mcpriskscan

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

// ApprovalGate holds MCP tool calls that a require_approval policy covers.
type ApprovalGate interface {
	// HoldCall lets the call through when an approval covers it. Otherwise it
	// records or refreshes the call's approval request and returns its token
	// and the instructions the agent needs.
	HoldCall(ctx context.Context, call approval.HoldInput) (approval.HoldOutcome, error)
}

var _ ApprovalGate = (*approval.Service)(nil)

// WithApprovalGate enables require_approval enforcement. Without a gate, a
// require_approval policy denies the call.
func WithApprovalGate(gate ApprovalGate) PolicyEvaluatorOption {
	return func(evaluator *policyEvaluator) {
		evaluator.approvals = gate
	}
}

// holdForApproval lets the call through only when every require_approval
// policy covering it has an approval for it. Failures deny: an approval gate
// that cannot record a request must not let the call through.
func (p *policyEvaluator) holdForApproval(ctx context.Context, subject Subject, policies []policycore.Policy) Decision {
	event := subject.Event
	if p.approvals == nil {
		return approvalUnavailableDecision(policies[0], "approvals are not available on this server")
	}
	requesterURN, requesterUserID, ok := approval.Requester(event.Principal())
	if !ok {
		return approvalUnavailableDecision(policies[0], "this credential cannot request approval; call with an agent key or sign in")
	}
	projectID, err := uuid.Parse(event.ProjectID)
	if err != nil {
		return approvalUnavailableDecision(policies[0], "the project could not be resolved")
	}
	outcome, err := p.approvals.HoldCall(ctx, approval.HoldInput{
		Policies:        policies,
		OrganizationID:  event.OrganizationID,
		ProjectID:       projectID,
		RequesterURN:    requesterURN,
		RequesterUserID: requesterUserID,
		Surface:         event.Surface,
		ToolsetID:       event.ToolsetID,
		ServerID:        event.ServerID,
		ToolName:        event.ToolName,
		ChatID:          event.ChatID,
		Arguments:       subject.Payload.Bytes(),
	})
	if err != nil {
		p.logger.ErrorContext(ctx, "hold MCP call for approval", attr.SlogError(err), attr.SlogRiskPolicyID(policies[0].ID.String()))
		return approvalUnavailableDecision(policies[0], "the approval request could not be recorded; try again")
	}
	if outcome.Allowed {
		return Allow()
	}
	holding := policies[0]
	if outcome.Policy != nil {
		holding = *outcome.Policy
	}
	return Decision{
		Disposition:   DispositionDeny,
		PolicyID:      holding.ID.String(),
		PolicyName:    holding.Name,
		RuleID:        "",
		Description:   "held for approval",
		UserMessage:   outcome.UserMessage,
		Indeterminate: false,
		ApprovalToken: outcome.Token,
	}
}

func approvalUnavailableDecision(policy policycore.Policy, reason string) Decision {
	return Decision{
		Disposition:   DispositionDeny,
		PolicyID:      policy.ID.String(),
		PolicyName:    policy.Name,
		RuleID:        "",
		Description:   "approval required but unavailable",
		UserMessage:   fmt.Sprintf("This MCP request needs approval under risk policy %q, but %s.", policy.Name, reason),
		Indeterminate: false,
		ApprovalToken: "",
	}
}

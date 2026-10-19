package mcpriskscan

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ApprovalGate holds MCP tool calls that a require_approval policy covers.
type ApprovalGate interface {
	// HoldCall lets the call through when an approval covers it. Otherwise it
	// records or refreshes the call's approval request and returns its token
	// and the instructions the agent needs.
	HoldCall(ctx context.Context, call ApprovalCall) (ApprovalOutcome, error)
}

// ApprovalCall is one MCP tool call a require_approval policy covers.
type ApprovalCall struct {
	// Policy is the require_approval policy that covers the call.
	Policy policycore.Policy

	// OrganizationID is the organization the call belongs to.
	OrganizationID string

	// ProjectID is the project the call belongs to.
	ProjectID uuid.UUID

	// RequesterURN is the principal that made the call: an agent or a user.
	RequesterURN string

	// RequesterUserID is set when a person called through their own session.
	RequesterUserID string

	// Surface is the MCP surface that received the call.
	Surface string

	// ToolsetID is the hosted toolset that served the call, when known.
	ToolsetID string

	// ServerID is the MCP server the call targets, when known.
	ServerID string

	// ToolName is the called tool.
	ToolName string

	// ChatID is the chat the call belongs to, when known.
	ChatID string

	// Arguments is the raw JSON arguments object of the call.
	Arguments []byte
}

// ApprovalOutcome is the result of holding one call.
type ApprovalOutcome struct {
	// Allowed reports that an approval covered the call and was used.
	Allowed bool

	// Token is the request token the agent passes to request_approval and
	// watches with. Empty when Allowed.
	Token string

	// UserMessage tells the agent how to request approval and resume.
	UserMessage string
}

// WithApprovalGate enables require_approval enforcement. Without a gate, a
// require_approval policy denies the call.
func WithApprovalGate(gate ApprovalGate) PolicyEvaluatorOption {
	return func(evaluator *policyEvaluator) {
		evaluator.approvals = gate
	}
}

// holdForApproval checks each require_approval policy in turn. A policy whose
// approval covers the call lets it pass to the next; the first policy without
// one holds the call. Failures deny: an approval gate that cannot record a
// request must not let the call through.
func (p *policyEvaluator) holdForApproval(ctx context.Context, subject Subject, policies []policycore.Policy) Decision {
	event := subject.Event
	if p.approvals == nil {
		return approvalUnavailableDecision(policies[0], "approvals are not available on this server")
	}
	requesterURN, requesterUserID, ok := ApprovalRequester(event.Principal())
	if !ok {
		return approvalUnavailableDecision(policies[0], "this credential cannot request approval; call with an agent key or sign in")
	}
	projectID, err := uuid.Parse(event.ProjectID)
	if err != nil {
		return approvalUnavailableDecision(policies[0], "the project could not be resolved")
	}
	for _, policy := range policies {
		outcome, err := p.approvals.HoldCall(ctx, ApprovalCall{
			Policy:          policy,
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
			p.logger.ErrorContext(ctx, "hold MCP call for approval", attr.SlogError(err), attr.SlogRiskPolicyID(policy.ID.String()))
			return approvalUnavailableDecision(policy, "the approval request could not be recorded; try again")
		}
		if outcome.Allowed {
			continue
		}
		return Decision{
			Disposition:   DispositionDeny,
			PolicyID:      policy.ID.String(),
			PolicyName:    policy.Name,
			RuleID:        "",
			Description:   "held for approval",
			UserMessage:   outcome.UserMessage,
			Indeterminate: false,
			ApprovalToken: outcome.Token,
		}
	}
	return Allow()
}

// ApprovalRequester maps validated provenance to the principal an approval
// request binds to. Only agents and people signed in through their own session
// can request approval.
func ApprovalRequester(identity mcpidentity.Identity) (requesterURN, requesterUserID string, ok bool) {
	switch identity.Kind() {
	case mcpidentity.KindAgent:
		if identity.AgentID() == "" {
			return "", "", false
		}
		return urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID()).String(), "", true
	case mcpidentity.KindUserSession:
		if identity.UserID() == "" {
			return "", "", false
		}
		return urn.NewPrincipal(urn.PrincipalTypeUser, identity.UserID()).String(), identity.UserID(), true
	default:
		return "", "", false
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

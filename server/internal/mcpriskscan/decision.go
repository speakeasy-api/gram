package mcpriskscan

// Disposition is the request-path result of an MCP risk scan.
type Disposition string

const (
	DispositionAllow Disposition = "allow"
	DispositionDeny  Disposition = "deny"
)

const decisionIndeterminate = "indeterminate"

// Decision is the complete request-path policy result. Indeterminate records
// that fail mode resolved an incomplete evaluation to the final disposition.
type Decision struct {
	Disposition   Disposition
	PolicyID      string
	PolicyName    string
	RuleID        string
	Description   string
	UserMessage   string
	Indeterminate bool

	// ApprovalToken is set when a require_approval policy holds the call. The
	// deny is then an instruction to the agent, not a refusal: seams that can
	// return a tool result return UserMessage as an isError result so the agent
	// requests approval and retries.
	ApprovalToken string
}

// HeldForApproval reports whether the call is denied pending approval.
func (d Decision) HeldForApproval() bool {
	return d.Denied() && d.ApprovalToken != ""
}

// Allow returns a clean allow decision.
func Allow() Decision {
	return Decision{
		Disposition:   DispositionAllow,
		PolicyID:      "",
		PolicyName:    "",
		RuleID:        "",
		Description:   "",
		UserMessage:   "",
		Indeterminate: false,
		ApprovalToken: "",
	}
}

// Denied reports whether the caller must stop before execution.
func (d Decision) Denied() bool {
	return d.Disposition == DispositionDeny
}

func (d Decision) metricDecision() string {
	if d.Indeterminate {
		return decisionIndeterminate
	}
	return string(d.Disposition)
}

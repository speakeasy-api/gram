package risk_analysis

// Finding represents a single secret or sensitive data match found in a message.
type Finding struct {
	RuleID           string
	Description      string
	Match            string
	StartPos         int
	EndPos           int
	Tags             []string
	Source           string
	Confidence       float64
	DeadLetterReason string

	mcpLookupToolCallID string
	spanGroupKey        string
	field               string
	path                string
}

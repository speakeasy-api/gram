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
	Stage            string
	Confidence       float64
	DeadLetterReason string

	mcpLookupToolCallID string
	spanGroupKey        string
	field               string
	path                string
}

// Detection stages, recorded as the `stage` attribute on risk.rule.confidence so
// findings that share a rule_id (notably prompt_injection, which both layers
// emit) can be split by the layer that produced them.
const (
	// StageHeuristic marks findings from deterministic, always-on in-process
	// detectors (regex/keyword/pattern/entity matchers): the L0 layer.
	StageHeuristic = "heuristic"
	// StageJudge marks findings from an LLM judge: the opt-in L1 layer.
	StageJudge = "judge"
)

// Package intentpolicy assesses recorded tool calls against a fixed intent
// guardrail. An evaluator infers labels about one call, then CEL contracts over
// those labels decide permit, clarify, or block. Assessments are pure
// functions of (action, guardrail version, evaluator descriptor): no state
// carries between calls and nothing is enforced or persisted.
package intentpolicy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ToolRef identifies the invoked tool.
type ToolRef struct {
	// Name is the raw tool name as recorded.
	Name string

	// MCPServer is the MCP server parsed from Name; empty for native tools.
	MCPServer string

	// MCPFunction is the MCP function parsed from Name; empty for native tools.
	MCPFunction string
}

// Action is one recorded tool call.
type Action struct {
	// ChatID is the session the call belongs to.
	ChatID uuid.UUID

	// MessageID is the assistant message that carried the call.
	MessageID uuid.UUID

	// Seq is the carrying message's sequence number.
	Seq int64

	// Generation is the carrying message's chat generation.
	Generation int32

	// ToolCallID is the provider-assigned call id.
	ToolCallID string

	// Tool identifies the invoked tool.
	Tool ToolRef

	// Arguments is the argument payload exactly as recorded.
	Arguments json.RawMessage

	// Cwd is the session working directory, for resolving relative paths.
	Cwd string

	// OccurredAt is when the carrying message was recorded.
	OccurredAt time.Time
}

// Kind is the shape of a label question.
type Kind string

const (
	// KindNoul is a yes/no question answered with a probability.
	KindNoul Kind = "noul"

	// KindMultiNoul is a set of independent yes/no members exposed as a list.
	KindMultiNoul Kind = "multi_noul"

	// KindChoice selects one option, optionally ordinal.
	KindChoice Kind = "choice"
)

// Option is one value of a choice or multi-noul question.
type Option struct {
	// Key is the value exposed to contracts.
	Key string

	// Description grounds the value for the evaluator.
	Description string

	// Ordinal orders the option for rank; higher is better understood.
	Ordinal int

	// Uncertain marks values whose rank is reported as -1.
	Uncertain bool
}

// LabelQuestion is one label the evaluator answers about a call.
type LabelQuestion struct {
	// Key is the label name exposed to contracts as label.<key>.
	Key string

	// Kind is the question shape.
	Kind Kind

	// Instructions states the question for the evaluator.
	Instructions string

	// Options lists the values of a choice or multi-noul question.
	Options []Option
}

// Contract is one CEL rule over labels and tool metadata.
type Contract struct {
	// ID is a stable identifier.
	ID string

	// Name is a short display name.
	Name string

	// Expression evaluates to PERMIT, CLARIFY, BLOCK, or ABSTAIN.
	Expression string
}

// Guardrail is the versioned set of statements, questions, and contracts.
type Guardrail struct {
	// ID identifies the guardrail.
	ID string

	// Version changes whenever statements, questions, or contracts change.
	Version string

	// Statements ground the evaluator.
	Statements []string

	// Questions are the labels inferred for every call.
	Questions []LabelQuestion

	// Contracts decide over the labels.
	Contracts []Contract
}

// Descriptor identifies the evaluator that produced labels.
type Descriptor struct {
	// Evaluator is the evaluator kind (jev or gemini).
	Evaluator string

	// Provider is the inference transport.
	Provider string

	// Model is the configured model id.
	Model string

	// PromptVersion identifies the question-to-prompt compilation rules.
	PromptVersion string
}

// LabelInput is everything an evaluator may see for one call.
type LabelInput struct {
	// Action is the call under evaluation.
	Action Action

	// Grounding is the guardrail statements.
	Grounding []string

	// Questions are the labels to infer.
	Questions []LabelQuestion
}

// LabelValue is one inferred label.
type LabelValue struct {
	// Value is a bool, string, or []string depending on the question kind.
	Value any

	// Rank is the ordinal of a choice value; -1 when categorical or uncertain.
	Rank int

	// Probability is the evaluator's probability for the selected value.
	Probability float64

	// Distribution holds per-option probabilities for choice and multi-noul labels.
	Distribution map[string]float64

	// Rationale is the evaluator's explanation, when it gives one.
	Rationale string
}

// LabelSet maps label keys to values. A missing key means the evaluator failed
// to answer that label.
type LabelSet map[string]LabelValue

// Usage is the cost of one evaluator call.
type Usage struct {
	// InputTokens is the provider-reported prompt token count.
	InputTokens int64

	// OutputTokens is the provider-reported completion token count.
	OutputTokens int64

	// CostUSD is the provider-reported cost; zero when unreported.
	CostUSD float64

	// LatencyMs is the wall-clock duration of the evaluator call.
	LatencyMs int64
}

// Evaluator infers labels for one call.
type Evaluator interface {
	// Label answers the questions for one call. A partial LabelSet may be
	// returned alongside an error.
	Label(ctx context.Context, in LabelInput) (LabelSet, Usage, error)

	// Descriptor identifies the provider, model, and prompt version.
	Descriptor() Descriptor
}

// Decision is the outcome of contract evaluation.
type Decision string

const (
	// DecisionPermit allows the call.
	DecisionPermit Decision = "permit"

	// DecisionClarify asks a person before the call proceeds.
	DecisionClarify Decision = "clarify"

	// DecisionBlock stops the call.
	DecisionBlock Decision = "block"

	// decisionAbstain is a contract result that does not participate.
	decisionAbstain Decision = "abstain"
)

// ContractOutcome is one contract's result for one call.
type ContractOutcome struct {
	// ContractID identifies the contract.
	ContractID string

	// Result is the contract's decision, or empty when it errored.
	Result Decision

	// Error is the evaluation error, when any.
	Error string

	// Inputs maps each label or tool path the contract reads to its value.
	Inputs map[string]any
}

// Assessment is the full result for one call.
type Assessment struct {
	// Action is the assessed call.
	Action Action

	// Labels is what the evaluator inferred.
	Labels LabelSet

	// Decision is the combined contract decision.
	Decision Decision

	// DecidedBy lists the contracts that produced Decision.
	DecidedBy []string

	// Outcomes holds one entry per contract, in guardrail order.
	Outcomes []ContractOutcome

	// EvaluatorError is set when the evaluator failed to answer every label.
	EvaluatorError string

	// Usage is the evaluator cost for this call.
	Usage Usage

	// Descriptor identifies the evaluator.
	Descriptor Descriptor
}

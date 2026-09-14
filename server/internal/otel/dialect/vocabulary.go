package dialect

// The agent vocabulary: what agent_events.event_type and outcome hold. A
// dialect maps each producer's own event names onto these; an empty type
// means no dialect recognised the record, and the row still lands with its
// raw name and payload intact so it stays countable and reclassifiable.
const (
	EventTypeUnclassified   = ""
	EventTypePrompt         = "prompt"
	EventTypeAPIRequest     = "api_request"
	EventTypeAPIResponse    = "api_response"
	EventTypeAPIError       = "api_error"
	EventTypeAPIRefusal     = "api_refusal"
	EventTypeToolCall       = "tool_call"
	EventTypeToolCallResult = "tool_call_result"
	EventTypeToolDecision   = "tool_decision"
)

// Outcomes, in agent vocabulary rather than as a protocol status code.
// Empty means the producer did not say.
const (
	OutcomeUnknown  = ""
	OutcomeOK       = "ok"
	OutcomeError    = "error"
	OutcomeRejected = "rejected"
	OutcomeRefused  = "refused"
)

// scopeNameKey is the key a producer dialect reports for answers that come
// from recognising the producer rather than from an attribute.
const scopeNameKey = "scope.name"

// tokensDisjoint clamps a producer-reported cached count into [0, input] and
// returns the disjoint (input excluding cache reads, cache reads) pair, so bad
// client data can never increase usage.
func tokensDisjoint(inputInclusive, cached int64) (input, cacheRead int64) {
	inputInclusive = max(inputInclusive, 0)
	cacheRead = min(max(cached, 0), inputInclusive)
	return inputInclusive - cacheRead, cacheRead
}

package enrich

import (
	"context"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/agentsurface"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// Coverage counts what a producer should have stated and did not, so a
// renamed attribute shows up as one key going quiet on one event type for
// one surface. The expectations are OpenTelemetry's attribute requirement
// levels (https://opentelemetry.io/docs/specs/semconv/general/attribute-requirement-level/):
// an expectation is Required, one with a condition is Conditionally
// Required, and a Recommended or Opt-In attribute has none and is never
// counted.

// expectation is one attribute the listed event types must carry, always or
// only when the condition over what was written beside it holds.
type expectation struct {
	key  attribute.Key
	on   []string
	when func(written map[attribute.Key]attribute.Value) bool
}

func present(key attribute.Key) func(map[attribute.Key]attribute.Value) bool {
	return func(written map[attribute.Key]attribute.Value) bool {
		_, ok := written[key]
		return ok
	}
}

func equals(key attribute.Key, value string) func(map[attribute.Key]attribute.Value) bool {
	return func(written map[attribute.Key]attribute.Value) bool {
		v, ok := written[key]
		return ok && v.AsString() == value
	}
}

// An api_request_body and a compaction have no subject of their own.
var typesWithASubject = []string{
	dialect.EventTypePrompt,
	dialect.EventTypeAPIRequest,
	dialect.EventTypeAPIResponse,
	dialect.EventTypeAPIError,
	dialect.EventTypeAPIRefusal,
	dialect.EventTypeToolCall,
	dialect.EventTypeToolCallResult,
	dialect.EventTypeToolDecision,
	dialect.EventTypeAPIResponseBody,
}

var toolEventTypes = []string{
	dialect.EventTypeToolCall,
	dialect.EventTypeToolCallResult,
	dialect.EventTypeToolDecision,
}

var identityExpectations = []expectation{
	{key: SessionIDColumnKey, on: classifiedEventTypes, when: nil},
	{key: TurnIDColumnKey, on: classifiedEventTypes, when: nil},
	{key: EventIDColumnKey, on: typesWithASubject, when: nil},
	{key: UserEmailColumnKey, on: classifiedEventTypes, when: nil},
	{key: ExternalUserIDColumnKey, on: classifiedEventTypes, when: nil},
	{key: ExternalOrgIDColumnKey, on: classifiedEventTypes, when: nil},
}

// Skill, agent, text and a decision's outcome are Recommended or Opt-In.
// The MCP server and tool are a pair: each half is required once the other
// is stated.
var operationExpectations = []expectation{
	{key: ModelColumnKey, on: []string{dialect.EventTypeAPIRequest, dialect.EventTypeAPIResponse, dialect.EventTypeAPIError, dialect.EventTypeAPIRefusal}, when: nil},
	{key: QuerySourceColumnKey, on: []string{dialect.EventTypeAPIRequest}, when: nil},
	{key: MCPServerNameColumnKey, on: toolEventTypes, when: present(MCPToolNameColumnKey)},
	{key: MCPToolNameColumnKey, on: toolEventTypes, when: present(MCPServerNameColumnKey)},
	{key: NameColumnKey, on: toolEventTypes, when: nil},
	{key: ToolNameColumnKey, on: toolEventTypes, when: nil},
	{key: OutcomeColumnKey, on: []string{dialect.EventTypeToolCallResult, dialect.EventTypeCompaction}, when: nil},
	{key: OutcomeMessageColumnKey, on: []string{dialect.EventTypeAPIError}, when: nil},
	{key: OutcomeMessageColumnKey, on: []string{dialect.EventTypeToolCallResult, dialect.EventTypeCompaction}, when: equals(OutcomeColumnKey, dialect.OutcomeError)},
	{key: DurationNanoColumnKey, on: []string{dialect.EventTypeAPIRequest, dialect.EventTypeToolCallResult}, when: nil},
}

var usageExpectations = []expectation{
	{key: InputTokensColumnKey, on: []string{dialect.EventTypeAPIRequest}, when: nil},
	{key: OutputTokensColumnKey, on: []string{dialect.EventTypeAPIRequest}, when: nil},
	{key: CacheReadTokensColumnKey, on: []string{dialect.EventTypeAPIRequest}, when: nil},
	{key: CacheWriteTokensColumnKey, on: []string{dialect.EventTypeAPIRequest}, when: nil},
	{key: CostUSDColumnKey, on: []string{dialect.EventTypeAPIRequest}, when: nil},
}

// countMissing counts every expectation on the event type that the enricher
// did not write. The surface label is read lazily, since only a count needs
// it.
func countMissing(ctx context.Context, in *Instruments, surface func() string, eventType string, expectations []expectation, written []attribute.KeyValue) {
	index := make(map[attribute.Key]attribute.Value, len(written))
	for _, kv := range written {
		index[kv.Key] = kv.Value
	}

	label := ""
	for _, e := range expectations {
		if !slices.Contains(e.on, eventType) {
			continue
		}
		if _, ok := index[e.key]; ok {
			continue
		}
		if e.when != nil && !e.when(index) {
			continue
		}
		if label == "" {
			label = surface()
		}
		in.recordColumnValueMissing(ctx, label, eventType, attributeName(e.key))
	}
}

// attributeName is the key without its namespace, which is the counter's
// label.
func attributeName(key attribute.Key) string {
	return strings.TrimPrefix(string(key), agentColumnKeyPrefix)
}

const missingLabelOther = string(agentsurface.SurfaceOther)

// missingLabel folds the surface the dialect reported into the agent surface
// vocabulary, or "other". Folding is what keeps the counter's label set
// bounded: a producer's free-form service.name is never a label.
func missingLabel(_, surface string, err error) string {
	if err != nil || surface == "" {
		return missingLabelOther
	}
	folded, ok := agentsurface.ForHookSource(surface, "")
	if !ok || folded == agentsurface.SurfaceUnknown {
		return missingLabelOther
	}
	return string(folded)
}

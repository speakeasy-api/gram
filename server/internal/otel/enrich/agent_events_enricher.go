package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// The agent_events enricher fills the agent_events columns in the transform,
// as canonical speakeasy.agent.<column> attributes beside the producer's own,
// and the agent_events writer copies them into the row. Classification runs
// first, since its event type is what every column table keys on. Which
// types carry which column, and at what requirement level, is the design doc
// "Populating agent_events, column by column" in Linear.

// LogColumns is every enricher that fills an agent_events column from a log
// record, in column order, classification first.
func LogColumns(in *Instruments) []LogEnricher {
	definitions := registry()
	out := make([]LogEnricher, 0, len(definitions)+1)
	out = append(out, &logClassification{})
	for _, definition := range definitions {
		out = append(out, definition.log(in))
	}
	return out
}

// SpanColumns is LogColumns for spans: the same columns from the same tables.
func SpanColumns(in *Instruments) []SpanEnricher {
	definitions := registry()
	out := make([]SpanEnricher, 0, len(definitions)+1)
	out = append(out, &spanClassification{})
	for _, definition := range definitions {
		out = append(out, definition.span(in))
	}
	return out
}

func registry() []columnDefinition {
	out := make([]columnDefinition, 0, 23)
	out = append(out, identityColumns()...)
	out = append(out, operationColumns()...)
	out = append(out, usageColumns()...)
	return out
}

// Classification writes what a record is: event_type, raw_event_name, source,
// provider and surface. Nothing is counted here; an unclassified record keeps
// its raw name and source and gets no type. The source is the resource's
// service.name, canonicalised. A provider the pipeline already attributed
// (gram.provider) wins over the dialect's.

const classificationEnricherName = "enrich-classification"

func classify(source, rawEventName, eventType, attributedProvider, dialectProvider, surface string) []attribute.KeyValue {
	out := []attribute.KeyValue{SourceColumnKey.String(source)}
	if rawEventName != "" {
		out = append(out, RawEventNameColumnKey.String(rawEventName))
	}
	if eventType != dialect.EventTypeUnclassified {
		out = append(out, EventTypeColumnKey.String(eventType))
	}
	provider := attributedProvider
	if provider == "" {
		provider = dialectProvider
	}
	if provider != "" {
		out = append(out, ProviderColumnKey.String(provider))
	}
	if surface != "" {
		out = append(out, SurfaceColumnKey.String(surface))
	}
	return out
}

type logClassification struct{}

func (*logClassification) Name() string {
	return classificationEnricherName
}

func (*logClassification) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	return classify(
		inboundLogSource(record),
		stated(d.EventName(record)),
		stated(d.EventType(record)),
		inboundLogAttributeString(record, string(attr.ProviderKey)),
		stated(d.Provider(record)),
		stated(d.Surface(record)),
	), nil
}

type spanClassification struct{}

func (*spanClassification) Name() string {
	return classificationEnricherName
}

func (*spanClassification) Enrich(_ context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	return classify(
		inboundSpanSource(span),
		stated(d.EventName(span)),
		stated(d.EventType(span)),
		inboundSpanAttributeString(span, string(attr.ProviderKey)),
		stated(d.Provider(span)),
		stated(d.Surface(span)),
	), nil
}

// Identity: who the event belongs to. Every classified type carries these,
// since a session, a person and an account are properties of the session
// rather than of one kind of event.
func identityColumns() []columnDefinition {
	return []columnDefinition{
		columnSessionID(),
		columnTurnID(),
		columnEventID(),
		columnUserEmail(),
		columnExternalUserID(),
		columnExternalOrgID(),
	}
}

func columnSessionID() columnDefinition {
	return column[string]{
		key:    SessionIDColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.SessionID, span: dialect.SpanDialect.SessionID}),
	}
}

// Codex states no turn, so every Codex record counts as missing here.
func columnTurnID() columnDefinition {
	return column[string]{
		key:    TurnIDColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.TurnID, span: dialect.SpanDialect.TurnID}),
	}
}

// An api_request_body and a compaction have no subject of their own, so the
// writer keeps the record id for them.
func columnEventID() columnDefinition {
	subject := getter[string]{log: dialect.LogDialect.SubjectID, span: dialect.SpanDialect.SubjectID}
	return column[string]{
		key: EventIDColumnKey,
		byType: perEventType[string]{
			dialect.EventTypePrompt:          subject,
			dialect.EventTypeAPIRequest:      subject,
			dialect.EventTypeAPIResponse:     subject,
			dialect.EventTypeAPIError:        subject,
			dialect.EventTypeAPIRefusal:      subject,
			dialect.EventTypeToolCall:        subject,
			dialect.EventTypeToolCallResult:  subject,
			dialect.EventTypeToolDecision:    subject,
			dialect.EventTypeAPIResponseBody: subject,
		},
	}
}

func columnUserEmail() columnDefinition {
	return column[string]{
		key:    UserEmailColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.ExternalUserEmail, span: dialect.SpanDialect.ExternalUserEmail}),
	}
}

func columnExternalUserID() columnDefinition {
	return column[string]{
		key:    ExternalUserIDColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.ExternalUserID, span: dialect.SpanDialect.ExternalUserID}),
	}
}

// Only Claude Code states an organization; other producers count as missing.
func columnExternalOrgID() columnDefinition {
	return column[string]{
		key:    ExternalOrgIDColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.ExternalOrgID, span: dialect.SpanDialect.ExternalOrgID}),
	}
}

// Operation: what happened and how it went.
func operationColumns() []columnDefinition {
	return []columnDefinition{
		columnModel(),
		columnQuerySource(),
		columnSkillName(),
		columnAgentName(),
		columnMCPServerName(),
		columnMCPToolName(),
		columnName(),
		columnToolName(),
		columnText(),
		columnOutcome(),
		columnOutcomeMessage(),
		columnDurationNano(),
	}
}

// A payload capture states the model only sometimes.
func columnModel() columnDefinition {
	model := getter[string]{log: dialect.LogDialect.Model, span: dialect.SpanDialect.Model}
	return column[string]{
		key: ModelColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:      model,
			dialect.EventTypeAPIResponse:     model,
			dialect.EventTypeAPIError:        model,
			dialect.EventTypeAPIRefusal:      model,
			dialect.EventTypeAPIRequestBody:  recommended(model),
			dialect.EventTypeAPIResponseBody: recommended(model),
		},
	}
}

// Only Claude Code's log events state where a request originated.
func columnQuerySource() columnDefinition {
	return column[string]{
		key: QuerySourceColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.QuerySource, span: dialect.SpanDialect.QuerySource},
		},
	}
}

// Most requests and tool events involve no skill. Deprecated in favour of
// name once a skill event type exists.
func columnSkillName() columnDefinition {
	skill := recommended(getter[string]{log: dialect.LogDialect.SkillName, span: dialect.SpanDialect.SkillName})
	return column[string]{
		key: SkillNameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:     skill,
			dialect.EventTypeToolCall:       skill,
			dialect.EventTypeToolCallResult: skill,
			dialect.EventTypeToolDecision:   skill,
		},
	}
}

// Most requests come from the main thread and most tool events start no
// sub-agent. Deprecated in favour of name once a sub-agent event type exists.
func columnAgentName() columnDefinition {
	agent := recommended(getter[string]{log: dialect.LogDialect.AgentName, span: dialect.SpanDialect.AgentName})
	return column[string]{
		key: AgentNameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:     agent,
			dialect.EventTypeToolCall:       agent,
			dialect.EventTypeToolCallResult: agent,
			dialect.EventTypeToolDecision:   agent,
		},
	}
}

// The MCP server and tool come as a pair on tool events: each half is
// required once the other is stated, and a built-in tool states neither.
func columnMCPServerName() columnDefinition {
	server := getter[string]{log: dialect.LogDialect.MCPServerName, span: dialect.SpanDialect.MCPServerName}
	tool := getter[string]{log: dialect.LogDialect.MCPToolName, span: dialect.SpanDialect.MCPToolName}
	onTool := conditionallyRequired(server, statedBy(tool))
	return column[string]{
		key: MCPServerNameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:     recommended(server),
			dialect.EventTypeToolCall:       onTool,
			dialect.EventTypeToolCallResult: onTool,
			dialect.EventTypeToolDecision:   onTool,
		},
	}
}

// mcp_tool_name stays filled until a contract migration drops it; name
// carries the tool whether or not it is an MCP tool.
func columnMCPToolName() columnDefinition {
	tool := getter[string]{log: dialect.LogDialect.MCPToolName, span: dialect.SpanDialect.MCPToolName}
	server := getter[string]{log: dialect.LogDialect.MCPServerName, span: dialect.SpanDialect.MCPServerName}
	onTool := conditionallyRequired(tool, statedBy(server))
	return column[string]{
		key: MCPToolNameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:     recommended(tool),
			dialect.EventTypeToolCall:       onTool,
			dialect.EventTypeToolCallResult: onTool,
			dialect.EventTypeToolDecision:   onTool,
		},
	}
}

// name is the subject's name, the generic pair to event_id: today the tool on
// tool events. A skill or sub-agent event type joins this table when one
// exists.
func columnName() columnDefinition {
	tool := getter[string]{log: dialect.LogDialect.ToolName, span: dialect.SpanDialect.ToolName}
	return column[string]{
		key: NameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeToolCall:       tool,
			dialect.EventTypeToolCallResult: tool,
			dialect.EventTypeToolDecision:   tool,
		},
	}
}

// tool_name is filled the same way as name until a contract migration drops
// it.
func columnToolName() columnDefinition {
	tool := getter[string]{log: dialect.LogDialect.ToolName, span: dialect.SpanDialect.ToolName}
	return column[string]{
		key: ToolNameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeToolCall:       tool,
			dialect.EventTypeToolCallResult: tool,
			dialect.EventTypeToolDecision:   tool,
		},
	}
}

// maxTextBytes bounds the canonical copy of a record's words, which sits
// beside the producer's own attribute: 64 KiB holds any prompt a person types
// and stays inside the headroom a relay export reserves for enrichments.
const maxTextBytes = 64 * constants.KiB

// text is Opt-In: producers log words only when the person agreed to it. A
// request and a tool call keep everything in attributes, so they are absent.
func columnText() columnDefinition {
	text := optIn(getter[string]{log: dialect.LogDialect.Text, span: dialect.SpanDialect.Text})
	return cappedColumn{
		column: column[string]{
			key: TextColumnKey,
			byType: perEventType[string]{
				dialect.EventTypePrompt:       text,
				dialect.EventTypeAPIResponse:  text,
				dialect.EventTypeAPIError:     text,
				dialect.EventTypeToolDecision: text,
			},
		},
		capBytes: maxTextBytes,
	}
}

// An api_request and a tool_call are absent on purpose: they record that a
// call was made, not how it went. An accepted tool_decision carries no
// outcome; the result row says how the call went.
func columnOutcome() columnDefinition {
	outcome := getter[string]{log: dialect.LogDialect.Outcome, span: dialect.SpanDialect.Outcome}
	return column[string]{
		key: OutcomeColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIResponse:    constant(dialect.OutcomeOK),
			dialect.EventTypeAPIError:       constant(dialect.OutcomeError),
			dialect.EventTypeAPIRefusal:     constant(dialect.OutcomeRefused),
			dialect.EventTypeToolCallResult: outcome,
			dialect.EventTypeToolDecision:   recommended(outcome),
			dialect.EventTypeCompaction:     outcome,
		},
	}
}

var outcomeIsError = condition{
	log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) bool {
		return stated(d.Outcome(r)) == dialect.OutcomeError
	},
	span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) bool {
		return stated(d.Outcome(s)) == dialect.OutcomeError
	},
}

func columnOutcomeMessage() columnDefinition {
	message := getter[string]{log: dialect.LogDialect.OutcomeMessage, span: dialect.SpanDialect.OutcomeMessage}
	return column[string]{
		key: OutcomeMessageColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIError:       message,
			dialect.EventTypeToolCallResult: conditionallyRequired(message, outcomeIsError),
			dialect.EventTypeCompaction:     conditionallyRequired(message, outcomeIsError),
		},
	}
}

// A tool_call span is the whole call and has a duration; a tool_call log
// record is the call's start and has none. A compaction's duration stays in
// its payload with its token counts.
func columnDurationNano() columnDefinition {
	duration := getter[int64]{log: dialect.LogDialect.DurationNano, span: dialect.SpanDialect.DurationNano}
	return column[int64]{
		key: DurationNanoColumnKey,
		byType: perEventType[int64]{
			dialect.EventTypeAPIRequest:     duration,
			dialect.EventTypeToolCall:       recommended(duration),
			dialect.EventTypeToolCallResult: duration,
		},
	}
}

// Usage: what a request used. Only an api_request carries it; a compaction's
// before and after counts are housekeeping and stay in its payload. A stated
// zero is written.
func usageColumns() []columnDefinition {
	return []columnDefinition{
		columnInputTokens(),
		columnOutputTokens(),
		columnCacheReadTokens(),
		columnCacheWriteTokens(),
		columnCostUSD(),
	}
}

// input_tokens excludes the tokens read from the cache; the dialects turn
// Codex's cache-inclusive count into this shape.
func columnInputTokens() columnDefinition {
	return column[int64]{
		key: InputTokensColumnKey,
		byType: perEventType[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.InputTokens, span: dialect.SpanDialect.InputTokens},
		},
	}
}

func columnOutputTokens() columnDefinition {
	return column[int64]{
		key: OutputTokensColumnKey,
		byType: perEventType[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.OutputTokens, span: dialect.SpanDialect.OutputTokens},
		},
	}
}

func columnCacheReadTokens() columnDefinition {
	return column[int64]{
		key: CacheReadTokensColumnKey,
		byType: perEventType[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.CacheReadTokens, span: dialect.SpanDialect.CacheReadTokens},
		},
	}
}

// Codex reports no cache writes, so its requests count as missing here.
func columnCacheWriteTokens() columnDefinition {
	return column[int64]{
		key: CacheWriteTokensColumnKey,
		byType: perEventType[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.CacheWriteTokens, span: dialect.SpanDialect.CacheWriteTokens},
		},
	}
}

// Codex reports no cost, so its requests count as missing here.
func columnCostUSD() columnDefinition {
	return column[float64]{
		key: CostUSDColumnKey,
		byType: perEventType[float64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.CostUSD, span: dialect.SpanDialect.CostUSD},
		},
	}
}

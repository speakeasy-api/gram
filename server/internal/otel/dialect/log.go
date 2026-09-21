package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type LogDialect interface {
	AppliesTo(record *otelv1.InboundLogRecord) bool
	InputContent(record *otelv1.InboundLogRecord) (key string, val genaiconv.InputMessages, err error)
	OutputContent(record *otelv1.InboundLogRecord) (key string, val genaiconv.OutputMessages, err error)
	SessionID(record *otelv1.InboundLogRecord) (key string, val string, err error)
	ExternalUserID(record *otelv1.InboundLogRecord) (key string, val string, err error)
	ExternalUserEmail(record *otelv1.InboundLogRecord) (key string, val string, err error)
	ResponseID(record *otelv1.InboundLogRecord) (key string, val string, err error)

	// Producer knowledge. The key is what identified the producer, since the
	// answer is not read from the record.
	Provider(record *otelv1.InboundLogRecord) (key string, val string, err error)
	Surface(record *otelv1.InboundLogRecord) (key string, val string, err error)

	// What the record is, in agent vocabulary. Every answer names the
	// attribute it came from; an empty key means the producer did not say.
	EventName(record *otelv1.InboundLogRecord) (key string, val string, err error)
	EventType(record *otelv1.InboundLogRecord) (key string, val string, err error)
	SubjectID(record *otelv1.InboundLogRecord) (key string, val string, err error)
	TurnID(record *otelv1.InboundLogRecord) (key string, val string, err error)
	Model(record *otelv1.InboundLogRecord) (key string, val string, err error)
	ToolName(record *otelv1.InboundLogRecord) (key string, val string, err error)
	Outcome(record *otelv1.InboundLogRecord) (key string, val string, err error)
	OutcomeMessage(record *otelv1.InboundLogRecord) (key string, val string, err error)
	Text(record *otelv1.InboundLogRecord) (key string, val string, err error)
	DurationNano(record *otelv1.InboundLogRecord) (key string, val int64, err error)
	InputTokens(record *otelv1.InboundLogRecord) (key string, val int64, err error)
	OutputTokens(record *otelv1.InboundLogRecord) (key string, val int64, err error)
	CacheReadTokens(record *otelv1.InboundLogRecord) (key string, val int64, err error)
	CacheWriteTokens(record *otelv1.InboundLogRecord) (key string, val int64, err error)
	CostUSD(record *otelv1.InboundLogRecord) (key string, val float64, err error)
	QuerySource(record *otelv1.InboundLogRecord) (key string, val string, err error)
	SkillName(record *otelv1.InboundLogRecord) (key string, val string, err error)
	AgentName(record *otelv1.InboundLogRecord) (key string, val string, err error)
	MCPServerName(record *otelv1.InboundLogRecord) (key string, val string, err error)
	MCPToolName(record *otelv1.InboundLogRecord) (key string, val string, err error)
	ExternalOrgID(record *otelv1.InboundLogRecord) (key string, val string, err error)
}

var logDialects = []LogDialect{
	ClaudeCodeLog{},
	CodexLog{},
}

func ForLog(record *otelv1.InboundLogRecord) LogDialect {
	if record == nil {
		return NilLog{}
	}

	for _, candidate := range logDialects {
		if candidate.AppliesTo(record) {
			return LogFallback{Candidates: []LogDialect{candidate, SemconvLog{}}}
		}
	}

	return SemconvLog{}
}

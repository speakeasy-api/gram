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

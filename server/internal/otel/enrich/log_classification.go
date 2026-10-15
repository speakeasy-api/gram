package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

type logClassification struct{}

func (*logClassification) Name() string {
	return classificationEnricherName
}

func (*logClassification) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	surface := stated(d.Surface(record))
	provider := stated(d.Provider(record))
	switch {
	case isHooksRecord(record):
		// The surface is the hook source the endpoint resolved.
		surface = inboundLogAttributeString(record, string(attr.HookSourceKey))
		provider = hooksProvider(surface)
	}
	return classify(
		inboundLogSource(record),
		stated(d.EventName(record)),
		logEventType(d, record),
		inboundLogAttributeString(record, string(attr.ProviderKey)),
		provider,
		surface,
	), nil
}

// logEventType is what a log record is in the agent vocabulary. A hook row
// is typed by its hook event.
func logEventType(d dialect.LogDialect, record *otelv1.InboundLogRecord) string {
	if isHooksRecord(record) {
		return hooksEventType(stated(d.EventName(record)))
	}
	return stated(d.EventType(record))
}

// hooksEventType types the tool, prompt and permission hooks; session
// lifecycle, notifications and derived rows stay unclassified.
func hooksEventType(hookEvent string) string {
	switch hookEvent {
	case "PreToolUse", "BeforeMCPExecution":
		return dialect.EventTypeToolCall
	case "PostToolUse", "PostToolUseFailure", "AfterMCPExecution":
		return dialect.EventTypeToolCallResult
	case "UserPromptSubmit", "BeforeSubmitPrompt":
		return dialect.EventTypePrompt
	case "PermissionRequest":
		return dialect.EventTypeToolDecision
	default:
		return dialect.EventTypeUnclassified
	}
}

// hooksProvider is the provider a hook source's family implies.
func hooksProvider(hookSource string) string {
	switch hookSource {
	case "claude-code", "claude-code-desktop", "cowork", "claude-tag":
		return "anthropic"
	case "codex":
		return "openai"
	default:
		return ""
	}
}

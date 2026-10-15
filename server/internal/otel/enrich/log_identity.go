package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// logIdentity writes whose event a log record is: the session, the turn,
// the subject, the person and the account.
type logIdentity struct{}

func (*logIdentity) Name() string {
	return identityEnricherName
}

func (*logIdentity) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := logEventType(d, record)
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
	}
	if isHooksRecord(record) {
		return hooksIdentity(record, eventType), nil
	}

	var out []attribute.KeyValue
	if key, v, err := d.SessionID(record); known(key, err) {
		out = append(out, AgentSessionIDKey.String(v))
	}
	if key, v, err := d.TurnID(record); known(key, err) {
		out = append(out, AgentTurnIDKey.String(v))
	}
	// An api_request_body and a compaction have no subject of their own; the
	// writer keeps the record id for them.
	if eventType != dialect.EventTypeAPIRequestBody && eventType != dialect.EventTypeCompaction {
		if key, v, err := d.SubjectID(record); known(key, err) {
			out = append(out, AgentEventIDKey.String(v))
		}
	}
	if key, v, err := d.ExternalUserEmail(record); known(key, err) {
		out = append(out, AgentUserEmailKey.String(v))
	}
	if key, v, err := d.ExternalUserID(record); known(key, err) {
		out = append(out, AgentExternalUserIDKey.String(v))
	}
	if key, v, err := d.ExternalOrgID(record); known(key, err) {
		out = append(out, AgentExternalOrgIDKey.String(v))
	}
	return out, nil
}

// hooksIdentity reads the agent's own session id, not the chat it maps to,
// and the AI account the endpoint attributed the session to, not the
// Speakeasy user. Only the tool hooks name a subject.
func hooksIdentity(record *otelv1.InboundLogRecord, eventType string) []attribute.KeyValue {
	var out []attribute.KeyValue
	session := inboundLogAttributeString(record, string(attr.SessionIDKey))
	if session == "" {
		session = inboundLogAttributeString(record, "session.id")
	}
	if session != "" {
		out = append(out, AgentSessionIDKey.String(session))
	}
	if v := inboundLogAttributeString(record, string(attr.HookTurnIDKey)); v != "" {
		out = append(out, AgentTurnIDKey.String(v))
	}
	if isToolEvent(eventType) {
		if v := inboundLogAttributeString(record, string(attr.GenAIToolCallIDKey)); v != "" {
			out = append(out, AgentEventIDKey.String(v))
		}
	}
	if v := inboundLogAttributeString(record, string(attr.UserEmailKey)); v != "" {
		out = append(out, AgentUserEmailKey.String(v))
	}
	if v := inboundLogAttributeString(record, string(attr.ExternalUserIDKey)); v != "" {
		out = append(out, AgentExternalUserIDKey.String(v))
	}
	if v := inboundLogAttributeString(record, string(attr.ExternalOrgIDKey)); v != "" {
		out = append(out, AgentExternalOrgIDKey.String(v))
	}
	return out
}

package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// spanIdentity is logIdentity for spans.
type spanIdentity struct{}

func (*spanIdentity) Name() string {
	return identityEnricherName
}

func (*spanIdentity) Enrich(_ context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	eventType := stated(d.EventType(span))
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
	}

	var out []attribute.KeyValue
	if key, v, err := d.SessionID(span); known(key, err) {
		out = append(out, AgentSessionIDKey.String(v))
	}
	if key, v, err := d.TurnID(span); known(key, err) {
		out = append(out, AgentTurnIDKey.String(v))
	}
	if eventType != dialect.EventTypeAPIRequestBody && eventType != dialect.EventTypeCompaction {
		if key, v, err := d.SubjectID(span); known(key, err) {
			out = append(out, AgentEventIDKey.String(v))
		}
	}
	if key, v, err := d.ExternalUserEmail(span); known(key, err) {
		out = append(out, AgentUserEmailKey.String(v))
	}
	if key, v, err := d.ExternalUserID(span); known(key, err) {
		out = append(out, AgentExternalUserIDKey.String(v))
	}
	if key, v, err := d.ExternalOrgID(span); known(key, err) {
		out = append(out, AgentExternalOrgIDKey.String(v))
	}
	return out, nil
}

package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/attribute"
)

// logIdentity writes whose event a log record is: the session, the turn,
// the subject, the person, the account and the agent.
type logIdentity struct{}

func (*logIdentity) Name() string {
	return identityEnricherName
}

func (*logIdentity) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
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
	if agentID := inboundLogAgentID(record); agentID != "" {
		out = append(out, AgentAgentIDKey.String(agentID))
	}
	return out, nil
}

// inboundLogAgentID trusts only Speakeasy-emitted records, since a producer can send the same attributes.
func inboundLogAgentID(record *otelv1.InboundLogRecord) string {
	if record.GetProvenance().GetSource() != InternalProvenanceSource {
		return ""
	}
	if inboundLogAttributeString(record, string(attr.AuthorizationActorTypeKey)) != string(urn.PrincipalTypeAgent) {
		return ""
	}
	return inboundLogAttributeString(record, string(attr.AuthorizationActorIDKey))
}

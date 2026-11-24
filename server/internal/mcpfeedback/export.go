package mcpfeedback

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// exportEventName is the event name of every exported feedback record.
const exportEventName = "speakeasy.mcp_feedback"

// storedReport is one accepted report with everything that leaves with it.
type storedReport struct {
	id        uuid.UUID
	createdAt time.Time
	target    Target
	report    Report
	caller    Caller
}

// newRecordBuilder builds feedback records under their own scope and
// provenance source. The source is what routes a record to the MCP feedback
// export and lets it carry speakeasy.mcp_feedback attributes.
func newRecordBuilder(serviceName string) *otelpub.RecordBuilder {
	return otelpub.NewRecordBuilder(
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(serviceName)),
		dialect.FeedbackLogScope,
		otelpub.WithProvenanceSource(dialect.FeedbackLogSource),
	)
}

// exportSeverity maps the impact an agent reported onto a log level, so a
// destination can alert on blocked agents the way it alerts on errors. A
// report with no severity is informational, and so is praise.
func exportSeverity(severity Severity) (log.Severity, string) {
	switch severity {
	case SeverityBlocked:
		return log.SeverityError, "ERROR"
	case SeverityDegraded:
		return log.SeverityWarn, "WARN"
	case SeverityCosmetic, SeverityUnspecified:
		return log.SeverityInfo, "INFO"
	default:
		return log.SeverityInfo, "INFO"
	}
}

// exportRecord builds the log record for one stored report. The report's own
// text is the body, and what the agent wrote elsewhere and who sent it travel
// under keys a destination that excludes sensitive data gets redacted, so such
// a destination still learns that feedback arrived, for which server and tool,
// and how severe it was.
func exportRecord(stored storedReport) log.Record {
	severity, severityText := exportSeverity(stored.report.Severity)

	attrs := []attribute.KeyValue{
		attr.ExtMcpFeedbackID(stored.id.String()),
		// The relay strips the record id, so destinations dedupe redeliveries on this.
		attr.LogRecordUID(stored.id.String()),
		attr.ExtMcpFeedbackServerID(stored.target.McpServerID.String()),
	}
	for _, kv := range []attribute.KeyValue{
		attr.ExtMcpFeedbackSeverity(string(stored.report.Severity)),
		attr.ExtMcpFeedbackToolName(stored.report.ToolName),
		attr.ExtMcpFeedbackWorkaround(stored.report.Workaround),
		attr.ExtMcpFeedbackIntention(stored.report.Intention),
		attr.ExtMcpFeedbackModel(stored.report.Model),
		attr.ExtMcpFeedbackClientName(stored.caller.ClientName),
		attr.ExtMcpFeedbackClientVersion(stored.caller.ClientVersion),
		attr.ExtMcpFeedbackUserID(stored.caller.UserID),
		attr.UserEmailKey.String(stored.caller.UserEmail),
		attr.UserID(stored.caller.ExternalUserID),
	} {
		if kv.Value.AsString() != "" {
			attrs = append(attrs, kv)
		}
	}

	record := newExportRecord(exportEventName, stored.createdAt, severity, severityText, attrs)
	record.SetBody(log.StringValue(stored.report.Content))
	return record
}

// testExportRecord is the event a destination test sends: the export's shape
// with no report, person or server in it. The route tester adds the body and
// the test marks.
func testExportRecord(at time.Time) log.Record {
	return newExportRecord(otelsvc.TestEventName, at, log.SeverityInfo, "INFO", []attribute.KeyValue{
		attr.LogRecordUID(uuid.NewString()),
	})
}

func newExportRecord(eventName string, at time.Time, severity log.Severity, severityText string, attrs []attribute.KeyValue) log.Record {
	var record log.Record
	record.SetEventName(eventName)
	record.SetTimestamp(at)
	record.SetSeverity(severity)
	record.SetSeverityText(severityText)
	for _, kv := range attrs {
		record.AddAttributes(log.KeyValueFromAttribute(kv))
	}
	return record
}

// webhookPayload is the webhook event for one stored report.
func webhookPayload(stored storedReport) events.McpFeedbackSubmittedPayloadV1 {
	return events.McpFeedbackSubmittedPayloadV1{
		ID:             stored.id,
		OrganizationID: stored.target.OrganizationID,
		ProjectID:      stored.target.ProjectID,
		McpServerID:    stored.target.McpServerID,
		Content:        stored.report.Content,
		Severity:       string(stored.report.Severity),
		Workaround:     stored.report.Workaround,
		Intention:      stored.report.Intention,
		ToolName:       stored.report.ToolName,
		Model:          stored.report.Model,
		UserID:         stored.caller.UserID,
		ClientName:     stored.caller.ClientName,
		ClientVersion:  stored.caller.ClientVersion,
		CreatedAt:      stored.createdAt,
	}
}

// TestSource builds the MCP feedback export's test event exactly as the
// recorder builds real records. It never publishes.
type TestSource struct {
	records  *otelpub.RecordBuilder
	requests *otelsvc.FeedbackLogTestRequestBuilder
}

var _ otelsvc.RouteTestSource = (*TestSource)(nil)

// NewTestSource returns the test source for the MCP feedback export.
// serviceName is the service.name real records carry.
func NewTestSource(requests *otelsvc.FeedbackLogTestRequestBuilder, serviceName string) *TestSource {
	return &TestSource{records: newRecordBuilder(serviceName), requests: requests}
}

func (s *TestSource) DataSource() string { return dataexports.DataSourceMCPFeedback }

func (s *TestSource) BuildTestRequest(ctx context.Context, signal dataexports.Signal, destination dataexports.TestDestination) (proto.Message, error) {
	if signal != dataexports.SignalLogs {
		return nil, fmt.Errorf("%s does not export %s", s.DataSource(), signal)
	}

	record := testExportRecord(time.Now())
	inbound, err := s.records.Prepare(otelpub.WithTenant(ctx, destination.OrganizationID, destination.ProjectID.String()), &record)
	if err != nil {
		return nil, fmt.Errorf("prepare mcp feedback test record: %w", err)
	}

	request, err := s.requests.Build(ctx, inbound, destination)
	if err != nil {
		return nil, fmt.Errorf("build mcp feedback test request: %w", err)
	}

	return request, nil
}

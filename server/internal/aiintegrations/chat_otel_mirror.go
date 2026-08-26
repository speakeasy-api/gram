package aiintegrations

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

const (
	// chatOTELProvenanceSource is stamped as provenance.source on mirrored
	// records, distinguishing them from OTLP exports ingested at the edge
	// with source "speakeasy".
	chatOTELProvenanceSource = "compliance-import"

	// chatOTELEventName types every mirrored record so downstream consumers
	// can select compliance chat messages by event name alone.
	chatOTELEventName = "gram.compliance.chat_message"

	// chatOTELModelAttr carries the model that produced the message, using
	// the gen-ai semconv key so it lines up with OTLP-ingested records.
	chatOTELModelAttr = "gen_ai.request.model"

	// chatOTELServiceNameAttr is the OTel resource attribute the ClickHouse
	// event feed derives its source column from.
	chatOTELServiceNameAttr = "service.name"

	// chatOTELPublishAckTimeout bounds the detached goroutine that drains
	// publish acks for one batch. The broker publish itself is bounded
	// separately by the publisher's PublishSettings.
	chatOTELPublishAckTimeout = 10 * time.Second
)

// chatOTELRecordNamespace is the UUIDv5 namespace for mirrored record ids. A
// record id derives from (chat_id, external_message_id) — the same pair that
// dedupes the Postgres import — so a replayed publish yields the identical
// record id and downstream consumers can dedupe.
var chatOTELRecordNamespace = uuid.MustParse("5f2ac9a7-1f7d-4c15-9f28-3f4be3a1d0b6")

// ChatOTELMirror mirrors fetched compliance chat messages onto the
// gram.otel.v1.InboundLogRecord Pub/Sub topic, feeding the OTEL log pipeline
// (transform/enrichment, ClickHouse event feed, relay) alongside the primary
// Postgres write. It is best-effort and non-blocking: publish acks drain on a
// detached goroutine and failures surface as a single error log — a mirror
// failure never fails the sync that fetched the rows.
type ChatOTELMirror struct {
	logger *slog.Logger
	pub    gcp.Publisher[*otelv1.InboundLogRecord]

	// drains tracks in-flight ack-drain goroutines so tests can await them
	// deterministically.
	drains sync.WaitGroup
}

// NewChatOTELMirror constructs a ChatOTELMirror. Callers must always pass a
// publisher — a real Pub/Sub publisher, or gcp.NewNoopPublisher where the
// mirror is not wanted (tests, workers without a broker).
func NewChatOTELMirror(logger *slog.Logger, pub gcp.Publisher[*otelv1.InboundLogRecord]) *ChatOTELMirror {
	return &ChatOTELMirror{
		logger: logger.With(attr.SlogComponent("aiintegrations.chat_otel_mirror")),
		pub:    pub,
		drains: sync.WaitGroup{},
	}
}

// PublishMessages mirrors fetched chat message rows onto the OTEL log topic.
// Callers publish before writing the rows to Postgres, so a retried batch may
// publish the same rows again; record ids are deterministic so downstream
// consumers can dedupe.
func (m *ChatOTELMirror) PublishMessages(ctx context.Context, cfg Config, rows []chatrepo.CreateExternalChatMessageParams) {
	if len(rows) == 0 {
		return
	}

	// Detach caller cancellation (activity teardown) while keeping trace
	// context so publishes are not dropped mid-batch; the publisher's own
	// settings and chatOTELPublishAckTimeout bound the work instead.
	ctx = context.WithoutCancel(ctx)

	results := make([]gcp.PublishResult, len(rows))
	for i, row := range rows {
		results[i] = m.pub.Publish(ctx, chatMessageLogRecord(cfg, row))
	}

	m.drains.Add(1)
	go m.drainPublishAcks(ctx, results)
}

// drainPublishAcks waits for every publish result of one batch and surfaces
// failures in a single error log.
func (m *ChatOTELMirror) drainPublishAcks(ctx context.Context, results []gcp.PublishResult) {
	defer m.drains.Done()

	ctx, cancel := context.WithTimeout(ctx, chatOTELPublishAckTimeout)
	defer cancel()

	var firstErr error
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		m.logger.ErrorContext(ctx, "failed to publish compliance chat messages to otel log topic", attr.SlogError(firstErr))
	}
}

// chatMessageLogRecord maps one fetched chat message row to its inbound OTEL
// log record, in the wire shape the dialect.ComplianceLog interpreter reads.
// Every derived field is deterministic — the record id comes from the same
// (chat_id, external_message_id) pair that dedupes the Postgres import and
// both timestamps come from the row's created_at — so a replayed publish
// produces an identical record.
func chatMessageLogRecord(cfg Config, row chatrepo.CreateExternalChatMessageParams) *otelv1.InboundLogRecord {
	recordID := uuid.NewSHA1(chatOTELRecordNamespace, []byte(row.ChatID.String()+"/"+row.ExternalMessageID.String)).String()
	timeNano := uint64(row.CreatedAt.Time.UnixNano())

	attrs := []*otelv1.InboundLogRecord_KeyValue{
		chatOTELStringAttr(dialect.ComplianceLogRoleAttr, row.Role),
		chatOTELStringAttr(dialect.ComplianceLogChatIDAttr, row.ChatID.String()),
		chatOTELStringAttr(dialect.ComplianceLogExternalMessageIDAttr, row.ExternalMessageID.String),
	}
	if row.ExternalUserID.String != "" {
		attrs = append(attrs, chatOTELStringAttr(dialect.ComplianceLogUserIDAttr, row.ExternalUserID.String))
	}
	if row.Model.String != "" {
		attrs = append(attrs, chatOTELStringAttr(chatOTELModelAttr, row.Model.String))
	}

	severity := otelv1.InboundLogRecord_SEVERITY_NUMBER_INFO
	eventName := chatOTELEventName
	provenanceSource := chatOTELProvenanceSource
	organizationID := cfg.OrganizationID
	projectID := cfg.ProjectID.String()
	scopeName := dialect.ComplianceLogScopeName

	return (&otelv1.InboundLogRecord_builder{
		TimeUnixNano:         &timeNano,
		ObservedTimeUnixNano: &timeNano,
		SeverityNumber:       &severity,
		Body:                 (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &row.Content}).Build(),
		Attributes:           attrs,
		EventName:            &eventName,
		RecordId:             &recordID,
		Resource: (&otelv1.InboundLogRecord_Resource_builder{
			// The row's source slug (e.g. claude-web, chatgpt) becomes the
			// ClickHouse event feed's source column.
			Attributes: []*otelv1.InboundLogRecord_KeyValue{chatOTELStringAttr(chatOTELServiceNameAttr, row.Source.String)},
		}).Build(),
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: &scopeName}).Build(),
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			Source:         &provenanceSource,
			OrganizationId: &organizationID,
			ProjectId:      &projectID,
		}).Build(),
	}).Build()
}

func chatOTELStringAttr(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

package mcpconnectionattempts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	meterExportSkipped = "mcp.connection.events.export_skipped"

	// exportQueueSize bounds rows waiting for the publisher. A full queue drops instead of blocking the writer.
	exportQueueSize = 1024
	exportWorkers   = 2

	// exportTimeout bounds the flag check and publish of one row.
	exportTimeout = 15 * time.Second

	exportEventName = "gram.mcp_connection"

	exportSkipNoProject     = "no_project"
	exportSkipUnknownReason = "unknown_reason"
)

// ExportSink publishes connection logs. Publish and Enabled may block, so only export workers call them.
type ExportSink interface {
	Enabled(ctx context.Context, organizationID string) bool
	Publish(ctx context.Context, organizationID, projectID, recordID string, record log.Record) error
	Close()
}

type exporter struct {
	sink    ExportSink
	queue   chan Row
	workers sync.WaitGroup
	skipped metric.Int64Counter
}

// WithExport sends final failed, blocked and declined rows to sink once they are stored.
func WithExport(sink ExportSink) RecorderOption {
	return func(r *Recorder) {
		r.exporter = &exporter{sink: sink, queue: make(chan Row, exportQueueSize), workers: sync.WaitGroup{}, skipped: nil}
	}
}

func (r *Recorder) startExport(meter metric.Meter) {
	if r.exporter == nil {
		return
	}
	skipped, err := meter.Int64Counter(meterExportSkipped,
		metric.WithDescription("Stored MCP connection events left out of the export, by reason."),
		metric.WithUnit("{event}"))
	if err != nil {
		r.logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterExportSkipped), attr.SlogError(err))
	}
	r.exporter.skipped = skipped

	r.exporter.workers.Add(exportWorkers)
	for range exportWorkers {
		go r.exportWorker()
	}
}

// stopExport runs once the writer is done, so nothing sends on the closed queue.
func (r *Recorder) stopExport() {
	if r.exporter == nil {
		return
	}
	close(r.exporter.queue)
	r.exporter.workers.Wait()
	r.exporter.sink.Close()
}

// exportRows runs on the writer goroutine, so it only filters and hands off.
func (r *Recorder) exportRows(rows []Row) {
	if r.exporter == nil {
		return
	}
	for _, row := range rows {
		if _, _, ok := exportSeverity(row); !ok {
			continue
		}
		// otelpub requires a project, and org gateways have none.
		if row.ProjectID == uuid.Nil {
			r.countExportSkip(row, exportSkipNoProject)
			continue
		}
		select {
		case r.exporter.queue <- row:
		default:
			r.drop(context.Background(), dropExportQueueFull, events(row), nil)
		}
	}
}

func (r *Recorder) countExportSkip(row Row, reason string) {
	if r.exporter.skipped != nil {
		r.exporter.skipped.Add(context.Background(), int64(events(row)), metric.WithAttributes(attr.Reason(reason)))
	}
}

func (r *Recorder) exportWorker() {
	defer r.exporter.workers.Done()

	for row := range r.exporter.queue {
		r.surviving(events(row), func() { r.exportRow(row) })
	}
}

func (r *Recorder) exportRow(row Row) {
	if r.writerCtx.Err() != nil {
		r.drop(context.Background(), dropShutdown, events(row), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.writerCtx, exportTimeout)
	defer cancel()

	if !r.exporter.sink.Enabled(ctx, row.OrganizationID) {
		return
	}
	record, ok := exportRecord(row)
	if !ok {
		r.countExportSkip(row, exportSkipUnknownReason)
		return
	}
	ctx = withRowTrace(ctx, row)
	if err := r.exporter.sink.Publish(ctx, row.OrganizationID, row.ProjectID.String(), exportRecordID(row), record); err != nil {
		r.drop(context.Background(), dropExportFailed, events(row), fmt.Errorf("publish connection log: %w", err))
	}
}

// exportRecordID keeps two endings of one attempt apart, such as a replayed callback.
func exportRecordID(row Row) string {
	return row.AttemptID.String() + ":" + row.Step + ":" + row.Reason
}

func withRowTrace(ctx context.Context, row Row) context.Context {
	traceID, err := trace.TraceIDFromHex(row.TraceID)
	if err != nil {
		return ctx
	}
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{},
		TraceFlags: 0,
		TraceState: trace.TraceState{},
		Remote:     false,
	}))
}

// exportSeverity also decides what is exported: final rows with a reason that did not connect.
func exportSeverity(row Row) (log.Severity, string, bool) {
	if !row.IsFinal || row.Reason == "" {
		return log.SeverityUndefined, "", false
	}
	switch Outcome(row.Outcome) {
	case OutcomeFailed:
		return log.SeverityError, "ERROR", true
	case OutcomeBlocked:
		return log.SeverityWarn, "WARN", true
	case OutcomeDeclined:
		return log.SeverityInfo, "INFO", true
	case OutcomeConnected, OutcomeAbandoned:
		return log.SeverityUndefined, "", false
	default:
		return log.SeverityUndefined, "", false
	}
}

// exportRecord builds the event from the stored row and the catalog. The text is in attributes
// because destinations that exclude sensitive data redact string bodies.
func exportRecord(row Row) (log.Record, bool) {
	var record log.Record
	severity, severityText, ok := exportSeverity(row)
	if !ok {
		return record, false
	}
	meaning, ok := MeaningOf(row.Reason)
	if !ok {
		return record, false
	}

	attrs := []attribute.KeyValue{
		attr.ConnectionAttemptID(row.AttemptID.String()),
		attr.ConnectionOutcome(row.Outcome),
		attr.ConnectionReason(row.Reason),
		attr.ErrorType(row.Reason),
		attr.ErrorMessage(meaning.WhatHappened),
		attr.ConnectionWhatToDo(meaning.WhatToDo),
		attr.ConnectionStep(row.Step),
		attr.ConnectionDuration(row.EventTime.Sub(row.AttemptStartedAt)),
		attr.ConnectionRepeats(row.Repeats),
		attr.ConnectionRetries(row.Retries),
	}
	for _, kv := range []attribute.KeyValue{
		attr.ConnectionWhoFixes(row.WhoFixes),
		attr.ConnectionServerKind(row.ServerKind),
		attr.ConnectionServerID(row.ServerID),
		attr.ConnectionMemberID(row.MemberID),
		attr.ServerAddress(row.ProviderHost),
		attr.ConnectionGrantType(row.GrantType),
		attr.McpClientName(row.ClientFamily),
		attr.McpClientVersion(row.ClientVersion),
		attr.ConnectionSubjectKind(subjectKind(row.SubjectURN)),
	} {
		if kv.Value.AsString() != "" {
			attrs = append(attrs, kv)
		}
	}

	record.SetEventName(exportEventName)
	record.SetTimestamp(row.EventTime)
	record.SetSeverity(severity)
	record.SetSeverityText(severityText)
	record.SetBody(log.StringValue("MCP connection " + row.Outcome))
	for _, kv := range attrs {
		record.AddAttributes(log.KeyValueFromAttribute(kv))
	}
	return record, true
}

// subjectKind keeps a subject's kind and never its id.
func subjectKind(subjectURN string) string {
	subject, err := urn.ParseSessionSubject(subjectURN)
	if err != nil {
		return ""
	}
	return string(subject.Kind)
}

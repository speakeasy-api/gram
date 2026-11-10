package mcpconnectionattempts

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	meterNotExported = "mcp.connection.events.not_exported"

	// exportMaxInFlight bounds publishes Pub/Sub has not acked yet. Past it rows drop
	// instead of growing memory while Pub/Sub is slow.
	exportMaxInFlight = 1024

	// exportTimeout bounds how long one row waits for its ack.
	exportTimeout = 15 * time.Second

	// defaultExportDrain is how long outstanding publishes may settle at shutdown.
	defaultExportDrain = 5 * time.Second

	exportLogInterval = time.Minute

	exportEventName = "speakeasy.mcp_connection"

	// minAttemptDuration separates a real span from a one-row attempt, whose start and end are the same moment.
	minAttemptDuration = time.Millisecond
)

// notExportedReason says why a stored row did not reach the export.
type notExportedReason string

const (
	notExportedNoProject notExportedReason = "no_project"
	notExportedQueueFull notExportedReason = "export_queue_full"
	notExportedFailed    notExportedReason = "export_failed"
	notExportedShutdown  notExportedReason = "shutdown"
	notExportedPanic     notExportedReason = "panic"
)

// ExportSink publishes connection logs. Publish must not wait for the ack: the writer
// goroutine calls it, and the returned result settles once Pub/Sub has the record.
type ExportSink interface {
	Publish(ctx context.Context, organizationID, projectID, recordID string, record log.Record) gcp.PublishResult
	Close()
}

type exporter struct {
	sink        ExportSink
	maxInFlight int64
	inFlight    atomic.Int64
	// acks joins the goroutines waiting on outstanding results.
	acks        sync.WaitGroup
	notExported metric.Int64Counter
	drain       time.Duration
	lastLog     atomic.Int64
}

// WithExport sends stored rows that an organization's admins can act on to sink.
func WithExport(sink ExportSink) RecorderOption {
	return func(r *Recorder) {
		r.exporter = &exporter{sink: sink, maxInFlight: exportMaxInFlight, inFlight: atomic.Int64{}, acks: sync.WaitGroup{}, notExported: nil, drain: defaultExportDrain, lastLog: atomic.Int64{}}
	}
}

func (r *Recorder) startExport(meter metric.Meter) {
	if r.exporter == nil {
		return
	}
	notExported, err := meter.Int64Counter(meterNotExported,
		metric.WithDescription("Stored MCP connection events that were not exported, by reason. The rows themselves were written."),
		metric.WithUnit("{event}"))
	if err != nil {
		r.logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterNotExported), attr.SlogError(err))
	}
	r.exporter.notExported = notExported
}

// stopExport runs once the writer is done, so no new publishes start. It gives outstanding
// ones their budget, then cancels their waits, which count as shutdown; Pub/Sub may still
// deliver what it holds. A result's Get must honor its context, so the join is prompt.
func (r *Recorder) stopExport() {
	if r.exporter == nil {
		return
	}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		r.exporter.acks.Wait()
		r.exporter.sink.Close()
	}()

	budget := time.NewTimer(r.exporter.drain)
	defer budget.Stop()
	select {
	case <-finished:
		return
	case <-budget.C:
	case <-r.writerCtx.Done():
	}
	r.cancelWriter()
	<-finished
}

// exportRows runs on the writer goroutine. Publishing does not wait for Pub/Sub, so it
// only builds each record and hands it over.
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
			r.countNotExported(row, notExportedNoProject, nil)
			continue
		}
		// Only the writer adds, so the check and the add do not race.
		if r.exporter.inFlight.Load() >= r.exporter.maxInFlight {
			r.countNotExported(row, notExportedQueueFull, nil)
			continue
		}
		r.exportRow(row)
	}
}

// countNotExported counts on its own metric because these rows were stored.
func (r *Recorder) countNotExported(row Row, reason notExportedReason, cause error) {
	ctx := context.Background()
	if r.exporter.notExported != nil {
		r.exporter.notExported.Add(ctx, int64(events(row)), metric.WithAttributes(attr.Reason(string(reason))))
	}
	if reason == notExportedNoProject {
		return
	}
	now := time.Now().UnixNano()
	previous := r.exporter.lastLog.Load()
	if now-previous < int64(exportLogInterval) || !r.exporter.lastLog.CompareAndSwap(previous, now) {
		return
	}
	attrs := []any{attr.SlogReason(string(reason))}
	if cause != nil {
		attrs = append(attrs, attr.SlogError(cause))
	}
	r.logger.WarnContext(ctx, "connection log not exported; the attempt is stored", attrs...)
}

// exportRow publishes one row. A panicking sink costs the row, never the writer.
func (r *Recorder) exportRow(row Row) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.countNotExported(row, notExportedPanic, fmt.Errorf("connection log export: %v", recovered))
		}
	}()
	if r.writerCtx.Err() != nil {
		r.countNotExported(row, notExportedShutdown, nil)
		return
	}
	record, ok := exportRecord(row)
	if !ok {
		return
	}

	result := r.exporter.sink.Publish(withRowTrace(r.writerCtx, row), row.OrganizationID, row.ProjectID.String(), exportRecordID(row), record)
	r.exporter.inFlight.Add(1)
	r.exporter.acks.Go(func() {
		defer r.exporter.inFlight.Add(-1)
		ctx, cancel := context.WithTimeout(r.writerCtx, exportTimeout)
		defer cancel()
		if _, err := result.Get(ctx); err != nil {
			reason := notExportedFailed
			if r.writerCtx.Err() != nil {
				reason = notExportedShutdown
			}
			r.countNotExported(row, reason, err)
		}
	})
}

// exportRecordID stays stable across redelivery. The member and time keep one attempt's
// rows apart: gateway members share a step and reason, and a step can repeat.
func exportRecordID(row Row) string {
	return strings.Join([]string{row.AttemptID.String(), row.Step, row.Reason, row.MemberID, strconv.FormatInt(row.EventTime.UnixNano(), 10)}, ":")
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

// exportSeverity also decides what is exported: every final row that did not connect,
// and non-final rows only when someone other than the person has to fix them.
func exportSeverity(row Row) (log.Severity, string, bool) {
	meaning, ok := MeaningOf(row.Reason)
	if !ok {
		return log.SeverityUndefined, "", false
	}
	if !row.IsFinal {
		switch WhoFixes(row.WhoFixes) {
		case WhoAdmin, WhoProvider, WhoSpeakeasy:
		case WhoNobody, WhoPerson, WhoApp:
			return log.SeverityUndefined, "", false
		default:
			return log.SeverityUndefined, "", false
		}
	}
	switch meaning.Outcome {
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

// exportRecord builds the event from the stored row and the catalog. It has no body because
// destinations that exclude sensitive data redact string bodies. The catalog text is written
// to the person who hit the failure, so its keys say so.
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
		attr.ExtMcpConnectionAttemptID(row.AttemptID.String()),
		attr.ExtMcpConnectionFinal(row.IsFinal),
		// error.type holds the connection reason code.
		attr.ErrorType(row.Reason),
		attr.ExtMcpConnectionUserMessage(meaning.WhatHappened),
		attr.ExtMcpConnectionUserNextStep(meaning.WhatToDo),
		attr.ExtMcpConnectionStep(row.Step),
		attr.ExtMcpConnectionStepResult(row.StepResult),
		attr.ExtMcpConnectionRepeatedFailures(row.Repeats),
	}
	if duration := row.EventTime.Sub(row.AttemptStartedAt); duration >= minAttemptDuration {
		attrs = append(attrs, attr.ExtMcpConnectionDuration(duration))
	}
	// server.address is the external host that failed: the sign-in provider on sign-in and
	// refresh steps, the upstream MCP server on MCP requests. Unknown means it is left out.
	address, port := "", 0
	if !row.failedElsewhere {
		address, port = splitProviderHost(row.ProviderHost)
	}
	if port != 0 {
		attrs = append(attrs, attr.ServerPort(port))
	}
	for _, kv := range []attribute.KeyValue{
		attr.ExtMcpConnectionOutcome(row.Outcome),
		attr.ExtMcpConnectionWhoFixes(row.WhoFixes),
		attr.ExtMcpConnectionServerKind(row.ServerKind),
		attr.ExtMcpConnectionServerID(row.ServerID),
		attr.ExtMcpConnectionMemberID(row.MemberID),
		attr.ServerAddress(address),
		attr.ExtMcpConnectionGrantType(row.GrantType),
		attr.ExtMcpConnectionClientName(row.ClientFamily),
		attr.ExtMcpConnectionClientVersion(row.ClientVersion),
		attr.ExtMcpConnectionSubjectKind(subjectKind(row.SubjectURN)),
	} {
		if kv.Value.AsString() != "" {
			attrs = append(attrs, kv)
		}
	}

	record.SetEventName(exportEventName)
	record.SetTimestamp(row.EventTime)
	record.SetSeverity(severity)
	record.SetSeverityText(severityText)
	for _, kv := range attrs {
		record.AddAttributes(log.KeyValueFromAttribute(kv))
	}
	return record, true
}

// splitProviderHost undoes NormalizeProviderHost's host:port and IPv6 brackets.
func splitProviderHost(providerHost string) (string, int) {
	host, portText, err := net.SplitHostPort(providerHost)
	if err != nil {
		return strings.Trim(providerHost, "[]"), 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return host, 0
	}
	return host, port
}

// subjectKind keeps a subject's kind and never its id.
func subjectKind(subjectURN string) string {
	subject, err := urn.ParseSessionSubject(subjectURN)
	if err != nil {
		return ""
	}
	return string(subject.Kind)
}

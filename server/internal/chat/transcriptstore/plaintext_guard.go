package transcriptstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// ErrPlaintextRead means a read for a project on the shadow list would have
// served plaintext because this process cannot open shadows.
var ErrPlaintextRead = errors.New("plaintext chat transcript read for a project that keeps encrypted shadows")

// meterPlaintextReads counts reads, by reader, that served or would have
// served plaintext for a project on the shadow list. It is not the
// plaintext_fallback outcome of shadow reads, which is legal: that counts
// messages that have no shadow yet.
const meterPlaintextReads = "chat.transcript_shadow.plaintext_reads"

// outcomeUnopened is a listed project read by a process with no store. With
// a strict guard the read fails; otherwise it is served from plaintext,
// logged and counted.
const outcomeUnopened outcome = "unopened"

// plaintextGuard reports reads that bypass the shadows of a listed project.
type plaintextGuard struct {
	logger *slog.Logger
	reads  metric.Int64Counter

	// strict fails the read instead of serving plaintext. Tests and local
	// development run strict; production logs, counts and serves, so an
	// on-call alert fires without breaking the request.
	strict bool
}

// NewReadOnlyTranscripts knows which projects keep shadows but cannot open
// them, for a process that reads chat messages without configuring the shadow
// store. Every read for a listed project goes through the plaintext guard.
func NewReadOnlyTranscripts(projectIDs ...uuid.UUID) *Transcripts {
	t := NewTranscripts(nil)
	for _, projectID := range projectIDs {
		t.projects[projectID] = struct{}{}
	}
	return t
}

// WithPlaintextGuard reports a read for a listed project that this process
// cannot open: an error log and a count of meterPlaintextReads, and, when
// strict, a failed read. Without a guard such a read fails.
func (t *Transcripts) WithPlaintextGuard(logger *slog.Logger, meterProvider metric.MeterProvider, strict bool) (*Transcripts, error) {
	reads, err := meterProvider.Meter(instrumentationName).Int64Counter(
		meterPlaintextReads,
		metric.WithDescription("Chat transcript reads that served or would have served plaintext for a project that keeps encrypted shadows"),
		metric.WithUnit("{read}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterPlaintextReads, err)
	}
	t.guard = &plaintextGuard{logger: logger, reads: reads, strict: strict}
	return t, nil
}

// checkOpenable lets a read through unless the project is on the shadow list
// and this process has no store to open its shadows.
func (t *Transcripts) checkOpenable(ctx context.Context, projectID uuid.UUID, reader string) error {
	if t == nil || t.store != nil {
		return nil
	}
	if _, listed := t.projects[projectID]; !listed {
		return nil
	}
	if t.guard == nil {
		return fmt.Errorf("%s: %w", reader, ErrPlaintextRead)
	}
	t.guard.reads.Add(ctx, 1, metric.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.TranscriptShadowReader(reader),
		attr.Outcome(outcomeUnopened),
	))
	t.guard.logger.ErrorContext(ctx, "plaintext chat transcript read for a project that keeps encrypted shadows",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTranscriptShadowReader(reader),
		attr.SlogOutcome(string(outcomeUnopened)),
	)
	if t.guard.strict {
		return fmt.Errorf("%s: %w", reader, ErrPlaintextRead)
	}
	return nil
}

package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/temporal"
)

// newTranscriptWriter builds the chat message writer whose observers wake the
// coordinators that consume transcript rows: risk analysis, skill efficacy, and
// chat analysis.
//
// It exists because those coordinators sleep until signalled and complete when
// no signal is pending — nothing sweeps for rows they missed. On the
// synchronous hook path the API server's writer emits that wake. When the row
// is written here instead, the wake has to come from here too, or the row is
// stored and never looked at.
//
// The writer's asset storage is nil: this process only uses the writer for its
// observers, never for its content-uploading write methods.
//
// It returns the narrow notifier interface rather than the writer because the
// consumer decides whether to wake on `notifier == nil`, and a typed-nil
// *ChatMessageWriter would leave that check false and route wakes into a nil
// receiver.
//
// The Temporal environment is passed in rather than dialled here. The streams
// command already builds one and refuses to start without it, so a second
// client would be a second connection to the same server for the same flags.
func newTranscriptWriter(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	temporalEnv *temporal.Environment,
) (hooks.StoredNotifier, func(context.Context) error) {
	writer, writerShutdown := chat.NewChatMessageWriter(logger, db, nil)

	auditLogger := newAuditLogger()

	// Held so shutdown can flush them. A ThrottledSignaler coalesces wakes and
	// fires the last one on the trailing edge of its cooldown; dropped on exit,
	// that final wake never happens and the rows it would have announced sit
	// unanalysed until some later write happens to wake the coordinator again.
	riskSignaler := background.NewThrottledSignaler(
		&background.TemporalRiskAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
		background.RiskAnalysisSignalCooldown,
		logger.With(attr.SlogComponent("risk")),
	)
	efficacySignaler := background.NewThrottledSignaler(
		&background.TemporalSkillEfficacySignaler{TemporalEnv: temporalEnv, Logger: logger},
		background.SkillEfficacySignalCooldown,
		logger.With(attr.SlogComponent("skill-efficacy")),
	)
	chatAnalysisSignaler := background.NewThrottledSignaler(
		&background.TemporalChatAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
		background.ChatAnalysisSignalCooldown,
		logger.With(attr.SlogComponent("chat-analysis")),
	)

	writer.AddObserver(risk.NewObserver(logger, tracerProvider, db, riskSignaler, auditLogger))
	writer.AddObserver(efficacy.NewObserver(logger, efficacySignaler))
	writer.AddObserver(analysis.NewObserver(logger, chatAnalysisSignaler))

	shutdown := func(ctx context.Context) error {
		// Signalers first, then the writer, then Temporal.
		//
		// Flushing before the writer is cancelled is the whole point of this
		// order. notifyMessagesStored fires observers on a detached goroutine
		// whose context is cancelled by the writer's shutdown, so cancelling
		// first actively kills the wakes still in flight — the flush then has
		// nothing left to push. The API server has the same constraint and
		// solves it the same way: it flushes only after the HTTP server is
		// drained, and never cancels its writer first (see start.go).
		//
		// The Temporal client is not closed here: the streams command owns it
		// and tears it down after this shutdown runs, which keeps the
		// trailing-edge flush's gRPC connection alive long enough to land.
		//
		// This narrows the window rather than closing it. Observers are
		// fire-and-forget goroutines and the writer offers no way to wait for
		// them, so a wake started in the final moments can still be missed. That
		// gap is shared with the API server path and wants a real drain on
		// ChatMessageWriter to fix properly.
		for _, s := range []struct {
			name     string
			signaler *background.ThrottledSignaler
		}{
			{name: "risk", signaler: riskSignaler},
			{name: "skill-efficacy", signaler: efficacySignaler},
			{name: "chat-analysis", signaler: chatAnalysisSignaler},
		} {
			if err := s.signaler.Shutdown(ctx); err != nil {
				return fmt.Errorf("flush %s coordinator signals: %w", s.name, err)
			}
		}
		if err := writerShutdown(ctx); err != nil {
			return fmt.Errorf("shutdown transcript writer: %w", err)
		}
		return nil
	}
	return writer, shutdown
}

package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/temporal"
)

// newTranscriptWriter builds the chat message writer whose observers wake the
// coordinators that consume transcript rows: risk analysis, skill efficacy, and
// chat analysis.
//
// Those coordinators sleep until signalled and complete when no signal is
// pending — nothing sweeps for rows they missed, so a row written here without
// a wake is never looked at.
//
// Asset storage is nil: this process uses the writer for its observers and its
// metering, never for the content-uploading write methods. The Temporal
// environment is passed in because the streams command already built one.
func newTranscriptWriter(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	temporalEnv *temporal.Environment,
	auditLogger *audit.Logger,
) (*chat.ChatMessageWriter, func(context.Context) error) {
	writer, writerShutdown := chat.NewChatMessageWriter(logger, db, nil)

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
		// Signalers before the writer: notifyMessagesStored fires observers on a
		// goroutine the writer's shutdown cancels, so cancelling first kills the
		// wakes the flush is meant to push. start.go orders it the same way.
		//
		// This narrows the window rather than closing it — observers are
		// fire-and-forget and the writer offers no drain, so a wake started in
		// the final moments can still be missed.
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

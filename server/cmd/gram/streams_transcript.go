package gram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// riskAnalysisSignalCooldown mirrors the value the worker uses. A wake carries
// no payload, so a burst of them coalesces into the single pass they all ask
// for.
const riskAnalysisSignalCooldown = 30 * time.Second

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
// It returns the narrow notifier interface rather than the writer so the
// unconfigured case can be a true nil interface. Handing back a typed-nil
// *ChatMessageWriter would leave the consumer's `notifier == nil` check false
// and route wakes into a nil receiver — which happens to be safe today only
// because notifyMessagesStored guards on it.
//
// With Temporal unconfigured this returns nil, and nil means no wake. That is
// loud at boot rather than silent, because the failure it produces —
// transcripts that persist correctly and are never analysed — looks like
// nothing at all from the outside.
func newTranscriptWriter(
	c *cli.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
) (hooks.StoredNotifier, func(context.Context) error, error) {
	temporalEnv, temporalShutdown, err := newTemporalClient(logger, meterProvider, temporalClientOptions{
		address:      c.String("temporal-address"),
		namespace:    c.String("temporal-namespace"),
		taskQueue:    c.String("temporal-task-queue"),
		certPEMBlock: []byte(c.String("temporal-client-cert")),
		keyPEMBlock:  []byte(c.String("temporal-client-key")),
	})
	if err != nil {
		return nil, noopShutdown, fmt.Errorf("create temporal client for transcript wakes: %w", err)
	}
	if temporalEnv == nil {
		logger.WarnContext(c.Context, "temporal is not configured; persisted transcript rows will not wake risk analysis, skill efficacy, or chat analysis",
			attr.SlogEvent("streams_transcript_wakes_disabled"),
		)
		return nil, noopShutdown, nil
	}

	writer, writerShutdown := chat.NewChatMessageWriter(logger, db, nil)

	auditLogger := newAuditLogger()

	writer.AddObserver(risk.NewObserver(
		logger,
		tracerProvider,
		db,
		background.NewThrottledSignaler(
			&background.TemporalRiskAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
			riskAnalysisSignalCooldown,
			logger.With(attr.SlogComponent("risk")),
		),
		auditLogger,
	))

	writer.AddObserver(efficacy.NewObserver(
		logger,
		background.NewThrottledSignaler(
			&background.TemporalSkillEfficacySignaler{TemporalEnv: temporalEnv, Logger: logger},
			background.SkillEfficacySignalCooldown,
			logger.With(attr.SlogComponent("skill-efficacy")),
		),
	))

	writer.AddObserver(analysis.NewObserver(
		logger,
		background.NewThrottledSignaler(
			&background.TemporalChatAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
			background.ChatAnalysisSignalCooldown,
			logger.With(attr.SlogComponent("chat-analysis")),
		),
	))

	shutdown := func(ctx context.Context) error {
		// Writer first: it owns the goroutines that call into the signalers, so
		// closing the Temporal connection under them would race.
		if err := writerShutdown(ctx); err != nil {
			return fmt.Errorf("shutdown transcript writer: %w", err)
		}
		if err := temporalShutdown(ctx); err != nil {
			return fmt.Errorf("shutdown transcript writer temporal client: %w", err)
		}
		return nil
	}
	return writer, shutdown, nil
}

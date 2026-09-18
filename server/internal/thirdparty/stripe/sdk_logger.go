package stripe

import (
	"context"
	"fmt"
	"log/slog"

	stripesdk "github.com/stripe/stripe-go/v85"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// sdkLogger keeps SDK diagnostics in the request's structured logging context.
// Request progress is debug-only; expected idempotent duplicates are not failures.
type sdkLogger struct {
	logger *slog.Logger
}

var _ stripesdk.ContextLeveledLoggerInterface = (*sdkLogger)(nil)

func (l *sdkLogger) Debugf(ctx context.Context, format string, values ...any) {
	if l.logger.Enabled(ctx, slog.LevelDebug) {
		l.logger.DebugContext(ctx, fmt.Sprintf(format, values...))
	}
}

func (l *sdkLogger) Infof(ctx context.Context, format string, values ...any) {
	// Stripe emits multiple info messages per request, including successful ones.
	if l.logger.Enabled(ctx, slog.LevelDebug) {
		l.logger.DebugContext(ctx, fmt.Sprintf(format, values...))
	}
}

func (l *sdkLogger) Warnf(ctx context.Context, format string, values ...any) {
	if l.logger.Enabled(ctx, slog.LevelWarn) {
		l.logger.WarnContext(ctx, fmt.Sprintf(format, values...))
	}
}

func (l *sdkLogger) Errorf(ctx context.Context, format string, values ...any) {
	var sdkErr error
	for _, value := range values {
		if err, ok := value.(error); ok {
			sdkErr = err
			break
		}
	}
	if sdkErr == nil {
		sdkErr = fmt.Errorf(format, values...)
	}

	failure := classifyV2MeterEventError(sdkErr)
	level := slog.LevelError
	if failure.Code == "duplicate_meter_event" {
		level = slog.LevelDebug
	}
	if !l.logger.Enabled(ctx, level) {
		return
	}
	l.logger.LogAttrs(ctx, level, "stripe SDK request error",
		attr.SlogError(sdkErr),
		attr.SlogErrorType(string(failure.Class)),
		attr.SlogStripeErrorCode(failure.Code),
		attr.SlogHTTPResponseStatusCode(failure.HTTPStatusCode),
	)
}

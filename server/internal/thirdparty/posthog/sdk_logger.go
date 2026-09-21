package posthog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/posthog/posthog-go"
)

// localEvaluationWarningPrefix is the format string the PostHog SDK warns with
// whenever a flag cannot be decided from the cached definitions. The SDK then
// falls back to remote evaluation, so the warning is expected for any flag that
// targets properties the caller did not pass and is not actionable per call.
const localEvaluationWarningPrefix = "Unable to compute flag locally"

// sdkLogger routes PostHog SDK diagnostics through the structured logger so
// they carry the component attribute and a level that log aggregation can
// read. The SDK logger interface carries no context, so entries are logged
// against context.Background() and never carry a request trace.
type sdkLogger struct {
	logger *slog.Logger
}

var _ posthog.Logger = (*sdkLogger)(nil)

func (l *sdkLogger) Debugf(format string, args ...any) {
	l.log(slog.LevelDebug, format, args...)
}

// Logf is used by the SDK for routine progress, including the full response
// body of every successful batch upload, so it is treated as debug output.
func (l *sdkLogger) Logf(format string, args ...any) {
	l.log(slog.LevelDebug, format, args...)
}

func (l *sdkLogger) Warnf(format string, args ...any) {
	level := slog.LevelWarn
	if strings.HasPrefix(format, localEvaluationWarningPrefix) {
		level = slog.LevelDebug
	}
	l.log(level, format, args...)
}

func (l *sdkLogger) Errorf(format string, args ...any) {
	l.log(slog.LevelError, format, args...)
}

func (l *sdkLogger) log(level slog.Level, format string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, level) {
		return
	}
	l.logger.Log(ctx, level, fmt.Sprintf(format, args...))
}

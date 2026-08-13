package oops

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// LogError logs any error at error level and records it on the current span. A
// client-initiated cancellation is demoted to info level and left off the span.
// It returns err unchanged for use in a return statement.
func LogError(ctx context.Context, logger *slog.Logger, err error, args ...slog.Attr) error {
	logErr(ctx, logger, err, slog.LevelError, true, args)
	return err
}

// LogWarn logs any error at warn level and never touches the span, keeping
// client-fault errors out of the errored-span population that error-rate
// monitors are keyed on. A client-initiated cancellation is demoted to info
// level. It returns err unchanged for use in a return statement.
func LogWarn(ctx context.Context, logger *slog.Logger, err error, args ...slog.Attr) error {
	logErr(ctx, logger, err, slog.LevelWarn, false, args)
	return err
}

// recordSpan controls whether a non-canceled error is recorded on the span.
//
//go:noinline
func logErr(ctx context.Context, logger *slog.Logger, err error, level slog.Level, recordSpan bool, args []slog.Attr) {
	if err == nil {
		return
	}

	msg := err.Error()
	detail := err.Error()
	canceled := errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled)

	attrs := make([]slog.Attr, 0, len(args)+2)
	attrs = append(attrs, args...)

	var shareable *ShareableError
	if errors.As(err, &shareable) {
		shareable.spanHandled = true
		canceled = shareable.effectiveCode(ctx) == CodeCanceled
		attrs = append(attrs, attr.SlogErrorID(shareable.id))

		// A wrapper's Error() already carries the public message plus whatever
		// context the wrapping added. The exact type check distinguishes that
		// wrapper from the ShareableError found above.
		if direct, ok := err.(*ShareableError); ok && direct == shareable { //nolint:errorlint // The outer error must be matched without unwrapping.
			msg = shareable.public
			detail = shareable.String()
		}
	}

	if canceled {
		level = slog.LevelInfo
	}

	if recordSpan && !canceled {
		span := trace.SpanFromContext(ctx)
		span.SetStatus(codes.Error, detail)
		span.RecordError(err, trace.WithStackTrace(true))
	}

	attrs = append(attrs, attr.SlogErrorMessage(detail))

	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip [Callers, logErr, LogError|LogWarn]
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.AddAttrs(attrs...)

	_ = logger.Handler().Handle(ctx, r)
}

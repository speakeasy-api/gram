package oops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"
)

// E creates a new ShareableError with the given code, cause, public message,
// and optional formatting arguments to interpolate into the public message. The
// cause can be nil if there is no underlying error to capture.
//
//go:noinline
func E(code Code, cause error, public string, args ...any) *ShareableError {
	msg := public
	if len(args) > 0 {
		msg = fmt.Sprintf(public, args...)
	}

	return &ShareableError{
		Code:    code,
		id:      goa.NewErrorID(),
		cause:   cause,
		private: "",
		public:  msg,
	}
}

// C creates a new ShareableError with the given code and a public message
// derived from the code. It is a convenience function to quickly create errors
// with canned messages.
//
//go:noinline
func C(code Code) *ShareableError {
	return &ShareableError{
		Code:    code,
		id:      goa.NewErrorID(),
		cause:   nil,
		private: "",
		public:  code.UserMessage(),
	}
}

// ShareableError is an error that can be shared with clients. It contains a
// public-facing message and an underlying cause that is not shared. This error
// type is designed to be used within service methods that are at the HTTP
// server boundary.
type ShareableError struct {
	Code    Code
	id      string
	cause   error
	public  string
	private string
}

// Error implements the error interface.
func (e *ShareableError) Error() string {
	return e.public
}

// String returns a detailed string representation of the error, including the
// private message and the cause, if any.
func (e *ShareableError) String() string {
	msg := e.private
	if msg == "" {
		msg = e.public
	}

	if e.cause == nil {
		return msg
	}

	return fmt.Sprintf("%s: %s", msg, e.cause.Error())
}

// Unwrap returns the underlying cause of the error, if any.
func (e *ShareableError) Unwrap() error {
	return e.cause
}

// MarshalJSON implements the json.Marshaler interface.
func (e *ShareableError) MarshalJSON() ([]byte, error) {
	bs, err := json.Marshal(e.public)
	if err != nil {
		return nil, fmt.Errorf("marshal shareable error: %w", err)
	}

	return bs, nil
}

// MarshalText implements the encoding.TextMarshaler interface.
func (e *ShareableError) MarshalText() (text []byte, err error) {
	return []byte(e.public), nil
}

// LogValue implements the slog.LogValuer interface.
func (e *ShareableError) LogValue() slog.Value {
	return slog.StringValue(e.Error())
}

// effectiveCode returns CodeCanceled only for a client-initiated cancellation:
// the underlying cause is context.Canceled AND the inbound request context
// (ctx) has itself been canceled. Otherwise it returns the authored Code.
//
// Requiring the request context to be canceled is what distinguishes a client
// disconnect from server- and application-initiated cancellations, which all
// surface the same context.Canceled sentinel:
//
//   - Client disconnect: net/http cancels the request context, so ctx.Err() is
//     context.Canceled. Promoted.
//   - Application-initiated (e.g. an errgroup or an explicitly cancelled child
//     of the request context): only the derived context is canceled; the
//     request context passed to the error boundary is still live, so
//     ctx.Err() is nil. Not promoted, keeps full error severity.
//   - Server-initiated (e.g. graceful Server.Shutdown): does not cancel
//     in-flight request contexts, so ctx.Err() is nil. Not promoted.
//
// This mirrors the access log middleware, which classifies a request as 499 on
// the same ctx.Err() signal. context.DeadlineExceeded and all other causes keep
// their authored code. The authored Code field is left untouched so call sites
// that branch on it still see what they constructed; only the logging, span,
// and HTTP status outputs observe the promotion.
func (e *ShareableError) effectiveCode(ctx context.Context) Code {
	if errors.Is(e.cause, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		return CodeCanceled
	}
	return e.Code
}

// LogError logs the error using the provided logger and context. It also sets the
// OpenTelemetry span status to error. Additional arguments can be provided to
// include more context in the log entry.
//
// A client-initiated cancellation (context.Canceled cause with a canceled
// request context) is not a server fault: it is logged at info level and does
// not mark the span as errored or record an exception, to avoid drowning real
// faults in disconnect noise. Server- and application-initiated cancellations,
// where the request context is still live, keep full error severity.
func (e *ShareableError) LogError(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	canceled := e.effectiveCode(ctx) == CodeCanceled

	span := trace.SpanFromContext(ctx)
	if !canceled {
		span.SetStatus(codes.Error, e.String())
		span.RecordError(e, trace.WithStackTrace(true))
	}

	level := slog.LevelError
	if canceled {
		level = slog.LevelInfo
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, Log]
	r := slog.NewRecord(time.Now(), level, e.public, pcs[0])

	if len(args) > 0 {
		r.AddAttrs(append(args, attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))...)
	} else {
		r.AddAttrs(attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))
	}

	_ = logger.Handler().Handle(ctx, r)
	return e
}

// LogWarn logs the error using the provided logger and context at warn level.
// Unlike LogError, it never sets the OpenTelemetry span status to error or
// records the error on the span. It is intended for client-fault boundary errors
// (e.g. 400/401/403/404) that should be visible in logs but must not pollute the
// errored-span population that error-rate monitors and SLOs are keyed on.
// Additional arguments can be provided to include more context in the log entry.
//
// A client-initiated cancellation (context.Canceled cause with a canceled
// request context) is demoted to info level, mirroring LogError's cancellation
// handling.
func (e *ShareableError) LogWarn(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	level := slog.LevelWarn
	if e.effectiveCode(ctx) == CodeCanceled {
		level = slog.LevelInfo
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, LogWarn]
	r := slog.NewRecord(time.Now(), level, e.public, pcs[0])

	if len(args) > 0 {
		r.AddAttrs(append(args, attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))...)
	} else {
		r.AddAttrs(attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))
	}

	_ = logger.Handler().Handle(ctx, r)
	return e
}

// LogInfo logs the error using the provided logger and context at info level.
// Like LogWarn, it never sets the OpenTelemetry span status to error or records
// the error on the span. Additional arguments can be provided to include more
// context in the log entry.
//
// The client-initiated cancellation check is a no-op at info level (the level is
// already info and the span is never touched) but is kept for symmetry with
// LogError and LogWarn.
func (e *ShareableError) LogInfo(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	level := slog.LevelInfo
	if e.effectiveCode(ctx) == CodeCanceled {
		level = slog.LevelInfo
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip [Callers, LogInfo]
	r := slog.NewRecord(time.Now(), level, e.public, pcs[0])

	if len(args) > 0 {
		r.AddAttrs(append(args, attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))...)
	} else {
		r.AddAttrs(attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))
	}

	_ = logger.Handler().Handle(ctx, r)
	return e
}

func (e *ShareableError) IsTemporary(ctx context.Context) bool {
	return !errors.Is(e.cause, ErrPermanent) && e.effectiveCode(ctx).IsTemporary()
}

// AsGoa converts the ShareableError to a goa.ServiceError, preserving the error
// code, id, and cause. It also sets the timeout, temporary, and fault flags
// based on the error code and cause. The context is used to detect a
// client-initiated cancellation (see effectiveCode).
func (e *ShareableError) AsGoa(ctx context.Context) *goa.ServiceError {
	var timeout, temporary, fault bool

	temporary = e.IsTemporary(ctx)

	code := e.effectiveCode(ctx)

	switch code {
	case CodeUnexpected, CodeInvariantViolation:
		fault = true
	default:
		fault = false
	}

	goaErr := goa.NewServiceError(e, string(code), timeout, temporary, fault)
	goaErr.ID = e.id
	return goaErr
}

// HTTPStatus returns the appropriate HTTP status code for the error based on
// its code. If the code is not recognized, it defaults to 500 Internal Server
// Error.
func (e *ShareableError) HTTPStatus(ctx context.Context) int {
	return conv.Default(StatusCodes[e.effectiveCode(ctx)], http.StatusInternalServerError)
}

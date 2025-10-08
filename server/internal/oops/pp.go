package oops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"
)

var funcMemo sync.Map

// E creates a new ShareableError with the given code, cause, public message,
// and optional formatting arguments to interpolate into the public message. The
// cause can be nil if there is no underlying error to capture.
//
//go:noinline
func E(code Code, cause error, public string, args ...any) *ShareableError {
	var pc [1]uintptr
	runtime.Callers(2, pc[:])

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
		pc:      pc[0],
	}
}

// C creates a new ShareableError with the given code and a public message
// derived from the code. It is a convenience function to quickly create errors
// with canned messages.
//
//go:noinline
func C(code Code) *ShareableError {
	var pc [1]uintptr
	runtime.Callers(2, pc[:])

	return &ShareableError{
		Code:    code,
		id:      goa.NewErrorID(),
		cause:   nil,
		private: "",
		public:  code.UserMessage(),
		pc:      pc[0],
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
	pc      uintptr
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
		return fmt.Sprintf("%s [%s]", msg, funcForPC(e.pc))
	}

	return fmt.Sprintf("%s: %s [%s]", msg, e.cause.Error(), funcForPC(e.pc))
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

// Log logs the error using the provided logger and context. It also sets the
// OpenTelemetry span status to error. Additional arguments can be provided to
// include more context in the log entry.
func (e *ShareableError) Log(ctx context.Context, logger *slog.Logger, args ...any) *ShareableError {
	trace.SpanFromContext(ctx).SetStatus(codes.Error, e.String())

	if len(args) > 0 {
		logger.ErrorContext(ctx, e.public, append(args, attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))...)
	} else {
		logger.ErrorContext(ctx, e.public, attr.SlogErrorID(e.id), attr.SlogErrorMessage(e.String()))
	}
	return e
}

func (e *ShareableError) IsTemporary() bool {
	return !errors.Is(e.cause, ErrPermanent) && e.Code.IsTemporary()
}

// AsGoa converts the ShareableError to a goa.ServiceError, preserving the error
// code, id, and cause. It also sets the timeout, temporary, and fault flags
// based on the error code and cause.
func (e *ShareableError) AsGoa() *goa.ServiceError {
	var timeout, temporary, fault bool

	temporary = e.IsTemporary()

	switch e.Code {
	case CodeUnexpected, CodeInvariantViolation:
		fault = true
	default:
		fault = false
	}

	goaErr := goa.NewServiceError(e, string(e.Code), timeout, temporary, fault)
	goaErr.ID = e.id
	return goaErr
}

// HTTPStatus returns the appropriate HTTP status code for the error based on
// its code. If the code is not recognized, it defaults to 500 Internal Server
// Error.
func (e *ShareableError) HTTPStatus() int {
	return conv.Default(StatusCodes[e.Code], http.StatusInternalServerError)
}

func funcForPC(pc uintptr) string {
	if f, ok := funcMemo.Load(pc); ok {
		val, ok := f.(string)
		if !ok {
			panic(fmt.Sprintf("funcForPC: expected string, got %T", f))
		}
		return val
	}

	fnc := runtime.FuncForPC(pc)
	if fnc == nil {
		funcMemo.Store(pc, "")
		return ""
	}

	file, line := fnc.FileLine(pc)
	loc := fmt.Sprintf("%s:%d", file, line)
	funcMemo.Store(pc, loc)
	return loc
}

package oops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"
)

var funcMemo sync.Map

//go:noinline
func E(code Code, cause error, public string) *ShareableError {
	var pc [1]uintptr
	runtime.Callers(2, pc[:])

	return &ShareableError{
		code:    code,
		cause:   cause,
		private: "",
		public:  public,
		pc:      pc[0],
	}
}

//go:noinline
func EE(code Code, cause error, public string, private string) *ShareableError {
	var pc [1]uintptr
	runtime.Callers(2, pc[:])

	return &ShareableError{
		code:    code,
		cause:   cause,
		public:  public,
		private: private,
		pc:      pc[0],
	}
}

//go:noinline
func C(code Code) *ShareableError {
	var pc [1]uintptr
	runtime.Callers(2, pc[:])

	return &ShareableError{
		code:    code,
		cause:   nil,
		private: "",
		public:  code.UserMessage(),
		pc:      pc[0],
	}
}

type ShareableError struct {
	code    Code
	cause   error
	public  string
	private string
	pc      uintptr
}

func (e *ShareableError) Error() string {
	return e.public
}

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

func (e *ShareableError) Unwrap() error {
	return e.cause
}

func (e *ShareableError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.public)
}

func (e *ShareableError) MarshalText() (text []byte, err error) {
	return []byte(e.public), nil
}

func (e *ShareableError) LogValue() slog.Value {
	return slog.StringValue(e.Error())
}

func (e *ShareableError) Log(ctx context.Context, logger *slog.Logger, args ...any) *ShareableError {
	trace.SpanFromContext(ctx).SetStatus(codes.Error, e.String())

	if len(args) > 0 {
		logger.ErrorContext(ctx, e.public, append(args, slog.String("error", e.String()))...)
	} else {
		logger.ErrorContext(ctx, e.public, slog.String("error", e.String()))
	}
	return e
}

func (e *ShareableError) AsGoa() *goa.ServiceError {
	var timeout, temporary, fault bool

	switch e.code {
	case CodeUnexpected, CodeInvariantViolation:
		fault = true
	default:
		timeout, temporary, fault = false, false, false
	}

	return goa.NewServiceError(e, string(e.code), timeout, temporary, fault)
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

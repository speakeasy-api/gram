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
)

var funcMemo sync.Map

// E captures the public and private message for an error.
//
//go:noinline
func E(cause error, public string, private string) *ShareableError {
	if cause == nil {
		return nil
	}

	var pc [1]uintptr
	runtime.Callers(2, pc[:])

	return &ShareableError{
		cause:   cause,
		public:  public,
		private: private,
		pc:      pc[0],
	}
}

type ShareableError struct {
	cause   error
	public  string
	private string
	pc      uintptr
}

func (e *ShareableError) Error() string {
	return e.public
}

func (e *ShareableError) String() string {
	return fmt.Sprintf("%s: %s [%s]", e.private, e.cause.Error(), funcForPC(e.pc))
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

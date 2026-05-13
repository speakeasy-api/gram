// Package oops is dev-idp's lightweight error helper. It mirrors enough of
// the surface from server/internal/oops for the service code to compile,
// minus the OTel hooks. Errors carry a Code (mapped to HTTP status) and a
// public message; .Log() emits via slog.
package oops

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	goa "goa.design/goa/v3/pkg"
)

type Code string

const (
	CodeBadRequest         Code = "bad_request"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeInvalid            Code = "invalid"
	CodeUnexpected         Code = "unexpected"
	CodeGatewayError       Code = "gateway_error"
	CodeInvariantViolation Code = "invariant_violation"
	CodeNotImplemented     Code = "not_implemented"
)

var statusCodes = map[Code]int{
	CodeBadRequest:         http.StatusBadRequest,
	CodeNotFound:           http.StatusNotFound,
	CodeConflict:           http.StatusConflict,
	CodeUnauthorized:       http.StatusUnauthorized,
	CodeForbidden:          http.StatusForbidden,
	CodeInvalid:            http.StatusUnprocessableEntity,
	CodeUnexpected:         http.StatusInternalServerError,
	CodeGatewayError:       http.StatusBadGateway,
	CodeInvariantViolation: http.StatusUnprocessableEntity,
	CodeNotImplemented:     http.StatusNotImplemented,
}

// ShareableError carries a public-facing message plus the cause. The
// cause is wrapped in the .Error() output so dev-idp's permissive error
// encoder can leak it to the browser.
type ShareableError struct {
	Code   Code
	id     string
	cause  error
	public string
}

func E(code Code, cause error, public string, args ...any) *ShareableError {
	msg := public
	if len(args) > 0 {
		msg = fmt.Sprintf(public, args...)
	}
	return &ShareableError{
		Code:   code,
		id:     goa.NewErrorID(),
		cause:  cause,
		public: msg,
	}
}

func (e *ShareableError) Error() string {
	if e.cause == nil {
		return e.public
	}
	return fmt.Sprintf("%s: %s", e.public, e.cause.Error())
}

func (e *ShareableError) Unwrap() error { return e.cause }

func (e *ShareableError) HTTPStatus() int {
	if s, ok := statusCodes[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// AsGoa converts to a goa.ServiceError using the code as the name. The
// .Error() output (public + cause) becomes the response message body.
func (e *ShareableError) AsGoa() *goa.ServiceError {
	fault := e.Code == CodeUnexpected || e.Code == CodeInvariantViolation
	se := goa.NewServiceError(e, string(e.Code), false, false, fault)
	se.ID = e.id
	return se
}

// Log emits a slog Error and returns the receiver for chaining.
func (e *ShareableError) Log(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	if logger == nil {
		return e
	}
	attrs := append([]slog.Attr{
		slog.String("error.id", e.id),
		slog.String("error.message", e.Error()),
		slog.String("error.code", string(e.Code)),
	}, args...)
	logger.LogAttrs(ctx, slog.LevelError, e.public, attrs...)
	return e
}

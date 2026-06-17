// Package oops is a minimal stub of the real
// github.com/speakeasy-api/gram/server/internal/oops package, used so the
// no-client-error-log-error analyzer fixtures resolve oops.E/oops.C and the
// ShareableError log methods by type.
package oops

import (
	"context"
	"log/slog"
)

type Code string

const (
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeBadRequest         Code = "bad_request"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeInvalid            Code = "invalid"
	CodeUnexpected         Code = "unexpected"
	CodeInvariantViolation Code = "invariant_violation"
)

func E(code Code, cause error, public string, args ...any) *ShareableError {
	return &ShareableError{}
}

func C(code Code) *ShareableError {
	return &ShareableError{}
}

type ShareableError struct{}

func (e *ShareableError) LogError(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	return e
}

func (e *ShareableError) LogWarn(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	return e
}

func (e *ShareableError) LogInfo(ctx context.Context, logger *slog.Logger, args ...slog.Attr) *ShareableError {
	return e
}

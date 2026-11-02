package oops

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OutageRetryAfter is the Retry-After on a 503, matching the 30s key set and refresh hold-backs.
const OutageRetryAfter = 30 * time.Second

// outageMessage answers a request a failed dependency stopped.
const outageMessage = "Speakeasy is temporarily unavailable. Retry shortly."

// pgClassData and pgClassIntegrity are SQLSTATE classes a bad statement earns, not an outage.
const (
	pgClassData      = "22"
	pgClassIntegrity = "23"
)

// IsInfrastructureFailure reports a store or network that couldn't answer; a missing row is an answer.
func IsInfrastructureFailure(err error) bool {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return !strings.HasPrefix(pgErr.Code, pgClassData) && !strings.HasPrefix(pgErr.Code, pgClassIntegrity)
	}
	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return true
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	return pgconn.Timeout(err) || pgconn.SafeToRetry(err)
}

// answerOutage turns a server fault caused by a failed dependency into a 503
// with Retry-After, and paces every other 503. Never tagged external: it's ours.
func answerOutage(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, err error) error {
	if _, refused := errors.AsType[*OAuthError](err); refused {
		return err
	}
	shareable, isShareable := errors.AsType[*ShareableError](err)
	if ctx.Err() == nil && (!isShareable || shareable.Code == CodeUnexpected) && IsInfrastructureFailure(err) {
		err = E(CodeUnavailable, err, "%s", outageMessage).LogError(ctx, logger)
		shareable, isShareable = errors.AsType[*ShareableError](err)
	}
	if isShareable && shareable.Code == CodeUnavailable && w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", strconv.Itoa(int(OutageRetryAfter.Seconds())))
	}
	return err
}

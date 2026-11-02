package oops

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
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

// transientSQLStates are the SQLSTATEs a retry can clear; anything else is a bug or a broken deploy.
var transientSQLStates = map[string]struct{}{
	"57P01": {}, // admin_shutdown
	"57P02": {}, // crash_shutdown
	"57P03": {}, // cannot_connect_now
	"40001": {}, // serialization_failure
	"40P01": {}, // deadlock_detected
}

// transientSQLStateClasses are SQLSTATE classes a retry can clear: connection exceptions and exhausted resources.
var transientSQLStateClasses = []string{"08", "53"}

// IsInfrastructureFailure reports a store or network that couldn't answer right now.
func IsInfrastructureFailure(err error) bool {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		if _, transient := transientSQLStates[pgErr.Code]; transient {
			return true
		}
		return slices.ContainsFunc(transientSQLStateClasses, func(class string) bool { return strings.HasPrefix(pgErr.Code, class) })
	}
	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return true
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	// Waiting on an exhausted pool ends in a deadline; the caller's own is ruled out by answerOutage.
	return errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) || pgconn.SafeToRetry(err)
}

// IsStoreFailure reports a query the store failed: an outage, or a broken statement or deploy.
func IsStoreFailure(err error) bool {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	_, pgFailure := errors.AsType[*pgconn.PgError](err)
	return pgFailure || IsInfrastructureFailure(err)
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

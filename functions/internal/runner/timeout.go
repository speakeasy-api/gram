package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/svc"
)

const (
	// defaultExecutionTimeout applies when a request names no timeout, which
	// is what a server built before per-organization timeouts sends.
	defaultExecutionTimeout = 5 * time.Minute

	// maxExecutionTimeout caps whatever a request asks for. The server
	// validates the organization's setting against the same ceiling; this is
	// the runner's own backstop.
	maxExecutionTimeout = 10 * time.Minute
)

// executionTimeout turns a request's timeout_seconds into the budget one
// invocation gets. Zero or negative means the request named none.
func executionTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultExecutionTimeout
	}
	if seconds >= int(maxExecutionTimeout/time.Second) {
		return maxExecutionTimeout
	}
	return time.Duration(seconds) * time.Second
}

// timeoutError reports an invocation that ran out of its own budget, as
// opposed to one whose caller went away. It returns nil in every other case so
// the caller keeps its original error.
func timeoutError(ctx, timeoutCtx context.Context, limit time.Duration) error {
	if ctx.Err() != nil || !errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return svc.NewTimeoutError(
		fmt.Errorf("function exceeded its time limit of %s", limit),
		http.StatusRequestTimeout,
	)
}

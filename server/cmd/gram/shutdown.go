package gram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func runShutdown(logger *slog.Logger, logCtx context.Context, shutdownFuncs []func(context.Context) error) error {
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(len(shutdownFuncs))

	done := make(chan struct{})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, shutdown := range shutdownFuncs {
		if shutdown == nil {
			wg.Done()
			continue
		}

		go func(shutdown func(context.Context) error) {
			defer wg.Done()
			if err := shutdown(ctx); err != nil {
				logger.ErrorContext(logCtx, "failed to shutdown component", attr.SlogError(err))
			}
		}(shutdown)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return errors.New("failed to shutdown all components")
	}

	logger.InfoContext(logCtx, "all components shutdown")
	return nil
}

// errShutdownDeadline is the cause given to a server's graceful shutdown
// deadline and to the requests still running when it passes.
var errShutdownDeadline = errors.New("graceful shutdown timed out")

// drainHTTPServer stops srv accepting connections and waits up to timeout for
// in-flight requests to finish. Requests still running at the deadline have
// their context cancelled through cancelRequests, which must cancel the
// context srv's BaseContext returns, and their connections closed, so neither
// a stalled stream nor a stalled client holds shutdown past the deadline.
// failureMsg is the error logged when the deadline passes.
func drainHTTPServer(ctx context.Context, logger *slog.Logger, srv *http.Server, cancelRequests context.CancelCauseFunc, timeout time.Duration, failureMsg string) {
	graceCtx, graceCancel := context.WithTimeoutCause(context.WithoutCancel(ctx), timeout, errShutdownDeadline)
	defer graceCancel()

	err := srv.Shutdown(graceCtx)
	if err == nil {
		return
	}
	if cause := context.Cause(graceCtx); cause != nil && !errors.Is(err, cause) {
		err = errors.Join(err, cause)
	}
	logger.ErrorContext(ctx, failureMsg, attr.SlogError(err))

	cancelRequests(errShutdownDeadline)
	if err := srv.Close(); err != nil {
		logger.ErrorContext(ctx, "close connections after graceful shutdown deadline", attr.SlogError(err))
	}
}

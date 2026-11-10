package gram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/httpstream"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// runShutdownTimeout is the budget runShutdown gives its funcs before returning, after
// which the command closes its datastore clients.
const runShutdownTimeout = 30 * time.Second

// connectionRecorderShutdownTimeout is the connection recorder's share of runShutdownTimeout.
// With mcpconnectionattempts.MaxShutdownOverrun it still ends before ClickHouse closes.
const connectionRecorderShutdownTimeout = 18 * time.Second

// connectionRecorderShutdown is the recorder's runShutdown func, bounded so its writer is
// joined before runShutdown returns.
func connectionRecorderShutdown(recorder *mcpconnectionattempts.Recorder) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, connectionRecorderShutdownTimeout)
		defer cancel()
		return recorder.ShutdownAndWait(ctx)
	}
}

func runShutdown(logger *slog.Logger, logCtx context.Context, shutdownFuncs []func(context.Context) error) error {
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(len(shutdownFuncs))

	done := make(chan struct{})

	ctx, cancel := context.WithTimeout(ctx, runShutdownTimeout)
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

// drainHTTPServer stops srv accepting connections and waits up to timeout for
// in-flight requests to finish. Requests still running at the deadline have
// their context cancelled through cancelRequests, which must cancel the
// context srv's BaseContext returns, and their connections closed, so neither
// a stalled stream nor a stalled client holds shutdown past the deadline.
// failureMsg is the error logged when the deadline passes.
func drainHTTPServer(ctx context.Context, logger *slog.Logger, srv *http.Server, cancelRequests context.CancelCauseFunc, timeout time.Duration, failureMsg string) {
	graceCtx, graceCancel := context.WithTimeoutCause(context.WithoutCancel(ctx), timeout, httpstream.ErrServerShutdown)
	defer graceCancel()

	err := srv.Shutdown(graceCtx)
	if err == nil {
		return
	}
	if cause := context.Cause(graceCtx); cause != nil && !errors.Is(err, cause) {
		err = errors.Join(err, cause)
	}
	logger.ErrorContext(ctx, failureMsg, attr.SlogError(err))

	// The cause lets long-lived responses record that the server, not their
	// client, ended them.
	cancelRequests(httpstream.ErrServerShutdown)
	if err := srv.Close(); err != nil {
		logger.ErrorContext(ctx, "close connections after graceful shutdown deadline", attr.SlogError(err))
	}
}

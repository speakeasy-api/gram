package gram

import (
	"context"
	"errors"
	"log/slog"
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

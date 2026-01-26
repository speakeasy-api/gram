package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	// DefaultLogWriterBufferSize is the default size of the log queue buffer.
	DefaultLogWriterBufferSize = 1000

	// DefaultLogWriterWorkers is the default number of worker goroutines.
	DefaultLogWriterWorkers = 5
)

// LogWriter manages a pool of workers that write telemetry logs to ClickHouse.
// It provides bounded concurrency and graceful shutdown capabilities.
type LogWriter struct {
	queue         chan LogParams
	done          chan struct{}
	closeOnce     sync.Once
	wg            sync.WaitGroup
	logger        *slog.Logger
	chRepo        *repo.Queries
	featureClient *productfeatures.Client
}

// LogWriterOptions configures the LogWriter.
type LogWriterOptions struct {
	BufferSize int
	Workers    int
}

// NewLogWriter creates a new LogWriter with the specified options.
// It starts worker goroutines that process logs from the queue.
func NewLogWriter(
	logger *slog.Logger,
	chRepo *repo.Queries,
	featureClient *productfeatures.Client,
	opts *LogWriterOptions,
) *LogWriter {
	if opts == nil {
		opts = &LogWriterOptions{
			BufferSize: 0,
			Workers:    0,
		}
	}

	bufferSize := opts.BufferSize
	if bufferSize <= 0 {
		bufferSize = DefaultLogWriterBufferSize
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = DefaultLogWriterWorkers
	}

	w := &LogWriter{
		queue:         make(chan LogParams, bufferSize),
		done:          make(chan struct{}),
		closeOnce:     sync.Once{},
		wg:            sync.WaitGroup{},
		logger:        logger.With(attr.SlogComponent("log-writer")),
		chRepo:        chRepo,
		featureClient: featureClient,
	}

	for i := 0; i < workers; i++ {
		w.wg.Add(1)
		go w.worker()
	}

	w.logger.InfoContext(context.Background(),
		fmt.Sprintf("log writer started with %d workers and buffer size %d", workers, bufferSize),
	)

	return w
}

// Enqueue adds a log to the processing queue.
// If the queue is full or the writer is shut down, the log is dropped and a warning is logged.
func (w *LogWriter) Enqueue(params LogParams) {
	select {
	case w.queue <- params:
		// Successfully enqueued
	case <-w.done:
		w.logger.WarnContext(context.Background(),
			"log writer is shut down, dropping log",
			attr.SlogResourceURN(params.ToolInfo.URN),
			attr.SlogProjectID(params.ToolInfo.ProjectID),
		)
	default:
		w.logger.WarnContext(context.Background(),
			"telemetry log queue full, dropping log",
			attr.SlogResourceURN(params.ToolInfo.URN),
			attr.SlogProjectID(params.ToolInfo.ProjectID),
		)
	}
}

// Shutdown gracefully stops the LogWriter by closing the queue and waiting
// for all workers to finish processing. It respects the context deadline.
func (w *LogWriter) Shutdown(ctx context.Context) error {
	w.logger.InfoContext(ctx, "shutting down log writer")

	// Signal shutdown and close the queue (protected from double-close)
	w.closeOnce.Do(func() {
		close(w.done)
		close(w.queue)
	})

	workersDone := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(workersDone)
	}()

	select {
	case <-workersDone:
		w.logger.InfoContext(ctx, "log writer shutdown complete")
		return nil
	case <-ctx.Done():
		w.logger.WarnContext(ctx, "log writer shutdown timed out, some logs may be lost")
		return fmt.Errorf("log writer shutdown: %w", ctx.Err())
	}
}

// worker processes logs from the queue until the channel is closed.
func (w *LogWriter) worker() {
	defer w.wg.Done()

	for params := range w.queue {
		w.processLog(params)
	}
}

// processLog checks the feature flag and writes the log to ClickHouse.
func (w *LogWriter) processLog(params LogParams) {
	ctx := context.Background()

	enabled, err := w.featureClient.IsFeatureEnabled(ctx, params.ToolInfo.OrganizationID, productfeatures.FeatureLogs)
	if err != nil {
		w.logger.ErrorContext(ctx,
			"failed to check logs feature flag",
			attr.SlogError(err),
			attr.SlogOrganizationID(params.ToolInfo.OrganizationID),
		)
		return
	}
	if !enabled {
		return
	}

	logParams, err := buildTelemetryLogParams(params)
	if err != nil {
		w.logger.ErrorContext(ctx,
			"failed to build telemetry log params",
			attr.SlogError(err),
		)
		return
	}

	if err := w.chRepo.InsertTelemetryLog(ctx, *logParams); err != nil {
		w.logger.ErrorContext(ctx,
			"failed to emit telemetry log to ClickHouse",
			attr.SlogError(err),
			attr.SlogResourceURN(logParams.GramURN),
		)
		return
	}
}

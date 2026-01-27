package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"golang.org/x/sync/errgroup"
)

const (
	// DefaultLogWriterBufferSize is the default size of the log queue buffer.
	DefaultLogWriterBufferSize = 1000

	// DefaultLogWriterWorkers is the default number of concurrent log writes.
	DefaultLogWriterWorkers = 5
)

// LogWriter manages concurrent writes of telemetry logs to ClickHouse.
// It uses a buffered channel for burst absorption and an errgroup to
// bound the number of concurrent workers processing the queue.
type LogWriter struct {
	queue   chan LogParams
	eg      *errgroup.Group
	closed  atomic.Bool
	logger  *slog.Logger
	repo    *repo.Queries
	enabled LogsEnabled
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
	chConn clickhouse.Conn,
	logsEnabled LogsEnabled,
	opts LogWriterOptions,
) *LogWriter {
	bufferSize := opts.BufferSize
	if bufferSize <= 0 {
		bufferSize = DefaultLogWriterBufferSize
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = DefaultLogWriterWorkers
	}

	eg := new(errgroup.Group)

	w := &LogWriter{
		queue:   make(chan LogParams, bufferSize),
		eg:      eg,
		closed:  atomic.Bool{},
		logger:  logger.With(attr.SlogComponent("log-writer")),
		repo:    repo.New(chConn),
		enabled: logsEnabled,
	}

	// Start workers that drain the queue
	for i := 0; i < workers; i++ {
		eg.Go(func() error {
			for params := range w.queue {
				w.processLog(params)
			}
			return nil
		})
	}

	w.logger.InfoContext(context.Background(), "log writer started")

	return w
}

// Enqueue schedules a log to be written to ClickHouse.
// This is non-blocking until the buffer is full, then blocks until space is available.
// If the writer is shut down, the log is dropped.
func (w *LogWriter) Enqueue(params LogParams) {
	if w.closed.Load() {
		w.logger.WarnContext(context.Background(),
			"log writer is shut down, dropping log",
			attr.SlogResourceURN(params.ToolInfo.URN),
			attr.SlogProjectID(params.ToolInfo.ProjectID),
		)
		return
	}

	w.queue <- params
}

// Shutdown gracefully stops the LogWriter by closing the queue and waiting
// for all workers to drain remaining logs. It respects the context deadline.
func (w *LogWriter) Shutdown(ctx context.Context) error {
	w.logger.InfoContext(ctx, "shutting down log writer")

	w.closed.Store(true)
	close(w.queue)

	done := make(chan error, 1)
	go func() {
		done <- w.eg.Wait()
	}()

	select {
	case <-done:
		w.logger.InfoContext(ctx, "log writer shutdown complete")
		return nil
	case <-ctx.Done():
		w.logger.WarnContext(ctx, "log writer shutdown timed out, some logs may be lost")
		return fmt.Errorf("log writer shutdown: %w", ctx.Err())
	}
}

// processLog checks the feature flag and writes the log to ClickHouse.
func (w *LogWriter) processLog(params LogParams) {
	ctx := context.Background()

	enabled, err := w.enabled(ctx, params.ToolInfo.OrganizationID)
	if err != nil || !enabled {
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

	if err := w.repo.InsertTelemetryLog(ctx, *logParams); err != nil {
		w.logger.ErrorContext(ctx,
			"failed to emit telemetry log to ClickHouse",
			attr.SlogError(err),
			attr.SlogResourceURN(logParams.GramURN),
		)
		return
	}
}

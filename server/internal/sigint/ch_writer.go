package sigint

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// ReadingInserter durably persists validated reading batches.
type ReadingInserter interface {
	InsertReadings(context.Context, []chrepo.ReadingRow) error
}

// ReadingCHWriter lands successful attempts without suppressing duplicate deliveries.
type ReadingCHWriter struct {
	logger   *slog.Logger
	inserter ReadingInserter
	now      func() time.Time
}

// NewReadingCHWriter creates the independent ClickHouse reading consumer.
func NewReadingCHWriter(logger *slog.Logger, inserter ReadingInserter) *ReadingCHWriter {
	return &ReadingCHWriter{logger: logger.With(attr.SlogComponent("sigint-reading-ch-writer")), inserter: inserter, now: time.Now}
}

var _ streams.BatchHandler[*sigintv1.Reading] = (*ReadingCHWriter)(nil)

// HandleBatch drops malformed messages and nacks storage failures for redelivery.
func (w *ReadingCHWriter) HandleBatch(ctx context.Context, messages []*sigintv1.Reading, _ []gcp.MessageMetadata) error {
	rows := make([]chrepo.ReadingRow, 0, len(messages))
	insertedAt := w.now().UTC()
	for _, message := range messages {
		row, err := chrepo.ParseReading(message, insertedAt)
		if err != nil {
			messageID := ""
			if message != nil {
				messageID = message.GetId()
			}
			w.logger.WarnContext(ctx, "dropping malformed sensor reading", attr.SlogMessageID(messageID), attr.SlogError(err))
			continue
		}
		rows = append(rows, row)
	}
	if err := w.inserter.InsertReadings(ctx, rows); err != nil {
		return fmt.Errorf("persist sensor readings: %w", err)
	}
	return nil
}

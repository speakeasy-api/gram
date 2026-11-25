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

// attemptMessage is a sensor evaluation attempt landed in sigint_readings_v2.
// Getters are nil-safe on generated protobuf messages.
type attemptMessage interface {
	GetId() string
}

// CHWriter lands evaluation attempts in sigint_readings_v2 without suppressing
// duplicate deliveries. Readings and outcomes share the table and its
// replacement key, so the latest attempt wins across both.
type CHWriter[M attemptMessage] struct {
	logger   *slog.Logger
	inserter ReadingInserter
	parse    func(M, time.Time) (chrepo.ReadingRow, error)
	now      func() time.Time
}

// NewReadingCHWriter creates the independent ClickHouse reading consumer.
func NewReadingCHWriter(logger *slog.Logger, inserter ReadingInserter) *CHWriter[*sigintv1.Reading] {
	return &CHWriter[*sigintv1.Reading]{logger: logger.With(attr.SlogComponent("sigint-reading-ch-writer")), inserter: inserter, parse: chrepo.ParseReading, now: time.Now}
}

// NewOutcomeCHWriter creates the ClickHouse consumer for sensor decisions without an answer.
func NewOutcomeCHWriter(logger *slog.Logger, inserter ReadingInserter) *CHWriter[*sigintv1.Outcome] {
	return &CHWriter[*sigintv1.Outcome]{logger: logger.With(attr.SlogComponent("sigint-outcome-ch-writer")), inserter: inserter, parse: chrepo.ParseOutcome, now: time.Now}
}

var (
	_ streams.BatchHandler[*sigintv1.Reading] = (*CHWriter[*sigintv1.Reading])(nil)
	_ streams.BatchHandler[*sigintv1.Outcome] = (*CHWriter[*sigintv1.Outcome])(nil)
)

// HandleBatch drops malformed messages and nacks storage failures for redelivery.
func (w *CHWriter[M]) HandleBatch(ctx context.Context, messages []M, _ []gcp.MessageMetadata) error {
	rows := make([]chrepo.ReadingRow, 0, len(messages))
	insertedAt := w.now().UTC()
	for _, message := range messages {
		row, err := w.parse(message, insertedAt)
		if err != nil {
			w.logger.WarnContext(ctx, "dropping malformed sensor attempt", attr.SlogMessageID(message.GetId()), attr.SlogError(err))
			continue
		}
		rows = append(rows, row)
	}
	if err := w.inserter.InsertReadings(ctx, rows); err != nil {
		return fmt.Errorf("persist sensor attempts: %w", err)
	}
	return nil
}

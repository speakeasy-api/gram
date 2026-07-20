package riskfindings

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestSinkDryRunTracksCommitCursor exercises the sink lifecycle without a real
// ClickHouse connection: in dry-run it counts rows and advances LastCommitted to
// the last row of the last flushed batch, which is the resume watermark.
func TestSinkDryRunTracksCommitCursor(t *testing.T) {
	t.Parallel()

	const batchSize = 2
	sink := NewSink(nil, 4, batchSize, true)

	done := make(chan error, 1)
	go func() { done <- sink.Run(t.Context()) }()

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	in := sink.Input()
	for _, id := range ids {
		in <- FindingRow{ID: id}
	}
	close(in)

	require.NoError(t, <-done)
	require.Equal(t, int64(len(ids)), sink.Inserted())
	require.Equal(t, ids[len(ids)-1], sink.LastCommitted())
}

// TestSinkEmptyCommitsNothing checks that a sink that never receives a row does
// not advance the commit cursor.
func TestSinkEmptyCommitsNothing(t *testing.T) {
	t.Parallel()

	sink := NewSink(nil, 1, 2, true)

	done := make(chan error, 1)
	go func() { done <- sink.Run(t.Context()) }()
	close(sink.Input())

	require.NoError(t, <-done)
	require.Equal(t, int64(0), sink.Inserted())
	require.Equal(t, uuid.Nil, sink.LastCommitted())
}

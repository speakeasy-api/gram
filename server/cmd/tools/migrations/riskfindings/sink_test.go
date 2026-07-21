package riskfindings

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestSinkDryRunCountsButExposesNoCursor exercises the sink lifecycle without a
// real ClickHouse connection: dry-run drains and counts every row across batches
// but must NOT expose a commit cursor, since nothing was durably written.
func TestSinkDryRunCountsButExposesNoCursor(t *testing.T) {
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
	require.Equal(t, uuid.Nil, sink.LastCommitted())
}

// TestDeduplicationTokenDistinguishesInteriors guards the property the token
// exists for: two batches sharing the same first and last id but differing in
// the interior must get different tokens, so a Replicated engine does not drop
// the second as a false duplicate. Identical batches must collide; order matters.
func TestDeduplicationTokenDistinguishesInteriors(t *testing.T) {
	t.Parallel()

	first, mid1, mid2, last := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	batchA := []FindingRow{{ID: first}, {ID: mid1}, {ID: last}}
	batchB := []FindingRow{{ID: first}, {ID: mid2}, {ID: last}} // same endpoints, different middle
	batchReordered := []FindingRow{{ID: last}, {ID: mid1}, {ID: first}}

	require.NotEqual(t, deduplicationToken(batchA), deduplicationToken(batchB))
	require.NotEqual(t, deduplicationToken(batchA), deduplicationToken(batchReordered))
	require.Equal(t, deduplicationToken(batchA), deduplicationToken([]FindingRow{{ID: first}, {ID: mid1}, {ID: last}}))
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

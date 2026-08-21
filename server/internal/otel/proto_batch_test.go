package otel

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRightSizeProtoBatchesUsesLargestFittingBatches(t *testing.T) {
	t.Parallel()

	items := []int{
		constants.MiB,
		constants.MiB,
		constants.MiB,
		constants.MiB,
		constants.MiB,
	}
	build := buildBytesValueBatch

	batches, err := rightSizeProtoBatches(items, 4*constants.MiB, build)

	require.NoError(t, err)
	require.Len(t, batches, 2)
	require.Len(t, batches[0].items, 3)
	require.Len(t, batches[1].items, 2)
	delivered := 0
	for _, batch := range batches {
		require.Equal(t, proto.Size(batch.message), batch.size)
		require.LessOrEqual(t, batch.size, 4*constants.MiB)
		delivered += len(batch.items)
	}
	require.Equal(t, len(items), delivered)
}

func TestRightSizeProtoBatchesKeepsFittingInputInOneBatch(t *testing.T) {
	t.Parallel()

	items := []int{constants.MiB, constants.MiB}
	buildCalls := 0
	build := func(sizes []int) (*wrapperspb.BytesValue, error) {
		buildCalls++
		return buildBytesValueBatch(sizes)
	}

	batches, err := rightSizeProtoBatches(items, 4*constants.MiB, build)

	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.Equal(t, items, batches[0].items)
	require.Equal(t, proto.Size(batches[0].message), batches[0].size)
	require.LessOrEqual(t, batches[0].size, 4*constants.MiB)
	require.Equal(t, 1, buildCalls)
}

func TestRightSizeProtoBatchesRejectsSingleOversizedItem(t *testing.T) {
	t.Parallel()

	build := buildBytesValueBatch

	batches, err := rightSizeProtoBatches([]int{4 * constants.MiB}, 4*constants.MiB, build)

	require.ErrorIs(t, err, errProtoBatchItemTooLarge)
	require.Nil(t, batches)
}

func TestRightSizeProtoBatchesReturnsBuilderError(t *testing.T) {
	t.Parallel()

	builderErr := errors.New("build failed")
	batches, err := rightSizeProtoBatches([]int{1, 2}, 4*constants.MiB, func([]int) (*wrapperspb.BytesValue, error) {
		return nil, builderErr
	})

	require.ErrorIs(t, err, builderErr)
	require.Nil(t, batches)
}

func buildBytesValueBatch(sizes []int) (*wrapperspb.BytesValue, error) {
	total := 0
	for _, size := range sizes {
		total += size
	}
	return wrapperspb.Bytes(make([]byte, total)), nil
}

package otel

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
)

var errProtoBatchItemTooLarge = errors.New("single protobuf batch item exceeds size limit")

type rightSizedProtoBatch[T any, M proto.Message] struct {
	items   []T
	message M
	size    int
}

// rightSizeProtoBatches partitions items into the largest ordered, contiguous
// protobuf batches whose encoded size does not exceed maxBytes. The build
// function may be called repeatedly for overlapping slices, so it must be
// deterministic and free of side effects. Returned item slices alias items.
// An error is returned if building fails or one item cannot fit by itself.
func rightSizeProtoBatches[T any, M proto.Message](
	items []T,
	maxBytes int,
	build func([]T) (M, error),
) ([]rightSizedProtoBatch[T, M], error) {
	if len(items) == 0 {
		return nil, nil
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("protobuf batch size limit must be positive: %d", maxBytes)
	}

	batches := make([]rightSizedProtoBatch[T, M], 0, 1)
	remaining := items
	for len(remaining) > 0 {
		message, err := build(remaining)
		if err != nil {
			return nil, fmt.Errorf("build protobuf batch of %d items: %w", len(remaining), err)
		}
		size := proto.Size(message)
		if size <= maxBytes {
			batches = append(batches, rightSizedProtoBatch[T, M]{
				items:   remaining,
				message: message,
				size:    size,
			})
			break
		}
		if len(remaining) == 1 {
			return nil, fmt.Errorf("%w: encoded size %d, limit %d", errProtoBatchItemTooLarge, size, maxBytes)
		}

		count, batchMessage, batchSize, err := largestFittingBatch(remaining, maxBytes, build)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, fmt.Errorf("%w: limit %d", errProtoBatchItemTooLarge, maxBytes)
		}
		batches = append(batches, rightSizedProtoBatch[T, M]{
			items:   remaining[:count],
			message: batchMessage,
			size:    batchSize,
		})
		remaining = remaining[count:]
	}

	return batches, nil
}

// largestFittingBatch binary-searches candidate batch lengths and returns the
// largest batch, taken from the beginning of items, whose built protobuf fits
// within maxBytes. The caller has already established that the full slice is
// oversized. A zero count means even the first item does not fit.
func largestFittingBatch[T any, M proto.Message](
	items []T,
	maxBytes int,
	build func([]T) (M, error),
) (count int, message M, size int, err error) {
	low, high := 1, len(items)-1
	for low <= high {
		middle := low + (high-low)/2
		candidate, buildErr := build(items[:middle])
		if buildErr != nil {
			return 0, message, 0, fmt.Errorf("build protobuf batch from first %d items: %w", middle, buildErr)
		}
		candidateSize := proto.Size(candidate)
		if candidateSize <= maxBytes {
			count = middle
			message = candidate
			size = candidateSize
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return count, message, size, nil
}

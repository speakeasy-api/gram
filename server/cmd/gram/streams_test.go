package gram

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingPublisherStopper struct {
	started *atomic.Int64
	release <-chan struct{}
	err     error
}

func (b *blockingPublisherStopper) Stop(ctx context.Context) error {
	b.started.Add(1)
	select {
	case <-b.release:
		return b.err
	case <-ctx.Done():
		return fmt.Errorf("wait to release publisher stop: %w", ctx.Err())
	}
}

func TestShutdownPubSubPublishersStopsAllPublishersBeforeClosingClient(t *testing.T) {
	t.Parallel()

	findingsErr := errors.New("stop findings publisher")
	spansErr := errors.New("stop spans publisher")
	closeErr := errors.New("close pubsub client")
	started := new(atomic.Int64)
	release := make(chan struct{})
	findingsPub := &blockingPublisherStopper{started: started, release: release, err: findingsErr}
	spanPub := &blockingPublisherStopper{started: started, release: release, err: spansErr}
	var clientClosed atomic.Bool
	done := make(chan error, 1)

	go func() {
		done <- shutdownPubSubPublishers(t.Context(), func(context.Context) error {
			clientClosed.Store(true)
			return closeErr
		}, findingsPub, nil, spanPub)
	}()

	require.Eventually(t, func() bool {
		return started.Load() == 2
	}, time.Second, time.Millisecond)
	require.False(t, clientClosed.Load())

	close(release)
	err := <-done

	require.ErrorIs(t, err, findingsErr)
	require.ErrorIs(t, err, spansErr)
	require.ErrorIs(t, err, closeErr)
	require.True(t, clientClosed.Load())
}

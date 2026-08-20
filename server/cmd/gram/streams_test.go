package gram

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingPublisherStopper struct {
	name  string
	calls *[]string
	err   error
}

func (r *recordingPublisherStopper) Stop(context.Context) error {
	*r.calls = append(*r.calls, r.name)
	return r.err
}

func TestShutdownPubSubPublishersStopsAllPublishersBeforeClosingClient(t *testing.T) {
	t.Parallel()

	findingsErr := errors.New("stop findings publisher")
	spansErr := errors.New("stop spans publisher")
	closeErr := errors.New("close pubsub client")
	calls := make([]string, 0, 3)
	findingsPub := &recordingPublisherStopper{name: "findings", calls: &calls, err: findingsErr}
	spanPub := &recordingPublisherStopper{name: "spans", calls: &calls, err: spansErr}

	err := shutdownPubSubPublishers(t.Context(), func(context.Context) error {
		calls = append(calls, "client")
		return closeErr
	}, findingsPub, nil, spanPub)

	require.ErrorIs(t, err, findingsErr)
	require.ErrorIs(t, err, spansErr)
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, []string{"findings", "spans", "client"}, calls)
}

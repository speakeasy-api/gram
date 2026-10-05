package mcpoutbound_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/mcpoutbound"
)

type inboundKey struct{}

func TestDetachContextDropsValuesAndKeepsSpan(t *testing.T) {
	t.Parallel()

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	parent := trace.ContextWithSpanContext(context.WithValue(t.Context(), inboundKey{}, "2026-07-28"), spanContext)

	detached, cancel := mcpoutbound.DetachContext(parent)
	defer cancel()

	require.Nil(t, detached.Value(inboundKey{}))
	require.Equal(t, spanContext, trace.SpanContextFromContext(detached))
}

func TestDetachContextFollowsParentCancellation(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(t.Context())
	detached, cancel := mcpoutbound.DetachContext(parent)
	defer cancel()

	cancelParent()
	<-detached.Done()
	require.ErrorIs(t, detached.Err(), context.Canceled)
}

func TestDetachContextKeepsParentDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		parent, cancelParent := context.WithTimeout(t.Context(), time.Second)
		defer cancelParent()
		detached, cancel := mcpoutbound.DetachContext(parent)
		defer cancel()

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		parentDeadline, _ := parent.Deadline()
		require.Equal(t, parentDeadline, deadline)

		<-detached.Done()
		require.ErrorIs(t, detached.Err(), context.DeadlineExceeded)
	})
}

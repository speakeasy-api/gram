package mcp

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
	"github.com/stretchr/testify/require"
)

type codeShutdownExecutor struct {
	codemode.Disabled
	stopped bool
	onStop  func(context.Context)
}

func (c *codeShutdownExecutor) Shutdown(ctx context.Context) error {
	c.stopped = true
	if c.onStop != nil {
		c.onStop(ctx)
	}
	return nil
}

func TestCodeRuntimeParticipatesInServiceShutdown(t *testing.T) {
	t.Parallel()
	runtime := &codeShutdownExecutor{}
	service := &Service{codeExecutor: runtime, autoVerifications: newAutoVerifications(), remoteSessionRecheck: &remoteSessionRecheck{stop: make(chan struct{})}, toolCallLogger: &otelpub.Logger{}}
	require.NoError(t, service.Shutdown(t.Context()))
	require.True(t, runtime.stopped, "all server entrypoints drain through Service.Shutdown")
}

func TestCodeRuntimeDrainsBeforeBlockedVerification(t *testing.T) {
	t.Parallel()
	runtimeStopped := make(chan struct{})
	runtime := &codeShutdownExecutor{onStop: func(ctx context.Context) {
		require.NoError(t, ctx.Err(), "probe draining must not exhaust the runtime's deadline")
		close(runtimeStopped)
	}}
	service := &Service{codeExecutor: runtime, autoVerifications: newAutoVerifications(), remoteSessionRecheck: &remoteSessionRecheck{stop: make(chan struct{})}, toolCallLogger: &otelpub.Logger{}}
	verificationDone := make(chan struct{})
	defer close(verificationDone)
	require.True(t, service.autoVerifications.admit(func() { <-verificationDone }))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Shutdown(ctx) }()
	select {
	case <-runtimeStopped:
	case <-t.Context().Done():
		t.Fatal("runtime did not drain")
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

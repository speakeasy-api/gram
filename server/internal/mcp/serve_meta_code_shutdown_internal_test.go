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
}

func (c *codeShutdownExecutor) Shutdown(context.Context) error { c.stopped = true; return nil }

func TestCodeRuntimeParticipatesInServiceShutdown(t *testing.T) {
	t.Parallel()
	runtime := &codeShutdownExecutor{}
	service := &Service{codeExecutor: runtime, autoVerifications: newAutoVerifications(), remoteSessionRecheck: &remoteSessionRecheck{stop: make(chan struct{})}, toolCallLogger: &otelpub.Logger{}}
	require.NoError(t, service.Shutdown(t.Context()))
	require.True(t, runtime.stopped, "all server entrypoints drain through Service.Shutdown")
}

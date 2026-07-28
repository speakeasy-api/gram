package tunneledmcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
)

// failingRuntimeStore errors on every read — the shape of a Redis outage.
type failingRuntimeStore struct {
	err error
}

func (f *failingRuntimeStore) Publish(context.Context, string, string, time.Duration) error {
	return f.err
}
func (f *failingRuntimeStore) Candidates(context.Context, string) ([]string, error) {
	return nil, f.err
}
func (f *failingRuntimeStore) Unpublish(context.Context, string, string) error { return f.err }
func (f *failingRuntimeStore) Delete(context.Context, string) error            { return f.err }
func (f *failingRuntimeStore) PublishConnections(context.Context, string, string, []route.Connection, time.Duration) error {
	return f.err
}
func (f *failingRuntimeStore) Connections(context.Context, string) ([]route.Connection, error) {
	return nil, f.err
}
func (f *failingRuntimeStore) DeleteConnectionOwner(context.Context, string, string) error {
	return f.err
}
func (f *failingRuntimeStore) DeleteConnections(context.Context, string) error { return f.err }

var _ route.RuntimeStore = (*failingRuntimeStore)(nil)

// TestConnectionsForServerLogsRuntimeErrors: the management view degrades to
// "no live connections" when the runtime store fails, but the failure must be
// logged — otherwise a Redis outage is indistinguishable from every agent
// being disconnected.
func TestConnectionsForServerLogsRuntimeErrors(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	m := newTunnelManager(&failingRuntimeStore{err: errors.New("redis unavailable")})

	connections := m.connectionsForServer(t.Context(), logger, uuid.New())

	require.Nil(t, connections)
	require.Contains(t, logs.String(), "load tunneled mcp connection cache")
	require.Contains(t, logs.String(), "redis unavailable")
	require.Equal(t, 1, strings.Count(logs.String(), "load tunneled mcp connection cache"))
}

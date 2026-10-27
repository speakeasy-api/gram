package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// countingClientInfoStore counts reads so tests can tell a cache hit from a
// trip to the shared store.
type countingClientInfoStore struct {
	*fakeClientInfoStore
	loads int
	err   error
}

func (s *countingClientInfoStore) Load(ctx context.Context, projectID uuid.UUID, scope, sessionID string, nowMillis int64) (sessionclientinfo.Info, error) {
	s.loads++
	if s.err != nil {
		return sessionclientinfo.Info{}, s.err
	}
	return s.fakeClientInfoStore.Load(ctx, projectID, scope, sessionID, nowMillis)
}

func reportedInitializeParams(name string) *mcp.InitializeParams {
	return &mcp.InitializeParams{
		Capabilities:    nil,
		ClientInfo:      &mcp.Implementation{Name: name, Version: "1.0", Title: "", Description: "", WebsiteURL: "", Icons: nil},
		Meta:            nil,
		ProtocolVersion: "2025-06-18",
	}
}

func TestTunnelSessionClientsShareTheRecordAcrossReplicas(t *testing.T) {
	t.Parallel()

	store := &countingClientInfoStore{fakeClientInfoStore: newFakeClientInfoStore(), loads: 0, err: nil}
	logger := testenv.NewLogger(t)
	projectID, serverID := uuid.New(), uuid.New()

	initializing := newTunnelClientFamilies(store).forServer(logger, projectID, serverID)
	initializing.RecordInitialize(t.Context(), "session-1", reportedInitializeParams("claude-code"))
	require.Equal(t, "claude", initializing.ClientFamily(t.Context(), "session-1"))
	require.Zero(t, store.loads, "the replica that saw initialize answers from memory")

	other := newTunnelClientFamilies(store).forServer(logger, projectID, serverID)
	require.Equal(t, "claude", other.ClientFamily(t.Context(), "session-1"), "another replica reads the shared record")
	require.Equal(t, "claude", other.ClientFamily(t.Context(), "session-1"))
	require.Equal(t, 1, store.loads, "a session costs one shared read per replica")

	info, err := store.Load(t.Context(), projectID, "tunnel:"+serverID.String(), "session-1", 0)
	require.NoError(t, err)
	require.Equal(t, sessionclientinfo.Info{Name: "claude-code", Version: "1.0", ProtocolVersion: "2025-06-18"}, info)

	foreign := newTunnelClientFamilies(store).forServer(logger, projectID, uuid.New())
	require.Empty(t, foreign.ClientFamily(t.Context(), "session-1"), "session ids are client-supplied, so records are scoped per server")
}

func TestTunnelSessionClientsClassifyReportedNames(t *testing.T) {
	t.Parallel()

	store := &countingClientInfoStore{fakeClientInfoStore: newFakeClientInfoStore(), loads: 0, err: nil}
	clients := newTunnelClientFamilies(store).forServer(testenv.NewLogger(t), uuid.New(), uuid.New())

	clients.RecordInitialize(t.Context(), "vscode", reportedInitializeParams("Visual Studio Code"))
	require.Equal(t, "vscode", clients.ClientFamily(t.Context(), "vscode"))

	clients.RecordInitialize(t.Context(), "windsurf", reportedInitializeParams("windsurf-client"))
	require.Equal(t, "other", clients.ClientFamily(t.Context(), "windsurf"))

	clients.RecordInitialize(t.Context(), "nameless", reportedInitializeParams(""))
	require.Empty(t, clients.ClientFamily(t.Context(), "nameless"), "no reported name leaves the User-Agent to decide")
}

func TestTunnelSessionClientsCacheMissesButNotFailures(t *testing.T) {
	t.Parallel()

	store := &countingClientInfoStore{fakeClientInfoStore: newFakeClientInfoStore(), loads: 0, err: nil}
	families := newTunnelClientFamilies(store)
	clients := families.forServer(testenv.NewLogger(t), uuid.New(), uuid.New())

	require.Empty(t, clients.ClientFamily(t.Context(), "unknown"))
	require.Empty(t, clients.ClientFamily(t.Context(), "unknown"))
	require.Equal(t, 1, store.loads, "an unknown session is looked up once")

	store.err = errors.New("redis unavailable")
	require.Empty(t, clients.ClientFamily(t.Context(), "failing"))
	require.Empty(t, clients.ClientFamily(t.Context(), "failing"))
	require.Equal(t, 3, store.loads, "a failed read is retried rather than remembered")
}

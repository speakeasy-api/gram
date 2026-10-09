package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/environments"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// fakeEnvironmentHeaderSource returns the snapshot a concurrent writer left
// behind after the request read its server row.
type fakeEnvironmentHeaderSource struct {
	snapshot environments.MCPServerHeaderSnapshot
	err      error
	calls    int
}

func (f *fakeEnvironmentHeaderSource) InspectMCPServerHeaders(context.Context, uuid.UUID, uuid.UUID) (environments.MCPServerHeaderSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

const (
	authorizedURL  = "https://approved.example.invalid/mcp"
	syntheticValue = "synthetic-env-value"
)

func linked(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

func mappedHeaders() []proxy.EnvironmentHeaderInspection {
	return proxy.InspectEnvironmentHeaders([]proxy.EnvironmentHeaderEntry{{Name: "MCP_HEADER_X-Instance-Url", Value: syntheticValue, Undecryptable: false}})
}

// authorizedRemote is the server row a request was authorized against: remote
// source A, linked to environment E.
func authorizedRemote() (*mcpserversrepo.McpServer, environments.MCPServerHeaderSnapshot) {
	env, remote := uuid.New(), uuid.New()
	server := &mcpserversrepo.McpServer{ID: uuid.New(), EnvironmentID: linked(env), RemoteMcpServerID: linked(remote)}
	return server, environments.MCPServerHeaderSnapshot{
		EnvironmentID:       linked(env),
		EnvironmentLive:     true,
		RemoteMcpServerID:   linked(remote),
		TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteURL:           authorizedURL,
		Headers:             mappedHeaders(),
	}
}

func TestReadEnvironmentHeadersSendsAMatchingSnapshot(t *testing.T) {
	t.Parallel()

	server, snapshot := authorizedRemote()
	got, err := readEnvironmentHeaders(t.Context(), &fakeEnvironmentHeaderSource{snapshot: snapshot, err: nil, calls: 0}, uuid.New(), server, authorizedURL)
	require.NoError(t, err)
	require.Equal(t, []proxy.ConfiguredHeader{{IsRequired: true, Name: "X-Instance-Url", StaticValue: syntheticValue, ValueFromRequestHeader: ""}}, got.rows)
}

func TestReadEnvironmentHeadersSkipsUnlinkedServers(t *testing.T) {
	t.Parallel()

	source := &fakeEnvironmentHeaderSource{snapshot: environments.MCPServerHeaderSnapshot{}, err: nil, calls: 0}
	got, err := readEnvironmentHeaders(t.Context(), source, uuid.New(), &mcpserversrepo.McpServer{ID: uuid.New()}, authorizedURL)
	require.NoError(t, err)
	require.Empty(t, got.rows)
	require.Zero(t, source.calls)
}

// Each case is a configuration a concurrent writer committed after the
// request read its server row. None may combine the request's destination
// with headers from a configuration that never pointed at it.
func TestReadEnvironmentHeadersRefusesAChangedConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string]func(server *mcpserversrepo.McpServer, snapshot *environments.MCPServerHeaderSnapshot){
		// The request authorized remote A with no link; the server was then
		// repointed at remote B and linked to E in one update.
		"repointed and linked": func(server *mcpserversrepo.McpServer, snapshot *environments.MCPServerHeaderSnapshot) {
			snapshot.RemoteMcpServerID = linked(uuid.New())
		},
		// The request authorized a link to E; E was unlinked and the source
		// URL then moved by a writer who needed no environment authority.
		"unlinked then url changed": func(server *mcpserversrepo.McpServer, snapshot *environments.MCPServerHeaderSnapshot) {
			snapshot.EnvironmentID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
			snapshot.EnvironmentLive = false
			snapshot.Headers = nil
			snapshot.RemoteURL = "https://attacker.example.invalid/mcp"
		},
		"url changed": func(server *mcpserversrepo.McpServer, snapshot *environments.MCPServerHeaderSnapshot) {
			snapshot.RemoteURL = "https://elsewhere.example.invalid/mcp"
		},
		"relinked": func(server *mcpserversrepo.McpServer, snapshot *environments.MCPServerHeaderSnapshot) {
			snapshot.EnvironmentID = linked(uuid.New())
		},
		"moved to a tunnel": func(server *mcpserversrepo.McpServer, snapshot *environments.MCPServerHeaderSnapshot) {
			snapshot.RemoteMcpServerID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
			snapshot.TunneledMcpServerID = linked(uuid.New())
			snapshot.RemoteURL = ""
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server, snapshot := authorizedRemote()
			change(server, &snapshot)
			got, err := readEnvironmentHeaders(t.Context(), &fakeEnvironmentHeaderSource{snapshot: snapshot, err: nil, calls: 0}, uuid.New(), server, authorizedURL)
			require.ErrorIs(t, err, errServingConfigurationChanged)
			require.Empty(t, got.rows)
		})
	}
}

func TestReadEnvironmentHeadersRefusesADeletedServer(t *testing.T) {
	t.Parallel()

	server, _ := authorizedRemote()
	_, err := readEnvironmentHeaders(t.Context(), &fakeEnvironmentHeaderSource{snapshot: environments.MCPServerHeaderSnapshot{}, err: environments.ErrMCPServerUnavailable, calls: 0}, uuid.New(), server, authorizedURL)
	require.ErrorIs(t, err, errServingConfigurationChanged)
}

func TestReadEnvironmentHeadersRefusesAnUnavailableEnvironment(t *testing.T) {
	t.Parallel()

	server, snapshot := authorizedRemote()
	snapshot.EnvironmentLive = false
	snapshot.Headers = nil
	_, err := readEnvironmentHeaders(t.Context(), &fakeEnvironmentHeaderSource{snapshot: snapshot, err: nil, calls: 0}, uuid.New(), server, authorizedURL)
	require.ErrorIs(t, err, environments.ErrEnvironmentUnavailable)
	require.True(t, isEnvironmentHeaderConfigError(err))
}

func TestReadEnvironmentHeadersTunneledIgnoresURL(t *testing.T) {
	t.Parallel()

	env, tunnel := uuid.New(), uuid.New()
	server := &mcpserversrepo.McpServer{ID: uuid.New(), EnvironmentID: linked(env), TunneledMcpServerID: linked(tunnel)}
	snapshot := environments.MCPServerHeaderSnapshot{EnvironmentID: linked(env), EnvironmentLive: true, TunneledMcpServerID: linked(tunnel), Headers: mappedHeaders()}
	got, err := readEnvironmentHeaders(t.Context(), &fakeEnvironmentHeaderSource{snapshot: snapshot, err: nil, calls: 0}, uuid.New(), server, "")
	require.NoError(t, err)
	require.Len(t, got.rows, 1)
}

func TestReadEnvironmentHeadersWithoutSourceRefusesLinkedServer(t *testing.T) {
	t.Parallel()

	server, _ := authorizedRemote()
	_, err := readEnvironmentHeaders(t.Context(), nil, uuid.New(), server, authorizedURL)
	require.Error(t, err)
	require.False(t, isEnvironmentHeaderConfigError(err))
}

func TestMemberMatchesServer(t *testing.T) {
	t.Parallel()

	remote, env := uuid.New(), uuid.New()
	member := metaMember{remoteServerID: linked(remote), environmentID: linked(env)}

	require.True(t, memberMatchesServer(member, mcpserversrepo.McpServer{RemoteMcpServerID: linked(remote), EnvironmentID: linked(env)}))
	require.False(t, memberMatchesServer(member, mcpserversrepo.McpServer{RemoteMcpServerID: linked(uuid.New()), EnvironmentID: linked(env)}), "repointed")
	require.False(t, memberMatchesServer(member, mcpserversrepo.McpServer{RemoteMcpServerID: linked(remote), EnvironmentID: linked(uuid.New())}), "relinked")
	require.False(t, memberMatchesServer(member, mcpserversrepo.McpServer{RemoteMcpServerID: linked(remote)}), "unlinked")
	require.False(t, memberMatchesServer(metaMember{remoteServerID: linked(remote)}, mcpserversrepo.McpServer{RemoteMcpServerID: linked(remote), EnvironmentID: linked(env)}), "linked after snapshot")
}

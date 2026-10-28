package codemode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type recoveryFixture struct {
	fixtureBackend
	lookups  int
	connects int
}

func (b *recoveryFixture) DiscoveryServers(context.Context) ([]Server, error) {
	b.lookups++
	return []Server{{Slug: "tracker", Connection: &ConnectionState{Consent: "required", Authentication: "unknown"}}}, nil
}
func (b *recoveryFixture) Connect(context.Context, string) (*ConnectionAction, error) {
	b.connects++
	return &ConnectionAction{Server: "tracker", AuthorizationURL: "https://gateway.example.test/connect"}, nil
}
func TestConnectionHelpersDoNotWidenToolAllowlist(t *testing.T) {
	t.Parallel()
	backend := &recoveryFixture{}
	host, err := NewHost(t.Context(), backend, "scope", []string{})
	require.NoError(t, err)
	status, err := host.Servers(t.Context(), PageArgs{})
	require.NoError(t, err)
	require.Equal(t, "required", status.Items[0].Connection.Consent)
	action, err := host.Connect(t.Context(), "tracker")
	require.NoError(t, err)
	require.NotEmpty(t, action.AuthorizationURL)
	_, err = host.Call(t.Context(), "tracker--read", []byte(`{}`))
	require.Error(t, err)
	require.Empty(t, backend.calls)
	for _, server := range []string{"", "tracker--read", " tracker", string(make([]byte, 1025))} {
		_, err = host.Connect(t.Context(), server)
		require.Error(t, err)
	}
	require.Equal(t, 1, backend.connects)
}
func TestConnectionCallbacksUseStrictArguments(t *testing.T) {
	t.Parallel()
	backend := &recoveryFixture{}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	state := &executionState{}
	for _, method := range []string{"connect"} {
		result, err := state.callback(t.Context(), host, runnerMessage{Method: method, Arguments: []byte(`{"server":"tracker"}`)})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		_, err = state.callback(t.Context(), host, runnerMessage{Method: method, Arguments: []byte(`{"server":"tracker","user":"forged"}`)})
		require.Error(t, err)
	}
	require.Empty(t, backend.calls, "recovery never dispatches a business tool")
}

func TestCodeServerPaginationIgnoresConnectionChanges(t *testing.T) {
	t.Parallel()
	servers := []Server{{Slug: "a", Name: "A", Connection: &ConnectionState{Consent: "required"}}, {Slug: "b", Name: "B", Connection: &ConnectionState{Consent: "required"}}}
	first, err := PaginateServers("scope", PageArgs{Limit: 1}, servers)
	require.NoError(t, err)
	servers[1].Connection.Consent = "approved"
	next, err := PaginateServers("scope", PageArgs{Limit: 1, Cursor: first.NextCursor}, servers)
	require.NoError(t, err)
	require.Equal(t, "b", next.Items[0].Slug)
	require.Equal(t, "approved", next.Items[0].Connection.Consent)
	servers[1].Slug = "c"
	_, err = PaginateServers("scope", PageArgs{Limit: 1, Cursor: first.NextCursor}, servers)
	require.Error(t, err, "membership changes must invalidate the cursor")
	servers[1].Slug = "b"
	_, err = PaginateServers("different-scope", PageArgs{Limit: 1, Cursor: first.NextCursor}, servers)
	require.Error(t, err, "cursor cannot cross authorization scope")
}

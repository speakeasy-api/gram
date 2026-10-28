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

func (b *recoveryFixture) ConnectionStatus(context.Context, string) (*ConnectionStatus, error) {
	b.lookups++
	return &ConnectionStatus{Servers: []ConnectionState{{Server: "tracker", Status: "needs_consent"}}}, nil
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
	status, err := host.ConnectionStatus(t.Context(), "tracker")
	require.NoError(t, err)
	require.Equal(t, "needs_consent", status.Servers[0].Status)
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
	host, err := NewHost(t.Context(), &recoveryFixture{}, "scope", nil)
	require.NoError(t, err)
	state := &executionState{}
	for _, method := range []string{"connect", "connection_status"} {
		result, err := state.callback(t.Context(), host, runnerMessage{Method: method, Arguments: []byte(`{"server":"tracker"}`)})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		_, err = state.callback(t.Context(), host, runnerMessage{Method: method, Arguments: []byte(`{"server":"tracker","user":"forged"}`)})
		require.Error(t, err)
	}
	require.Empty(t, state.calls, "recovery never dispatches a business tool")
}

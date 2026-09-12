package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRemoteMCPTransportURL_AllowsFullIPv4LoopbackRange(t *testing.T) {
	t.Parallel()
	_, err := validateRemoteMCPTransportURL("http://127.42.19.8:8080/mcp")
	require.NoError(t, err)
}

func TestValidateRemoteMCPTransportURL_AllowsIPv4MappedIPv6Loopback(t *testing.T) {
	t.Parallel()
	_, err := validateRemoteMCPTransportURL("http://[::ffff:127.2.3.4]:8080/mcp")
	require.NoError(t, err)
}

func TestValidateRemoteMCPTransportURL_AllowsIPv6Loopback(t *testing.T) {
	t.Parallel()
	_, err := validateRemoteMCPTransportURL("http://[::1]:8080/mcp")
	require.NoError(t, err)
}

func TestValidateRemoteMCPTransportURL_RejectsHostedHTTP(t *testing.T) {
	t.Parallel()
	_, err := validateRemoteMCPTransportURL("http://192.0.2.1/mcp")
	require.ErrorIs(t, err, ErrInsecureRemoteMCPTransport)
}

func TestValidateRemoteMCPTransportURL_RejectsUserinfo(t *testing.T) {
	t.Parallel()
	_, err := validateRemoteMCPTransportURL("https://user:secret@mcp.example.com/mcp")
	require.ErrorIs(t, err, ErrRemoteMCPURLUserinfo)
}

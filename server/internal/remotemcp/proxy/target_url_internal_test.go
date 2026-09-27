package proxy

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func TestSameRemoteMCPOrigin_MatchesImpliedDefaultPort(t *testing.T) {
	t.Parallel()
	require.True(t, sameRemoteMCPOrigin(mustParseURL(t, "https://mcp.example.com/mcp"), mustParseURL(t, "https://mcp.example.com:443/other")))
}

func TestSameRemoteMCPOrigin_IgnoresCase(t *testing.T) {
	t.Parallel()
	require.True(t, sameRemoteMCPOrigin(mustParseURL(t, "https://MCP.Example.com/mcp"), mustParseURL(t, "HTTPS://mcp.example.com/mcp")))
}

func TestSameRemoteMCPOrigin_MatchesZeroPaddedPort(t *testing.T) {
	t.Parallel()
	require.True(t, sameRemoteMCPOrigin(mustParseURL(t, "https://mcp.example.com/mcp"), mustParseURL(t, "https://mcp.example.com:0443/mcp")))
	require.True(t, sameRemoteMCPOrigin(mustParseURL(t, "https://mcp.example.com:8443/mcp"), mustParseURL(t, "https://mcp.example.com:08443/mcp")))
}

func TestSameRemoteMCPOrigin_RejectsPortChange(t *testing.T) {
	t.Parallel()
	require.False(t, sameRemoteMCPOrigin(mustParseURL(t, "https://mcp.example.com/mcp"), mustParseURL(t, "https://mcp.example.com:8443/mcp")))
}

func TestSameRemoteMCPOrigin_RejectsSubdomain(t *testing.T) {
	t.Parallel()
	require.False(t, sameRemoteMCPOrigin(mustParseURL(t, "https://example.com/mcp"), mustParseURL(t, "https://attacker.example.com/mcp")))
}

func TestSameRemoteMCPOrigin_RejectsSchemeChange(t *testing.T) {
	t.Parallel()
	require.False(t, sameRemoteMCPOrigin(mustParseURL(t, "https://127.0.0.1:8080/mcp"), mustParseURL(t, "http://127.0.0.1:8080/mcp")))
}

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// The strict meta MCP router: exact resource match only, no lone-token
// fallback for remote members, lone unqualified token only for tunneled.
func TestRouteMetaMemberToken(t *testing.T) {
	t.Parallel()

	remoteMember := metaMember{slug: "m", remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	tunnelMember := metaMember{slug: "m", tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	entry := func(token, resource string) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Token: token, Resource: resource}
	}
	tokens := func(entries ...remotesessions.UpstreamToken) map[uuid.UUID]remotesessions.UpstreamToken {
		m := make(map[uuid.UUID]remotesessions.UpstreamToken, len(entries))
		for _, e := range entries {
			m[uuid.New()] = e
		}
		return m
	}

	t.Run("remote exact match wins", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "https://a.example.com/mcp"), entry("b", "https://b.example.com/mcp")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Equal(t, "a", got)
	})

	t.Run("remote trailing slash normalized", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "https://a.example.com/mcp/")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Equal(t, "a", got)
	})

	t.Run("remote lone mismatched token is never forwarded", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "https://elsewhere.example.com/mcp")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got, "no lone-token fallback: an unmatched lone token means an anonymous call")
	})

	t.Run("remote lone unqualified token is never forwarded", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("remote duplicate resource fails member-scoped", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(tokens(entry("a", "https://a.example.com/mcp"), entry("b", "https://a.example.com/mcp")), remoteMember, "https://a.example.com/mcp")
		var memberErr *metaMemberError
		require.ErrorAs(t, err, &memberErr)
	})

	t.Run("remote zero tokens is anonymous", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(nil, remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tunneled lone unqualified token forwards", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "")), tunnelMember, "")
		require.NoError(t, err)
		require.Equal(t, "a", got)
	})

	t.Run("tunneled lone qualified token belongs elsewhere", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "https://a.example.com/mcp")), tunnelMember, "")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tunneled multiple tokens fail member-scoped", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(tokens(entry("a", ""), entry("b", "https://b.example.com/mcp")), tunnelMember, "")
		var memberErr *metaMemberError
		require.ErrorAs(t, err, &memberErr)
	})
}

// close must reach the proxy builder on a live detached context even after
// the member call's own context is gone (the strand-avoidance contract).
func TestMemberSessionClose_DetachedContext(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	var buildCtxErr error
	sess := &memberSession{
		svc:    nil,
		logger: nil,
		build: func(ctx context.Context) (*proxy.Proxy, error) {
			buildCtxErr = ctx.Err()
			return nil, errors.New("stop before any network work")
		},
		member:    metaMember{slug: "m", remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
		sessionID: "sess-1",
	}
	sess.close(canceled)
	require.NoError(t, buildCtxErr, "the session DELETE must not be built on the expired call context")
}

// The forwarded _meta declares the upstream hop's own protocol version, never
// the client's, so it cannot contradict the member handshake.
func TestMemberWireMeta_ProtocolVersionRewrite(t *testing.T) {
	t.Parallel()

	require.Nil(t, memberWireMeta(nil))

	meta := &mcprequests.WireMeta{ProtocolVersion: "2026-07-28", ClientInfo: &mcprequests.WireClientInfo{Name: "c", Version: "1"}, Capabilities: nil}
	got := memberWireMeta(meta)
	require.Equal(t, metaMemberUpstreamProtocolVersion, got.ProtocolVersion)
	require.Equal(t, meta.ClientInfo, got.ClientInfo)
	require.Equal(t, "2026-07-28", meta.ProtocolVersion, "the caller's _meta must not be mutated")
}

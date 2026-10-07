package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestMemberResponseRecorderStatusStartsUnset(t *testing.T) {
	t.Parallel()

	rec := newMemberResponseRecorder()
	require.Zero(t, rec.status)

	rec.WriteHeader(http.StatusNoContent)
	require.Equal(t, http.StatusNoContent, rec.status)
}

// The strict meta MCP router: exact resource match only, no lone-token
// fallback for remote members; tunneled members route by the single
// credential of their own derived remote_session_issuer, unqualified grants
// only.
func TestRouteMetaMemberToken(t *testing.T) {
	t.Parallel()

	tunnelIssuerID := uuid.New()
	remoteMember := metaMember{slug: "m", remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	tunnelMember := metaMember{slug: "m", tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: tunnelIssuerID, Valid: true}}
	entry := func(token, resource string) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Token: token, Resource: resource}
	}
	// own is a credential of the tunneled member's own authorization server.
	own := func(token, resource string) remotesessions.UpstreamToken {
		e := entry(token, resource)
		e.RemoteSessionIssuerID = tunnelIssuerID
		return e
	}
	tokens := func(entries ...remotesessions.UpstreamToken) remotesessions.ClientTokens {
		m := make(remotesessions.ClientTokens, len(entries))
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

	t.Run("tunneled own-issuer unqualified token forwards", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(own("a", "")), tunnelMember, "")
		require.NoError(t, err)
		require.Equal(t, "a", got)
	})

	t.Run("tunneled fails closed when several clients share its issuer", func(t *testing.T) {
		t.Parallel()
		// A gateway may bind several clients of one authorization server for
		// its remote members; the tunnel's issuer then names no single
		// credential, and guessing could forward a sibling's bearer.
		got, err := routeMetaMemberToken(tokens(own("a", ""), own("b", "")), tunnelMember, "")
		require.ErrorIs(t, err, errAmbiguousMemberCredential)
		require.Empty(t, got)
	})

	t.Run("tunneled own-issuer qualified token belongs elsewhere", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(own("a", "https://a.example.com/mcp")), tunnelMember, "")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tunneled sibling token is never forwarded", func(t *testing.T) {
		t.Parallel()
		// A partial map holding only a sibling's credential — the exact shape
		// partial resolution produces when this member's provider is not
		// connected — must degrade to an anonymous call.
		got, err := routeMetaMemberToken(tokens(entry("sibling", "")), tunnelMember, "")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tunneled member without derived issuer is anonymous", func(t *testing.T) {
		t.Parallel()
		bare := metaMember{slug: "m", tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
		got, err := routeMetaMemberToken(tokens(entry("a", "")), bare, "")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tunneled routes its own entry among several", func(t *testing.T) {
		t.Parallel()
		m := tokens(entry("sibling", ""), entry("other", "https://b.example.com/mcp"), own("own", ""))
		got, err := routeMetaMemberToken(m, tunnelMember, "")
		require.NoError(t, err)
		require.Equal(t, "own", got)
	})

	t.Run("tunneled identifier matches its own grant", func(t *testing.T) {
		t.Parallel()
		m := tokens(entry("sibling", "https://b.example.com/mcp"), own("own", "https://tunneled.internal/mcp/"))
		got, err := routeMetaMemberToken(m, tunnelMember, "https://tunneled.internal/mcp")
		require.NoError(t, err)
		require.Equal(t, "own", got)
	})

	t.Run("tunneled identifier never selects across issuers", func(t *testing.T) {
		t.Parallel()
		// The member's dial target is its tunnel, not the resource its
		// identifier names. An operator with mcp:write picks that identifier,
		// so matching a sibling's credential on it would hand the sibling's
		// bearer to the tunnel.
		got, err := routeMetaMemberToken(tokens(entry("sibling", "https://api.vendor.com/mcp")), tunnelMember, "https://api.vendor.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tunneled grant qualified elsewhere is anonymous", func(t *testing.T) {
		t.Parallel()
		m := tokens(own("own", "https://elsewhere.example.com/mcp"))
		got, err := routeMetaMemberToken(m, tunnelMember, "https://tunneled.internal/mcp")
		require.NoError(t, err)
		require.Empty(t, got, "a credential qualified to another upstream degrades to an anonymous call")
	})

	t.Run("tunneled pre-identifier grant routes by own issuer", func(t *testing.T) {
		t.Parallel()
		// A grant minted against the member's issuer before its resource
		// identifier was recorded is unqualified; the identity key still
		// routes it, so recording an identifier does not strand the grant.
		m := tokens(entry("sibling", "https://b.example.com/mcp"), own("own", ""))
		got, err := routeMetaMemberToken(m, tunnelMember, "https://tunneled.internal/mcp")
		require.NoError(t, err)
		require.Equal(t, "own", got)
	})

	t.Run("remote duplicate resource errors before any rescue", func(t *testing.T) {
		t.Parallel()
		// An unqualified sibling entry must not turn an ambiguous remote
		// match into a silent selection.
		m := tokens(entry("a", "https://a.example.com/mcp"), entry("b", "https://a.example.com/mcp"), entry("c", ""))
		_, err := routeMetaMemberToken(m, remoteMember, "https://a.example.com/mcp")
		var memberErr *metaMemberError
		require.ErrorAs(t, err, &memberErr)
		require.ErrorIs(t, err, errAmbiguousMemberCredential, "callers can tell a configuration state from a probe or dispatch failure")
	})
}

// close must reach the proxy builder on a live detached context even after
// the member call's own context is gone (the strand-avoidance contract).
func TestUnobservedMemberSessionClose_DetachedContext(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	var buildCtxErr error
	build := func(ctx context.Context) (*proxy.Proxy, error) {
		buildCtxErr = ctx.Err()
		return nil, errors.New("stop before any network work")
	}
	closeUnobservedMemberSession(canceled, nil, build, "sess-1", memberSessionCloseTimeout)
	require.NoError(t, buildCtxErr, "the session DELETE must not be built on the expired call context")
}

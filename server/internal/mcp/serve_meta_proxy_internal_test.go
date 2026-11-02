package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMemberResponseRecorderStatusStartsUnset(t *testing.T) {
	t.Parallel()

	rec := newMemberResponseRecorder()
	require.Zero(t, rec.status)

	rec.WriteHeader(http.StatusNoContent)
	require.Equal(t, http.StatusNoContent, rec.status)
}

// The strict meta MCP router: exact resource match only, no lone-token
// fallback for remote members; tunneled members route by their own derived
// remote_session_issuer key, unqualified grants only.
func TestRouteMetaMemberToken(t *testing.T) {
	t.Parallel()

	tunnelIssuerID := uuid.New()
	remoteMember := metaMember{slug: "m", remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	tunnelMember := metaMember{slug: "m", tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: tunnelIssuerID, Valid: true}}
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
		require.Equal(t, "a", got.Token)
	})

	t.Run("remote trailing slash normalized", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "https://a.example.com/mcp/")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Equal(t, "a", got.Token)
	})

	t.Run("remote lone mismatched token is never forwarded", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "https://elsewhere.example.com/mcp")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got.Token, "no lone-token fallback: an unmatched lone token means an anonymous call")
	})

	t.Run("remote lone unqualified token is never forwarded", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(tokens(entry("a", "")), remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got.Token)
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
		require.Empty(t, got.Token)
	})

	t.Run("tunneled own-issuer unqualified token forwards", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{tunnelIssuerID: entry("a", "")}, tunnelMember, "")
		require.NoError(t, err)
		require.Equal(t, "a", got.Token)
	})

	t.Run("tunneled own-issuer qualified token belongs elsewhere", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{tunnelIssuerID: entry("a", "https://a.example.com/mcp")}, tunnelMember, "")
		require.NoError(t, err)
		require.Empty(t, got.Token)
	})

	t.Run("tunneled sibling token is never forwarded", func(t *testing.T) {
		t.Parallel()
		// A partial map holding only a sibling's credential — the exact shape
		// partial resolution produces when this member's provider is not
		// connected — must degrade to an anonymous call.
		got, err := routeMetaMemberToken(tokens(entry("sibling", "")), tunnelMember, "")
		require.NoError(t, err)
		require.Empty(t, got.Token)
	})

	t.Run("tunneled member without derived issuer is anonymous", func(t *testing.T) {
		t.Parallel()
		bare := metaMember{slug: "m", tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
		got, err := routeMetaMemberToken(tokens(entry("a", "")), bare, "")
		require.NoError(t, err)
		require.Empty(t, got.Token)
	})

	t.Run("tunneled routes its own entry among several", func(t *testing.T) {
		t.Parallel()
		m := tokens(entry("sibling", ""), entry("other", "https://b.example.com/mcp"))
		m[tunnelIssuerID] = entry("own", "")
		got, err := routeMetaMemberToken(m, tunnelMember, "")
		require.NoError(t, err)
		require.Equal(t, "own", got.Token)
	})

	t.Run("tunneled identifier matches its own grant", func(t *testing.T) {
		t.Parallel()
		m := tokens(entry("sibling", "https://b.example.com/mcp"))
		m[tunnelIssuerID] = entry("own", "https://tunneled.internal/mcp/")
		got, err := routeMetaMemberToken(m, tunnelMember, "https://tunneled.internal/mcp")
		require.NoError(t, err)
		require.Equal(t, "own", got.Token)
	})

	t.Run("tunneled identifier never selects across issuers", func(t *testing.T) {
		t.Parallel()
		// The member's dial target is its tunnel, not the resource its
		// identifier names. An operator with mcp:write picks that identifier,
		// so matching a sibling's credential on it would hand the sibling's
		// bearer to the tunnel.
		got, err := routeMetaMemberToken(tokens(entry("sibling", "https://api.vendor.com/mcp")), tunnelMember, "https://api.vendor.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got.Token)
	})

	t.Run("tunneled grant qualified elsewhere is anonymous", func(t *testing.T) {
		t.Parallel()
		m := map[uuid.UUID]remotesessions.UpstreamToken{tunnelIssuerID: entry("own", "https://elsewhere.example.com/mcp")}
		got, err := routeMetaMemberToken(m, tunnelMember, "https://tunneled.internal/mcp")
		require.NoError(t, err)
		require.Empty(t, got.Token, "a credential qualified to another upstream degrades to an anonymous call")
	})

	t.Run("tunneled pre-identifier grant routes by own issuer", func(t *testing.T) {
		t.Parallel()
		// A grant minted against the member's issuer before its resource
		// identifier was recorded is unqualified; the identity key still
		// routes it, so recording an identifier does not strand the grant.
		m := tokens(entry("sibling", "https://b.example.com/mcp"))
		m[tunnelIssuerID] = entry("own", "")
		got, err := routeMetaMemberToken(m, tunnelMember, "https://tunneled.internal/mcp")
		require.NoError(t, err)
		require.Equal(t, "own", got.Token)
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

func TestRouteMetaMemberToken_SelfCredential(t *testing.T) {
	t.Parallel()

	memberIssuer := uuid.New()
	remoteMember := metaMember{slug: "m", remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: memberIssuer, Valid: true}}
	self := func(token string, err error) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Token: token, CredentialOwner: remotesessions.CredentialOwnerSelf, ClientCredentialErr: err}
	}

	t.Run("routes by the member's own issuer", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{memberIssuer: self("self-token", nil)}, remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Equal(t, "self-token", got.Token)
		require.Equal(t, remotesessions.CredentialOwnerSelf, got.CredentialOwner)
	})

	t.Run("another issuer's self credential is never forwarded", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): self("self-token", nil)}, remoteMember, "https://a.example.com/mcp")
		require.NoError(t, err)
		require.Empty(t, got.Token)
	})

	t.Run("misconfigured self credential names the administrator", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{memberIssuer: self("", remotesessions.ErrClientCredentialMisconfigured)}, remoteMember, "https://a.example.com/mcp")
		memberErr, ok := errors.AsType[*metaMemberError](err)
		require.True(t, ok, "error: %v", err)
		require.Contains(t, memberErr.message, "contact the MCP server administrator")
	})

	t.Run("self credential for another resource names the administrator", func(t *testing.T) {
		t.Parallel()
		minted := remotesessions.UpstreamToken{Token: "self-token", Resource: "https://b.example.com/mcp", CredentialOwner: remotesessions.CredentialOwnerSelf}
		_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{memberIssuer: minted}, remoteMember, "https://a.example.com/mcp")
		memberErr, ok := errors.AsType[*metaMemberError](err)
		require.True(t, ok, "never an anonymous call: %v", err)
		require.Contains(t, memberErr.message, "issued for a different resource")
	})

	t.Run("unavailable self credential asks for a retry", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{memberIssuer: self("", remotesessions.ErrClientCredentialUnavailable)}, remoteMember, "https://a.example.com/mcp")
		memberErr, ok := errors.AsType[*metaMemberError](err)
		require.True(t, ok, "error: %v", err)
		require.Contains(t, memberErr.message, "Retry shortly")
	})
}

func TestRouteMetaMemberToken_SessionOutage(t *testing.T) {
	t.Parallel()

	const upstream = "https://a.example.com/mcp"
	memberIssuer := uuid.New()
	remoteMember := metaMember{slug: "m", remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: memberIssuer, Valid: true}}
	tunneledMember := metaMember{slug: "m", tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: memberIssuer, Valid: true}}
	outage := func(resource string) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Resource: resource, CredentialOwner: remotesessions.CredentialOwnerSubject, ClientCredentialErr: remotesessions.ErrRemoteSessionUnavailable}
	}
	requireRetry := func(t *testing.T, err error) {
		t.Helper()
		_, retryable := errors.AsType[memberUnavailableError](err)
		require.True(t, retryable, "error: %v", err)
		_, credential := errors.AsType[memberCredentialError](err)
		require.False(t, credential, "an outage must not ask for a reconnect")
	}

	t.Run("remote member by recorded resource", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): outage(upstream)}, remoteMember, upstream)
		requireRetry(t, err)
	})

	t.Run("remote member by issuer when no resource was recorded", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{memberIssuer: outage("")}, remoteMember, upstream)
		requireRetry(t, err)
	})

	t.Run("tunneled member", func(t *testing.T) {
		t.Parallel()
		_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{memberIssuer: outage("")}, tunneledMember, upstream)
		requireRetry(t, err)
	})

	t.Run("a sibling's outage leaves this member anonymous", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): outage("https://b.example.com/mcp")}, remoteMember, upstream)
		require.NoError(t, err)
		require.Empty(t, got.Token)
	})

	t.Run("a usable credential for the upstream outranks the outage", func(t *testing.T) {
		t.Parallel()
		got, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{
			uuid.New(): outage(upstream),
			uuid.New(): {Token: "good", Resource: upstream, CredentialOwner: remotesessions.CredentialOwnerSubject},
		}, remoteMember, upstream)
		require.NoError(t, err)
		require.Equal(t, "good", got.Token)
	})
}

func TestRouteUpstreamToken_LiveMatchOutranksOutage(t *testing.T) {
	t.Parallel()

	const upstream = "https://a.example.com/mcp"
	outage := remotesessions.UpstreamToken{Resource: upstream, CredentialOwner: remotesessions.CredentialOwnerSubject, ClientCredentialErr: remotesessions.ErrRemoteSessionUnavailable}
	none := uuid.NullUUID{}

	got, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): outage,
		uuid.New(): {Token: "good", Resource: upstream, CredentialOwner: remotesessions.CredentialOwnerSubject},
	}, upstream, false, none)
	require.NoError(t, err)
	require.Equal(t, "good", got.Token)

	_, err = routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): outage}, upstream, false, none)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
}

func TestMemberTokens_StoreOutageReadsAsUnavailable(t *testing.T) {
	t.Parallel()

	gate := func(err error) metaGateContext {
		return metaGateContext{projectTokens: &projectTokenCache{resolve: func(context.Context, uuid.UUID) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			return nil, err
		}, byProject: map[uuid.UUID]map[uuid.UUID]remotesessions.UpstreamToken{}}}
	}
	outageGate, brokenGate := gate(&pgconn.PgError{Code: "57P01"}), gate(&pgconn.PgError{Code: "42P01"})

	_, err := outageGate.memberTokens(t.Context(), metaMember{slug: "m"})
	_, unavailable := errors.AsType[memberUnavailableError](err)
	require.True(t, unavailable, "error: %v", err)

	_, err = brokenGate.memberTokens(t.Context(), metaMember{slug: "m"})
	_, unavailable = errors.AsType[memberUnavailableError](err)
	require.False(t, unavailable, "a broken query is not an outage: %v", err)
}

func TestChainMetaMember_ChainingServesAnOutage(t *testing.T) {
	t.Parallel()

	routed := remotesessions.UpstreamToken{CredentialOwner: remotesessions.CredentialOwnerSubject, ClientCredentialErr: remotesessions.ErrRemoteSessionUnavailable}
	routeErr := errors.New("outage")
	chaining := func(token string) metaGateContext {
		return metaGateContext{chainUpstream: func(context.Context, metaMember, string) (string, error) { return token, nil }}
	}

	_, token, err := chainMetaMember(t.Context(), chaining("chained"), metaMember{}, "r", routed, routeErr)
	require.NoError(t, err)
	require.Equal(t, "chained", token)

	_, token, err = chainMetaMember(t.Context(), chaining(""), metaMember{}, "r", routed, routeErr)
	require.ErrorIs(t, err, routeErr, "with nothing to chain the outage stands")
	require.Empty(t, token)

	self := remotesessions.UpstreamToken{CredentialOwner: remotesessions.CredentialOwnerSelf, ClientCredentialErr: remotesessions.ErrClientCredentialUnavailable}
	_, _, err = chainMetaMember(t.Context(), chaining("chained"), metaMember{}, "r", self, routeErr)
	require.ErrorIs(t, err, routeErr, "a self client's failure is never chained around")
}

func TestMemberAuthFailure_SelfCredentialNamesAdministrator(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "m"}
	err := memberAuthFailure(member, memberDial{anonymous: false, clientCredential: true})
	require.Contains(t, err.Error(), "contact the MCP server administrator")
	require.NotContains(t, err.Error(), "reconnect")

	err = memberAuthFailure(member, memberDial{anonymous: false, clientCredential: false})
	require.Contains(t, err.Error(), "reconnect it from this gateway's sign-in page")
}

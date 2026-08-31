package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// routeUpstreamToken selects the single Authorization value a proxied MCP
// backend forwards. These cover the pure selection branches the DB-backed
// resolver tests cannot reach cheaply: qualified routing by the credential's
// grant-time RFC 8707 resource, the tunneled-issuer identity path, and the
// fail-closed ambiguity paths. There is no lone-token fallback: an unmatched
// credential is never forwarded no matter how few there are.

// noIssuer marks a remote backend, which routes strictly by recorded resource.
var noIssuer = uuid.NullUUID{UUID: uuid.Nil, Valid: false}

func TestRouteUpstreamToken_EmptyMapReturnsEmpty(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), nil, "https://upstream.example.com/mcp", noIssuer)
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_SingleMatchingEntryReturnsToken(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "upstream-token", Resource: "https://upstream.example.com/mcp/", RemoteSessionClientID: uuid.New()},
	}, "https://upstream.example.com/mcp", noIssuer)
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token)
}

func TestRouteUpstreamToken_LoneMismatchedEntryFailsClosed(t *testing.T) {
	t.Parallel()

	// No lone-token fallback: a credential recorded for another upstream is
	// never forwarded, however few credentials there are.
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "upstream-token", Resource: "https://other.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://upstream.example.com/mcp", noIssuer)
	requireRoutingError(t, err, "no_match")
	require.Empty(t, token)
}

func TestRouteUpstreamToken_LoneUnqualifiedEntryFailsClosed(t *testing.T) {
	t.Parallel()

	// A legacy grant with no recorded resource cannot be qualified to a
	// remote backend; re-consent (or refresh-time backfill) qualifies it.
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "upstream-token", Resource: "", RemoteSessionClientID: uuid.New()},
	}, "https://upstream.example.com/mcp", noIssuer)
	requireRoutingError(t, err, "legacy_null_resource")
	require.Empty(t, token)
}

func TestRouteUpstreamToken_NoResourceRoutesByTunneledIssuer(t *testing.T) {
	t.Parallel()

	// A tunneled backend with no recorded resource identifier routes by its
	// own derived issuer key, unqualified grants only.
	issuerID := uuid.New()
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID:   {Token: "own-token", Resource: "", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "sibling-token", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "", uuid.NullUUID{UUID: issuerID, Valid: true})
	require.NoError(t, err)
	require.Equal(t, "own-token", token)
}

func TestRouteUpstreamToken_NoResourceSiblingTokenIsAnonymous(t *testing.T) {
	t.Parallel()

	// A sibling's credential — even a lone one — is never forwarded to a
	// resourceless tunneled backend; the call degrades to anonymous.
	issuerID := uuid.New()
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "sibling-token", Resource: "", RemoteSessionClientID: uuid.New()},
	}, "", uuid.NullUUID{UUID: issuerID, Valid: true})
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_NoResourceQualifiedIssuerEntryIsAnonymous(t *testing.T) {
	t.Parallel()

	// An entry keyed by the backend's issuer whose grant is audience-bound to
	// a remote upstream belongs elsewhere.
	issuerID := uuid.New()
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {Token: "qualified-token", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "", uuid.NullUUID{UUID: issuerID, Valid: true})
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_NoResourceNoIssuerIsAnonymous(t *testing.T) {
	t.Parallel()

	// Several credentials against a resourceless backend used to fail closed;
	// with identity routing available the correct degradation is anonymous.
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "", noIssuer)
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_TunneledIssuerBacksUnmatchedResource(t *testing.T) {
	t.Parallel()

	// Grants minted against a tunneled backend's issuer before its resource
	// identifier was recorded are unqualified but still keyed by the
	// backend's own issuer; that identity match is exact, not a guess.
	issuerID := uuid.New()
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {Token: "own-token", Resource: "", RemoteSessionClientID: uuid.New()},
	}, "https://tunneled.internal/mcp", uuid.NullUUID{UUID: issuerID, Valid: true})
	require.NoError(t, err)
	require.Equal(t, "own-token", token)
}

func TestRouteUpstreamToken_MultipleEntriesRoutesByResource(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://b.example.com/mcp", noIssuer)
	require.NoError(t, err)
	require.Equal(t, "token-b", token)
}

func TestRouteUpstreamToken_ResourceMatchIgnoresTrailingSlash(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp/", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://a.example.com/mcp", noIssuer)
	require.NoError(t, err)
	require.Equal(t, "token-a", token)
}

func TestRouteUpstreamToken_MultipleEntriesNoMatchFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://c.example.com/mcp", noIssuer)
	requireRoutingError(t, err, "no_match")
	require.Empty(t, token)
}

func TestRouteUpstreamToken_DuplicateResourceFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://a.example.com/mcp", noIssuer)
	requireRoutingError(t, err, "duplicate_resource")
	require.Empty(t, token)
}

// A fail-closed routing outcome must surface as the typed error, because that
// is what the call sites key on to return a precondition failure instead of a
// 500 for what is a configuration state.
func requireRoutingError(t *testing.T, err error, reason string) {
	t.Helper()

	var routeErr *upstreamRoutingError
	require.ErrorAs(t, err, &routeErr)
	require.Equal(t, reason, routeErr.reason)
}

func TestTunnelGatewayURL_NormalizesAcceptedAddrs(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]string{
		"10.0.0.5:8090":                        "http://10.0.0.5:8090",       // host:port defaults to http
		"tunnel-gateway:8090":                  "http://tunnel-gateway:8090", // dns host:port defaults to http
		"http://tunnel-gateway:8090":           "http://tunnel-gateway:8090",
		"https://tunnel-gateway.internal:8443": "https://tunnel-gateway.internal:8443",
	} {
		got, err := tunnelrouting.GatewayURL(addr)
		require.NoError(t, err, "addr %q", addr)
		require.Equal(t, want, got, "addr %q", addr)
	}
}

func TestTunnelGatewayURL_RejectsInvalidAddrs(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{
		"https:///missing-host",
		"http:tunnel-gateway:8090", // opaque URL
		"http://:8090",             // empty hostname
		"ftp://tunnel-gateway:8090",
	} {
		_, err := tunnelrouting.GatewayURL(addr)
		require.Error(t, err, "addr %q", addr)
	}
}

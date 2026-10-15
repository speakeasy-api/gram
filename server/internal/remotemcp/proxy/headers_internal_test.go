package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestApplyResponseHeadersStripsTunnelError: X-Gram-Tunnel-Error is internal
// gateway→gram-server wire vocabulary consumed by the retry policy. It must
// never relay to external MCP clients, or the internal statuses become a
// de-facto public contract.
func TestApplyResponseHeadersStripsTunnelError(t *testing.T) {
	t.Parallel()

	upstream := &http.Response{
		Header: http.Header{
			"X-Gram-Tunnel-Error": []string{"no-live-session"},
			"Content-Type":        []string{"application/json"},
		},
	}

	rec := httptest.NewRecorder()
	applyResponseHeaders(rec, upstream, "")

	require.Empty(t, rec.Header().Get("X-Gram-Tunnel-Error"))
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// TestApplyResponseHeadersStripsUpstreamCORS: the CORS middleware sets the
// Access-Control-* headers for the browser. Relaying the upstream's copies
// would duplicate them and the browser would reject the response.
func TestApplyResponseHeadersStripsUpstreamCORS(t *testing.T) {
	t.Parallel()

	upstream := &http.Response{
		Header: http.Header{
			"Access-Control-Allow-Origin":      []string{"*"},
			"Access-Control-Allow-Credentials": []string{"true"},
			"Access-Control-Allow-Headers":     []string{"Content-Type, Authorization"},
			"Access-Control-Allow-Methods":     []string{"POST, OPTIONS"},
			"Access-Control-Expose-Headers":    []string{"Mcp-Session-Id"},
			"Access-Control-Max-Age":           []string{"86400"},
			"Content-Type":                     []string{"text/event-stream"},
		},
	}

	rec := httptest.NewRecorder()
	rec.Header().Set("Access-Control-Allow-Origin", "https://app.example.com")
	applyResponseHeaders(rec, upstream, "")

	require.Equal(t, []string{"https://app.example.com"}, rec.Header().Values("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Values("Access-Control-Allow-Credentials"))
	require.Empty(t, rec.Header().Values("Access-Control-Allow-Headers"))
	require.Empty(t, rec.Header().Values("Access-Control-Allow-Methods"))
	require.Empty(t, rec.Header().Values("Access-Control-Expose-Headers"))
	require.Empty(t, rec.Header().Values("Access-Control-Max-Age"))
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
}

func TestApplyRequestHeadersUserAuthorizationOverrideWinsConfiguredAuthorization(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{
				Name:                   "Authorization",
				StaticValue:            "Bearer agent-token",
				ValueFromRequestHeader: "",
				IsRequired:             true,
			},
			{
				Name:                   "X-Configured",
				StaticValue:            "preserved",
				ValueFromRequestHeader: "",
				IsRequired:             false,
			},
		},
		AuthorizationOverride: "user-token",
	}

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.NoError(t, err)
	require.Equal(t, "Bearer user-token", remoteReq.Header.Get("Authorization"))
	require.Equal(t, "preserved", remoteReq.Header.Get("X-Configured"))
}

func TestApplyRequestHeadersConfiguredAuthorizationAppliesWithoutOverride(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{
				Name:                   "Authorization",
				StaticValue:            "Basic agent-credential",
				ValueFromRequestHeader: "",
				IsRequired:             true,
			},
		},
		AuthorizationOverride: "",
	}

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.NoError(t, err)
	require.Equal(t, "Basic agent-credential", remoteReq.Header.Get("Authorization"))
}

func TestApplyRequestHeadersOverridePreservesNonAuthorizationConfiguredHeader(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{
				Name:                   "X-API-Key",
				StaticValue:            "configured-key",
				ValueFromRequestHeader: "",
				IsRequired:             true,
			},
		},
		AuthorizationOverride: "user-token",
	}

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.NoError(t, err)
	require.Equal(t, "Bearer user-token", remoteReq.Header.Get("Authorization"))
	require.Equal(t, "configured-key", remoteReq.Header.Get("X-API-Key"))
}

// protectedInboundFixture is one synthetic value for every inbound header
// family the remote policy protects, keyed by the spelling a client sends.
var protectedInboundFixture = map[string]string{
	"Gram-Key":                     "synthetic-api-key",
	"Gram-Session":                 "synthetic-session",
	"Gram-Chat-Session":            "synthetic-chat-session",
	"Gram-Project":                 "synthetic-project",
	"Gram-Consent-State":           "synthetic-consent",
	"Gram_Key":                     "synthetic-alias-key",
	"Speakeasy-AI-Key":             "synthetic-ai-key",
	"Speakeasy-AI-Chat-Session":    "synthetic-ai-chat-session",
	"Speakeasy_AI_Session":         "synthetic-ai-session",
	"X-Gram-Tunnel-Require-Active": "1",
	"X-Gram-Tunnel-Forward-Token":  "synthetic-forward-token",
	"X-Gram-Agent-Version":         "1.0.0",
	"X-Speakeasy-Identity":         "synthetic-assertion",
	"X_Speakeasy_Identity":         "synthetic-assertion-alias",
	"Authorization":                "Bearer synthetic-speakeasy-token",
	"Proxy-Authorization":          "Basic synthetic",
	"Cookie":                       "gram_session=synthetic",
}

func newRemotePolicyRequests(t *testing.T) (*http.Request, *http.Request) {
	t.Helper()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	return userReq, remoteReq
}

// requireBadRequest asserts err is a client-facing bad request and returns
// its message.
func requireBadRequest(t *testing.T, err error) string {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	return shareable.Error()
}

func TestApplyRequestHeadersRemoteStripsProtectedInboundHeaders(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	for name, value := range protectedInboundFixture {
		userReq.Header[name] = []string{value}
	}
	userReq.Header.Set("X-Client-Trace", "allowed")
	userReq.Header.Set("Mcp-Protocol-Version", "2025-06-18")

	p := &Proxy{Logger: testenv.NewLogger(t)}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

	for name := range protectedInboundFixture {
		require.Empty(t, remoteReq.Header.Values(name), "%s must not be copied to a remote upstream", name)
	}
	require.Len(t, remoteReq.Header, 3, "only the allowed client headers and the default User-Agent remain")
	require.Equal(t, "allowed", remoteReq.Header.Get("X-Client-Trace"))
	require.Equal(t, "2025-06-18", remoteReq.Header.Get("Mcp-Protocol-Version"))
	require.Equal(t, constants.UserAgent, remoteReq.Header.Get("User-Agent"))
}

func TestApplyRequestHeadersTunneledCopyUnchanged(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Gram-Key", "synthetic-api-key")
	userReq.Header.Set("Authorization", "Bearer synthetic")
	p := &Proxy{
		Logger:       testenv.NewLogger(t),
		HeaderPolicy: HeaderPolicyTunneled,
		Headers: []ConfiguredHeader{
			{Name: "X-Gram-Tunnel-Id", StaticValue: "tunnel-1", ValueFromRequestHeader: "", IsRequired: true},
			{Name: McpSessionIDHeader, StaticValue: "backend-session", ValueFromRequestHeader: "", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

	require.Equal(t, "synthetic-api-key", remoteReq.Header.Get("Gram-Key"), "tunneled forwarding is out of scope here")
	require.Empty(t, remoteReq.Header.Get("Authorization"))
	require.Equal(t, "tunnel-1", remoteReq.Header.Get("X-Gram-Tunnel-Id"))
	require.Equal(t, "backend-session", remoteReq.Header.Get(McpSessionIDHeader))
}

func TestApplyRequestHeadersRemoteSuppressesOptionalProtectedSource(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"GRAM-KEY", "gram_chat_session", "X-Gram-Tunnel-Forward-Token", "x_speakeasy_identity", "Cookie"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			userReq, remoteReq := newRemotePolicyRequests(t)
			userReq.Header[source] = []string{"synthetic-credential"}
			// A client value under the destination's own name must not stand in
			// for the suppressed row.
			userReq.Header.Set("X-Upstream-Token", "client-supplied")
			p := &Proxy{
				Logger: testenv.NewLogger(t),
				Headers: []ConfiguredHeader{
					{Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: source, IsRequired: false},
				},
			}
			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Empty(t, remoteReq.Header.Values("X-Upstream-Token"))
		})
	}
}

func TestApplyRequestHeadersRemoteRejectsRequiredProtectedSource(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Gram-Key", "synthetic-api-key")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: "gram-key", IsRequired: true},
		},
	}
	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	message := requireBadRequest(t, err)
	require.Contains(t, message, `"X-Upstream-Token"`)
	require.Contains(t, message, `"gram-key"`)
	require.Contains(t, message, "never forwarded to remote MCP servers")
	require.NotContains(t, message, "synthetic-api-key")
}

// A stored row that fills a Speakeasy header from the caller's request is
// refused at request time too, while a static operator Gram-Key is sent.
func TestApplyRequestHeadersRemoteRefusesRequestSourcedSpeakeasyDestination(t *testing.T) {
	t.Parallel()

	t.Run("required", func(t *testing.T) {
		t.Parallel()

		userReq, remoteReq := newRemotePolicyRequests(t)
		userReq.Header.Set("X-Client-Token", "synthetic-client-value")
		p := &Proxy{
			Logger: testenv.NewLogger(t),
			Headers: []ConfiguredHeader{
				{Name: "Gram-Key", StaticValue: "", ValueFromRequestHeader: "X-Client-Token", IsRequired: true},
			},
		}
		err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
		message := requireBadRequest(t, err)
		require.Contains(t, message, `required header "Gram-Key" cannot be populated from a request header: it is a Speakeasy header`)
		require.NotContains(t, message, "synthetic-client-value")
	})

	t.Run("optional", func(t *testing.T) {
		t.Parallel()

		userReq, remoteReq := newRemotePolicyRequests(t)
		userReq.Header.Set("X-Client-Token", "synthetic-client-value")
		p := &Proxy{
			Logger: testenv.NewLogger(t),
			Headers: []ConfiguredHeader{
				{Name: "Gram-Key", StaticValue: "", ValueFromRequestHeader: "X-Client-Token", IsRequired: false},
			},
		}
		require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
		require.Empty(t, remoteReq.Header.Values("Gram-Key"))
	})

	t.Run("static", func(t *testing.T) {
		t.Parallel()

		userReq, remoteReq := newRemotePolicyRequests(t)
		p := &Proxy{
			Logger: testenv.NewLogger(t),
			Headers: []ConfiguredHeader{
				{Name: "Gram-Key", StaticValue: "operator-credential", ValueFromRequestHeader: "", IsRequired: true},
			},
		}
		require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
		require.Equal(t, "operator-credential", remoteReq.Header.Get("Gram-Key"))
	})
}

func TestApplyRequestHeadersRemoteAuthorizationOverrideShadowsRequiredPassThrough(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Authorization", "Bearer synthetic-speakeasy-token")
	p := &Proxy{
		Logger:                testenv.NewLogger(t),
		AuthorizationOverride: "upstream-token",
		Headers: []ConfiguredHeader{
			{Name: "Authorization", StaticValue: "", ValueFromRequestHeader: "Authorization", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"Bearer upstream-token"}, remoteReq.Header.Values("Authorization"))
}

// Forwarding the caller's own upstream credential is what pass-through
// identity is for, so Authorization is an allowed source.
func TestApplyRequestHeadersRemoteForwardsAuthorizationPassThrough(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Authorization", "Bearer caller-upstream")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "Authorization", StaticValue: "", ValueFromRequestHeader: "Authorization", IsRequired: true},
			{Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: "authorization", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"Bearer caller-upstream"}, remoteReq.Header.Values("Authorization"))
	require.Equal(t, []string{"Bearer caller-upstream"}, remoteReq.Header.Values("X-Upstream-Token"))
}

func TestApplyRequestHeadersRemoteAllowsAuthorizationFromSeparateSource(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("X-Service-Token", "Bearer upstream-service")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "Authorization", StaticValue: "", ValueFromRequestHeader: "X-Service-Token", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "Bearer upstream-service", remoteReq.Header.Get("Authorization"))
}

func TestApplyRequestHeadersRemoteAcceptsStoredNonCanonicalNames(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("X-Caller-Upstream-Token", "caller-upstream")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "x-api-key", StaticValue: "static-credential", ValueFromRequestHeader: "", IsRequired: true},
			{Name: "X-API-SECRET", StaticValue: "static-secret", ValueFromRequestHeader: "", IsRequired: true},
			{Name: "x-forwarded-token", StaticValue: "", ValueFromRequestHeader: "x-caller-upstream-token", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "static-credential", remoteReq.Header.Get("X-Api-Key"))
	require.Equal(t, "static-secret", remoteReq.Header.Get("X-Api-Secret"))
	require.Equal(t, "caller-upstream", remoteReq.Header.Get("X-Forwarded-Token"))
}

func TestApplyRequestHeadersRemoteRejectsPaddedStoredNames(t *testing.T) {
	t.Parallel()

	for _, h := range []ConfiguredHeader{
		{Name: " X-Api-Key ", StaticValue: "static-credential", ValueFromRequestHeader: "", IsRequired: true},
		{Name: "X-Forwarded-Token", StaticValue: "", ValueFromRequestHeader: " X-Caller ", IsRequired: true},
	} {
		userReq, remoteReq := newRemotePolicyRequests(t)
		userReq.Header.Set("X-Caller", "caller")
		p := &Proxy{Logger: testenv.NewLogger(t), Headers: []ConfiguredHeader{h}}
		requireBadRequest(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

		h.IsRequired = false
		userReq, remoteReq = newRemotePolicyRequests(t)
		userReq.Header.Set("X-Caller", "caller")
		p = &Proxy{Logger: testenv.NewLogger(t), Headers: []ConfiguredHeader{h}}
		require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
		require.Empty(t, remoteReq.Header.Get("X-Api-Key"))
		require.Empty(t, remoteReq.Header.Get("X-Forwarded-Token"))
	}
}

func TestApplyRequestHeadersRemoteCookieDestinations(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("X-Upstream-Cookie", "session=caller")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "Cookie", StaticValue: "session=operator", ValueFromRequestHeader: "", IsRequired: true},
			{Name: "Set-Cookie", StaticValue: "session=operator", ValueFromRequestHeader: "", IsRequired: false},
			{Name: "Proxy-Authorization", StaticValue: "Basic operator", ValueFromRequestHeader: "", IsRequired: false},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "session=operator", remoteReq.Header.Get("Cookie"), "a static Cookie is an operator credential")
	require.Empty(t, remoteReq.Header.Get("Set-Cookie"))
	require.Empty(t, remoteReq.Header.Get("Proxy-Authorization"))

	for _, h := range []ConfiguredHeader{
		{Name: "Cookie", StaticValue: "", ValueFromRequestHeader: "X-Upstream-Cookie", IsRequired: true},
		{Name: "set-cookie", StaticValue: "session=operator", ValueFromRequestHeader: "", IsRequired: true},
		{Name: "Proxy-Authorization", StaticValue: "Basic operator", ValueFromRequestHeader: "", IsRequired: true},
	} {
		userReq, remoteReq := newRemotePolicyRequests(t)
		userReq.Header.Set("X-Upstream-Cookie", "session=caller")
		p := &Proxy{Logger: testenv.NewLogger(t), Headers: []ConfiguredHeader{h}}
		requireBadRequest(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	}
}

func TestApplyRequestHeadersRemoteAllowsStaticSpeakeasyCredentialDestination(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Gram-Key", "caller-api-key")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "Gram-Key", StaticValue: "operator-api-key", ValueFromRequestHeader: "", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"operator-api-key"}, remoteReq.Header.Values("Gram-Key"))
}

func TestApplyRequestHeadersRemoteIgnoresUnownedDestinations(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Mcp-Method", "tools/call")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "Mcp-Method", StaticValue: "configured", ValueFromRequestHeader: "", IsRequired: true},
			{Name: "Mcp_Method", StaticValue: "configured", ValueFromRequestHeader: "", IsRequired: true},
			{Name: "X-Speakeasy-Identity", StaticValue: "forged", ValueFromRequestHeader: "", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"tools/call"}, remoteReq.Header.Values("Mcp-Method"), "the client's protocol header is forwarded untouched")
	require.Empty(t, remoteReq.Header.Values("Mcp_Method"))
	require.Empty(t, remoteReq.Header.Get("X-Speakeasy-Identity"))
}

func TestApplyRequestHeadersRemoteInvalidStaticValue(t *testing.T) {
	t.Parallel()

	optional := ConfiguredHeader{Name: "X-Api-Key", StaticValue: "line1\r\nX-Injected: 1", ValueFromRequestHeader: "", IsRequired: false}
	userReq, remoteReq := newRemotePolicyRequests(t)
	p := &Proxy{Logger: testenv.NewLogger(t), Headers: []ConfiguredHeader{optional}}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Empty(t, remoteReq.Header.Get("X-Api-Key"))

	required := optional
	required.IsRequired = true
	userReq, remoteReq = newRemotePolicyRequests(t)
	p = &Proxy{Logger: testenv.NewLogger(t), Headers: []ConfiguredHeader{required}}
	message := requireBadRequest(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.NotContains(t, message, "line1")

	tabbed := ConfiguredHeader{Name: "X-Api-Key", StaticValue: "a\tb", ValueFromRequestHeader: "", IsRequired: true}
	userReq, remoteReq = newRemotePolicyRequests(t)
	p = &Proxy{Logger: testenv.NewLogger(t), Headers: []ConfiguredHeader{tabbed}}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "a\tb", remoteReq.Header.Get("X-Api-Key"))
}

// Rows arrive ordered by stored name, and sequential Set keeps the last one:
// the policy's case-insensitive matching must not pick a different credential.
func TestApplyRequestHeadersRemoteKeepsCollisionOrder(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "X-Api-Key", StaticValue: "first", ValueFromRequestHeader: "", IsRequired: true},
			{Name: "x-api-key", StaticValue: "second", ValueFromRequestHeader: "", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"second"}, remoteReq.Header.Values("X-Api-Key"))
}

func TestCheckRemoteHeader(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		header  ConfiguredHeader
		wantErr error
	}{
		{name: "static custom", header: ConfiguredHeader{Name: "X-Api-Key", StaticValue: "v"}, wantErr: nil},
		{name: "allowed source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X-Service-Token"}, wantErr: nil},
		{name: "static cookie", header: ConfiguredHeader{Name: "Cookie", StaticValue: "a=b"}, wantErr: nil},
		{name: "static authorization", header: ConfiguredHeader{Name: "Authorization", StaticValue: "Bearer x"}, wantErr: nil},
		{name: "static gram key", header: ConfiguredHeader{Name: "Gram-Key", StaticValue: "operator"}, wantErr: nil},
		{name: "authorization source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "Authorization"}, wantErr: nil},
		{name: "gram key source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "Gram-Key"}, wantErr: ErrProtectedSource},
		{name: "mixed case gram source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "gRaM-cHaT-sEsSiOn"}, wantErr: ErrProtectedSource},
		{name: "speakeasy-ai key source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "Speakeasy-AI-Key"}, wantErr: ErrProtectedSource},
		{name: "speakeasy-ai alias source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "speakeasy_ai_project"}, wantErr: ErrProtectedSource},
		{name: "tunnel source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X-Gram-Tunnel-Id"}, wantErr: ErrProtectedSource},
		{name: "agent version source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X-Gram-Agent-Version"}, wantErr: ErrProtectedSource},
		{name: "assertion alias source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X_Speakeasy_Identity"}, wantErr: ErrProtectedSource},
		{name: "set-cookie destination", header: ConfiguredHeader{Name: "Set-Cookie", StaticValue: "a=b"}, wantErr: ErrReservedHeader},
		{name: "proxy-authorization destination", header: ConfiguredHeader{Name: "proxy-authorization", StaticValue: "Basic x"}, wantErr: ErrReservedHeader},
		{name: "gram key from source", header: ConfiguredHeader{Name: "Gram-Key", ValueFromRequestHeader: "X-Client-Token"}, wantErr: ErrProtectedDestination},
		{name: "speakeasy-ai alias from source", header: ConfiguredHeader{Name: "speakeasy_ai_key", ValueFromRequestHeader: "X-Client-Token"}, wantErr: ErrProtectedDestination},
		{name: "authorization from source", header: ConfiguredHeader{Name: "Authorization", ValueFromRequestHeader: "X-Client-Token"}, wantErr: nil},
		{name: "cookie from source", header: ConfiguredHeader{Name: "Cookie", ValueFromRequestHeader: "X-Upstream-Cookie"}, wantErr: ErrReservedHeader},
		{name: "assertion destination", header: ConfiguredHeader{Name: "x-speakeasy-identity", StaticValue: "v"}, wantErr: ErrReservedHeader},
		{name: "mcp destination alias", header: ConfiguredHeader{Name: "Mcp_Method", StaticValue: "v"}, wantErr: ErrReservedHeader},
		{name: "control byte value", header: ConfiguredHeader{Name: "X-Api-Key", StaticValue: "a\nb"}, wantErr: ErrInvalidHeaderValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := CheckRemoteHeader(tc.header)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// A resolved upstream token owns Authorization only: a custom header reading
// the caller's Authorization still forwards it beside the override.
func TestApplyRequestHeadersRemoteOverrideLeavesCustomPassThrough(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Authorization", "Bearer caller-upstream")
	p := &Proxy{
		Logger:                testenv.NewLogger(t),
		AuthorizationOverride: "upstream-token",
		Headers: []ConfiguredHeader{
			{Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: "Authorization", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"Bearer upstream-token"}, remoteReq.Header.Values("Authorization"))
	require.Equal(t, []string{"Bearer caller-upstream"}, remoteReq.Header.Values("X-Upstream-Token"))
}

// A sent configured header replaces every client spelling of its name, so an
// upstream folding '_' into '-' cannot read the client's value instead.
func TestApplyRequestHeadersRemoteSentHeaderClearsClientSpellings(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header["X_api_key"] = []string{"client-supplied"}
	userReq.Header.Set("X-Api-Key", "client-supplied")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "X-Api-Key", StaticValue: "operator-credential", ValueFromRequestHeader: "", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"operator-credential"}, remoteReq.Header.Values("X-Api-Key"))
	require.Empty(t, remoteReq.Header.Values("X_api_key"))
}

// A padded optional row is suppressed, and the client's own value under the
// trimmed name, the header it would actually send, is cleared with it.
func TestApplyRequestHeadersRemotePaddedOptionalRowClearsClientValue(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("X-Api-Key", "client-supplied")
	userReq.Header.Set("Mcp-Method", "tools/call")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: " X-Api-Key ", StaticValue: "operator-credential", ValueFromRequestHeader: "", IsRequired: false},
			{Name: " Mcp-Method ", StaticValue: "configured", ValueFromRequestHeader: "", IsRequired: false},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Empty(t, remoteReq.Header.Values("X-Api-Key"))
	require.Equal(t, []string{"tools/call"}, remoteReq.Header.Values("Mcp-Method"), "the client's protocol header is never cleared")
}

// A suppressed row clears the client's value under every spelling the policy
// reads as the same name, not just the canonical one.
func TestApplyRequestHeadersRemoteSuppressionClearsUnderscoreSpelling(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header["X_Upstream_Token"] = []string{"client-underscore"}
	userReq.Header["x-upstream-token"] = []string{"client-lowercase"}
	userReq.Header.Set("Gram-Key", "synthetic-api-key")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: "Gram-Key", IsRequired: false},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	for name := range remoteReq.Header {
		require.NotEqual(t, "x-upstream-token", headerKey(name), "client spelling %q survived", name)
	}
}

func TestApplyRequestHeadersForwardsCallerUserAgent(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	userReq.Header.Set("User-Agent", "claude-code/2.1.0")
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{Logger: testenv.NewLogger(t), Headers: nil, AuthorizationOverride: ""}

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{"claude-code/2.1.0"}, remoteReq.Header.Values("User-Agent"))
}

func TestApplyRequestHeadersDefaultsMissingUserAgent(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	userReq.Header.Del("User-Agent")
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{Logger: testenv.NewLogger(t), Headers: nil, AuthorizationOverride: ""}

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, constants.UserAgent, remoteReq.Header.Get("User-Agent"))
}

package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
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

// TestApplyRequestHeadersTunneledRoutingSeparated pins the combined contract
// for tunneled servers: Speakeasy credentials are stripped from the copy, a
// configured row cannot claim a tunnel transport field, and the tunnel's own
// fields arrive as routing headers that bypass the configured-header policy.
func TestApplyRequestHeadersTunneledRoutingSeparated(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Gram-Key", "synthetic-api-key")
	userReq.Header.Set("Authorization", "Bearer synthetic")
	p := &Proxy{
		Logger:       testenv.NewLogger(t),
		HeaderPolicy: HeaderPolicyTunneled,
		Headers: []ConfiguredHeader{
			{Name: "X-Gram-Tunnel-Id", StaticValue: "configured", ValueFromRequestHeader: "", IsRequired: false},
		},
		RoutingHeaders: []ConfiguredHeader{
			{Name: "X-Gram-Tunnel-Id", StaticValue: "tunnel-1", ValueFromRequestHeader: "", IsRequired: true},
			{Name: McpSessionIDHeader, StaticValue: "backend-session", ValueFromRequestHeader: "", IsRequired: true},
		},
	}
	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

	require.Empty(t, remoteReq.Header.Get("Gram-Key"))
	require.Empty(t, remoteReq.Header.Get("Authorization"))
	require.Equal(t, []string{"tunnel-1"}, remoteReq.Header.Values("X-Gram-Tunnel-Id"))
	require.Equal(t, "backend-session", remoteReq.Header.Get(McpSessionIDHeader))

	p.Headers[0].IsRequired = true
	_, remoteReq = newRemotePolicyRequests(t)
	require.Error(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq), "a required row claiming a tunnel field is refused")
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

// Synthetic credentials. Assertions compare against these constants only.
const (
	syntheticAPIKey       = "synthetic-api-key"
	syntheticSession      = "synthetic-session"
	syntheticChatSession  = "synthetic-chat-session"
	syntheticBearer       = "Bearer synthetic-speakeasy-bearer"
	syntheticCookie       = "gram_session=synthetic-cookie"
	syntheticForwardToken = "synthetic-forward-token"
)

func TestNormalizeHeaderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "canonicalizes case", raw: "x-jamf-tenant", want: "X-Jamf-Tenant"},
		{name: "trims ascii spaces", raw: "  X-Region ", want: "X-Region"},
		{name: "keeps underscores", raw: "x_custom", want: "X_custom"},
		{name: "rejects empty", raw: "", wantErr: true},
		{name: "rejects spaces only", raw: "   ", wantErr: true},
		{name: "rejects inner space", raw: "X Region", wantErr: true},
		{name: "rejects colon", raw: "X-Region:", wantErr: true},
		{name: "rejects CRLF injection", raw: "X-Region\r\nX-Injected", wantErr: true},
		{name: "rejects trailing LF", raw: "X-Region\n", wantErr: true},
		{name: "rejects tab", raw: "\tX-Region", wantErr: true},
		{name: "rejects NUL", raw: "X-\x00Region", wantErr: true},
		{name: "rejects DEL", raw: "X-Region\x7f", wantErr: true},
		{name: "rejects non-ascii", raw: "X-Régión", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeHeaderName(tc.raw)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrInvalidHeaderName)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestValidateHeaderValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "plain", value: "tenant-1"},
		{name: "tab allowed", value: "a\tb"},
		{name: "CR rejected", value: "a\rb", wantErr: true},
		{name: "LF rejected", value: "a\nX-Injected: 1", wantErr: true},
		{name: "NUL rejected", value: "a\x00b", wantErr: true},
		{name: "other control rejected", value: "a\x01b", wantErr: true},
		{name: "DEL rejected", value: "a\x7fb", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateHeaderValue(tc.value)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrInvalidHeaderValue)
				require.NotContains(t, err.Error(), tc.value)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCheckTunneledHeaderReservedDestinations(t *testing.T) {
	t.Parallel()

	reserved := []string{
		"X-Speakeasy-Identity", "X_speakeasy_identity",
		"X-Gram-Tunnel-Id", "X-Gram-Tunnel-Forward-Token", "X-Gram-Tunnel-Consumer-Session",
		"X-Gram-Tunnel-Agent-Session", "X-Gram-Tunnel-Require-Active", "X_gram_tunnel_id",
		"X-Gram-Agent-Version", "X-Gram-Scope-Override", "X-Gram-Source",
		"Gram-Key", "Gram-Session", "Gram-Chat-Session", "Gram-Project", "Gram-Consent-State", "Gram_key",
		"Speakeasy-AI-Key", "Speakeasy-AI-Session", "Speakeasy-AI-Chat-Session", "Speakeasy_AI_Project",
		"Cookie", "Set-Cookie", "Proxy-Authorization",
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Te", "Trailer", "Transfer-Encoding",
		"Upgrade", "Content-Length", "Host", "Accept-Encoding",
		"Mcp-Session-Id", "Mcp-Protocol-Version", "Mcp-Method", "Mcp-Name", "Mcp-Param-Region", "Last-Event-Id",
	}
	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := CheckTunneledHeader(ConfiguredHeader{IsRequired: false, Name: name, StaticValue: "x", ValueFromRequestHeader: ""})
			require.ErrorIs(t, err, ErrReservedHeader)
		})
	}

	allowed := []string{"Authorization", "X-Jamf-Tenant", "X-Api-Key", "Accept-Language"}
	for _, name := range allowed {
		t.Run("allowed "+name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, CheckTunneledHeader(ConfiguredHeader{IsRequired: false, Name: name, StaticValue: "x", ValueFromRequestHeader: ""}))
		})
	}
}

func TestCheckTunneledHeaderProtectedSources(t *testing.T) {
	t.Parallel()

	protected := []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie",
		"Gram-Key", "Gram-Session", "Gram-Chat-Session", "Gram-Project", "Gram-Consent-Csrf", "Gram_chat_session",
		"Speakeasy-AI-Key", "Speakeasy-AI-Session", "Speakeasy-AI-Chat-Session", "speakeasy_ai_key",
		"X-Speakeasy-Identity", "X_speakeasy_identity", "X-Gram-Tunnel-Forward-Token", "X-Gram-Agent-Version",
	}
	for _, source := range protected {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			err := CheckTunneledHeader(ConfiguredHeader{IsRequired: false, Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: source})
			require.ErrorIs(t, err, ErrReservedHeader)
		})
	}

	require.NoError(t, CheckTunneledHeader(ConfiguredHeader{IsRequired: false, Name: "X-Region", StaticValue: "", ValueFromRequestHeader: "X-Client-Region"}))
}

func TestCheckTunneledHeaderRejectsControlCharactersInStaticValue(t *testing.T) {
	t.Parallel()

	err := CheckTunneledHeader(ConfiguredHeader{IsRequired: false, Name: "X-Tenant", StaticValue: "a\r\nX-Injected: 1", ValueFromRequestHeader: ""})
	require.ErrorIs(t, err, ErrInvalidHeaderValue)
}

// credentialBearingRequest builds an inbound request that carries every kind
// of Speakeasy credential and internal field a caller could send, plus one
// ordinary client header and one standard MCP request header.
func credentialBearingRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	req.Header.Set("Authorization", syntheticBearer)
	req.Header.Set("Gram-Key", syntheticAPIKey)
	req.Header.Set("Gram-Session", syntheticSession)
	req.Header.Set("Gram-Chat-Session", syntheticChatSession)
	req.Header.Set("Gram-Project", "synthetic-project")
	req.Header.Set("Cookie", syntheticCookie)
	req.Header.Set("X-Gram-Tunnel-Require-Active", "1")
	req.Header.Set("X-Gram-Scope-Override", "client-supplied")
	req.Header.Set("X-Gram-Tunnel-Forward-Token", "client-supplied")
	req.Header["X_speakeasy_identity"] = []string{"client-supplied"}
	req.Header["gram_key"] = []string{syntheticAPIKey}
	req.Header.Set("Speakeasy-AI-Key", syntheticAPIKey)
	req.Header.Set("Speakeasy-AI-Chat-Session", syntheticChatSession)
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("X-Client-Region", "eu")
	req.Header.Set("X-Client-Ok", "kept")
	return req
}

func policyProxy(t *testing.T, policy HeaderPolicy, headers []ConfiguredHeader) *Proxy {
	t.Helper()
	return &Proxy{
		Logger:       testenv.NewLogger(t),
		Headers:      headers,
		HeaderPolicy: policy,
	}
}

// exfiltrationRows try to copy each inbound credential into a harmless name.
func exfiltrationRows() []ConfiguredHeader {
	sources := []string{"Authorization", "Gram-Key", "Gram-Session", "Gram-Chat-Session", "Cookie", "X_speakeasy_identity", "gram_key", "Speakeasy-AI-Key"}
	rows := make([]ConfiguredHeader, 0, len(sources))
	for i, source := range sources {
		rows = append(rows, ConfiguredHeader{
			IsRequired:             false,
			Name:                   "X-Upstream-" + string(rune('A'+i)),
			StaticValue:            "",
			ValueFromRequestHeader: source,
		})
	}
	return rows
}

// TestApplyRequestHeadersTunneledStripsSpeakeasyCredentials covers both the
// raw client-header copy and configured pass-through rows: under the tunneled
// policy no inbound Speakeasy credential reaches the upstream under any name.
func TestApplyRequestHeadersTunneledStripsSpeakeasyCredentials(t *testing.T) {
	t.Parallel()

	userReq := credentialBearingRequest(t)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, exfiltrationRows())

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

	for name, values := range remoteReq.Header {
		for _, v := range values {
			for _, secret := range []string{syntheticAPIKey, syntheticSession, syntheticChatSession, syntheticBearer, syntheticCookie, "client-supplied"} {
				require.NotEqual(t, secret, v, "header %s carries an inbound credential", name)
			}
		}
	}
	for _, name := range []string{"Authorization", "Gram-Key", "Gram-Session", "Gram-Chat-Session", "Gram-Project", "Speakeasy-AI-Key", "Speakeasy-AI-Chat-Session", "Cookie", "X-Gram-Tunnel-Require-Active", "X-Gram-Tunnel-Forward-Token", "X_speakeasy_identity", "gram_key"} {
		require.Empty(t, remoteReq.Header.Values(name), name)
	}
	require.Equal(t, "tools/call", remoteReq.Header.Get("Mcp-Method"))
	require.Equal(t, "kept", remoteReq.Header.Get("X-Client-Ok"))
}

// TestApplyRequestHeadersRemoteHardenedPolicy pins the combined contract for
// remote servers: the copy drops every Speakeasy credential, session,
// assertion and tunnel transport field, and pass-through rows may not read
// them. Authorization stays an allowed source so a caller can forward its own
// upstream credential, and internal X-Gram-* controls that are not tunnel
// fields are protected only under the tunneled policy.
func TestApplyRequestHeadersRemoteHardenedPolicy(t *testing.T) {
	t.Parallel()

	userReq := credentialBearingRequest(t)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyRemote, exfiltrationRows())

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

	for _, name := range []string{"Authorization", "Cookie", "Gram-Key", "Gram-Session", "Gram-Chat-Session", "Gram-Project", "X-Gram-Tunnel-Require-Active", "X-Gram-Tunnel-Forward-Token", "X_speakeasy_identity", "gram_key", "Speakeasy-AI-Key", "Speakeasy-AI-Chat-Session"} {
		require.Empty(t, remoteReq.Header.Values(name), "%s must not reach a remote upstream", name)
	}
	require.Equal(t, syntheticBearer, remoteReq.Header.Get("X-Upstream-A"), "Authorization is an allowed remote pass-through source")
	for i := 1; i < len(exfiltrationRows()); i++ {
		name := "X-Upstream-" + string(rune('A'+i))
		require.Empty(t, remoteReq.Header.Values(name), "%s reads a protected source", name)
	}
	require.Equal(t, "tools/call", remoteReq.Header.Get("Mcp-Method"))
	require.Equal(t, "kept", remoteReq.Header.Get("X-Client-Ok"))
	require.Equal(t, "client-supplied", remoteReq.Header.Get("X-Gram-Scope-Override"), "only the tunneled policy protects the whole X-Gram-* family")
}

func TestApplyRequestHeadersTunneledShadowedAuthorizationImposesNoRequirement(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{{
		IsRequired:             true,
		Name:                   "Authorization",
		StaticValue:            "",
		ValueFromRequestHeader: "X-Service-Token",
	}})
	p.AuthorizationOverride = "upstream-token"

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "Bearer upstream-token", remoteReq.Header.Get("Authorization"))
}

func TestApplyRequestHeadersTunneledStaticAuthorizationWithoutOverride(t *testing.T) {
	t.Parallel()

	userReq := credentialBearingRequest(t)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{{
		IsRequired:             true,
		Name:                   "Authorization",
		StaticValue:            "Basic service-credential",
		ValueFromRequestHeader: "",
	}})

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "Basic service-credential", remoteReq.Header.Get("Authorization"))
}

// An optional row that fails the policy must not leave the client's own value
// under its destination name standing in for it.
func TestApplyRequestHeadersTunneledSuppressedRowClearsDestination(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	userReq.Header.Set("X-Upstream-Token", "attacker-supplied")
	userReq.Header.Set("Gram-Key", syntheticAPIKey)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{{
		IsRequired:             false,
		Name:                   "X-Upstream-Token",
		StaticValue:            "",
		ValueFromRequestHeader: "Gram-Key",
	}})

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Empty(t, remoteReq.Header.Values("X-Upstream-Token"))
}

// A stored row naming a protected protocol field is ignored without touching
// the client's legitimate value for that field.
func TestApplyRequestHeadersTunneledReservedRowLeavesProtocolField(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	userReq.Header.Set("Mcp-Method", "tools/list")
	userReq.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{
		{IsRequired: false, Name: "Mcp-Method", StaticValue: "tools/call", ValueFromRequestHeader: ""},
		{IsRequired: false, Name: "Mcp-Protocol-Version", StaticValue: "", ValueFromRequestHeader: ""},
	})

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, "tools/list", remoteReq.Header.Get("Mcp-Method"))
	require.Equal(t, "2025-06-18", remoteReq.Header.Get("Mcp-Protocol-Version"))
}

func TestApplyRequestHeadersTunneledRequiredInvalidRowRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  ConfiguredHeader
	}{
		{name: "protected source", row: ConfiguredHeader{IsRequired: true, Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: "Gram-Chat-Session"}},
		{name: "reserved destination", row: ConfiguredHeader{IsRequired: true, Name: "X-Gram-Tunnel-Id", StaticValue: "spoofed", ValueFromRequestHeader: ""}},
		{name: "noncanonical name", row: ConfiguredHeader{IsRequired: true, Name: "x-tenant", StaticValue: "a", ValueFromRequestHeader: ""}},
		{name: "control bytes in stored value", row: ConfiguredHeader{IsRequired: true, Name: "X-Tenant", StaticValue: "stored\r\nX-Injected: 1", ValueFromRequestHeader: ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			userReq.Header.Set("Gram-Chat-Session", syntheticChatSession)
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
			p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{tc.row})

			err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, oops.CodeBadRequest, shareable.Code)
			require.NotContains(t, err.Error(), syntheticChatSession)
			require.NotContains(t, err.Error(), "stored\r\n")
			require.NotContains(t, err.Error(), "spoofed")
		})
	}
}

func TestApplyRequestHeadersTunneledResolvedValueWithControlBytes(t *testing.T) {
	t.Parallel()

	newRequest := func(t *testing.T) *http.Request {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
		req.Header["X-Client-Region"] = []string{"eu\x00west"}
		return req
	}
	row := ConfiguredHeader{IsRequired: false, Name: "X-Region", StaticValue: "", ValueFromRequestHeader: "X-Client-Region"}

	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	require.NoError(t, policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{row}).applyRequestHeaders(t.Context(), newRequest(t), remoteReq))
	require.Empty(t, remoteReq.Header.Values("X-Region"))

	row.IsRequired = true
	remoteReq = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	err := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{row}).applyRequestHeaders(t.Context(), newRequest(t), remoteReq)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "west")
}

func TestApplyRequestHeadersTunneledRequiredPassThroughMissing(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{{IsRequired: true, Name: "X-Region", StaticValue: "", ValueFromRequestHeader: "X-Client-Region"}})

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
}

func TestApplyRequestHeadersRoutingHeadersWin(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	userReq.Header.Set("Mcp-Session-Id", "speakeasy-minted")
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{
		{IsRequired: false, Name: "X-Gram-Tunnel-Forward-Token", StaticValue: "operator-spoof", ValueFromRequestHeader: ""},
		{IsRequired: false, Name: "X-Tenant", StaticValue: "tenant-1", ValueFromRequestHeader: ""},
	})
	p.RoutingHeaders = []ConfiguredHeader{
		{IsRequired: true, Name: "X-Gram-Tunnel-Forward-Token", StaticValue: syntheticForwardToken, ValueFromRequestHeader: ""},
		{IsRequired: true, Name: McpSessionIDHeader, StaticValue: "backend-session", ValueFromRequestHeader: ""},
	}

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Equal(t, []string{syntheticForwardToken}, remoteReq.Header.Values("X-Gram-Tunnel-Forward-Token"))
	require.Equal(t, []string{"backend-session"}, remoteReq.Header.Values(McpSessionIDHeader))
	require.Equal(t, "tenant-1", remoteReq.Header.Get("X-Tenant"))
}

func TestStripConfiguredCredentialsIncludesRoutingHeaders(t *testing.T) {
	t.Parallel()

	p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{
		{IsRequired: false, Name: "X-Tenant", StaticValue: "tenant-1", ValueFromRequestHeader: ""},
		{IsRequired: false, Name: "X-Region", StaticValue: "", ValueFromRequestHeader: "X-Client-Region"},
	})
	p.RoutingHeaders = []ConfiguredHeader{{IsRequired: true, Name: "X-Gram-Tunnel-Forward-Token", StaticValue: syntheticForwardToken, ValueFromRequestHeader: ""}}

	header := http.Header{}
	header.Set("X-Tenant", "tenant-1")
	header.Set("X-Region", "eu")
	header.Set("X-Client-Region", "eu")
	header.Set("X-Gram-Tunnel-Forward-Token", syntheticForwardToken)
	header.Set("X-Other", "kept")
	p.stripConfiguredCredentials(header)

	require.Equal(t, http.Header{"X-Other": []string{"kept"}}, header)
}

// TestForwardRequestWithRetryKeepsConfiguredHeaders: a tunnel reroute
// replaces Speakeasy's routing fields but keeps every configured header,
// static or passed through, and never resurrects a routing field the retry
// dropped.
func TestForwardRequestWithRetryKeepsConfiguredHeaders(t *testing.T) {
	t.Parallel()

	seen := make(chan http.Header, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		if r.Header.Get("X-Gram-Tunnel-Forward-Token") != "second" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	p := &Proxy{
		GuardianPolicy:       policy,
		Logger:               testenv.NewLogger(t),
		Tracer:               testenv.NewTracerProvider(t).Tracer("test"),
		NonStreamingTimeout:  5 * time.Second,
		StreamingTimeout:     5 * time.Second,
		MaxBufferedBodyBytes: DefaultMaxBufferedBodyBytes,
		RemoteURL:            upstream.URL,
		HeaderPolicy:         HeaderPolicyTunneled,
		Headers: []ConfiguredHeader{
			{IsRequired: true, Name: "X-Tenant", StaticValue: "decrypted-secret", ValueFromRequestHeader: ""},
			{IsRequired: true, Name: "X-Region", StaticValue: "", ValueFromRequestHeader: "X-Client-Region"},
		},
		RoutingHeaders: []ConfiguredHeader{
			{IsRequired: true, Name: "X-Gram-Tunnel-Forward-Token", StaticValue: "first", ValueFromRequestHeader: ""},
			{IsRequired: false, Name: "X-Gram-Tunnel-Consumer-Session", StaticValue: "affinity", ValueFromRequestHeader: ""},
		},
		AuthorizationOverride: "upstream-token",
		UpstreamResponseRetryer: func(_ context.Context, resp *http.Response) (*UpstreamResponseRetry, error) {
			if resp.StatusCode != http.StatusBadGateway {
				return nil, nil
			}
			return &UpstreamResponseRetry{
				RemoteURL:             "",
				RoutingHeaders:        []ConfiguredHeader{{IsRequired: true, Name: "X-Gram-Tunnel-Forward-Token", StaticValue: "second", ValueFromRequestHeader: ""}},
				AuthorizationOverride: "",
			}, nil
		},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://gram.local/mcp", nil)
	req.Header.Set("X-Client-Region", "eu")
	_, upstreamResp, err := p.forwardRequestWithRetry(t.Context(), req, func() io.Reader { return strings.NewReader("") }, nil)
	require.NoError(t, err)
	defer func() { _ = upstreamResp.Body.Close() }()
	require.Equal(t, http.StatusOK, upstreamResp.StatusCode)

	first, second := <-seen, <-seen
	require.Equal(t, "first", first.Get("X-Gram-Tunnel-Forward-Token"))
	require.Equal(t, "affinity", first.Get("X-Gram-Tunnel-Consumer-Session"))
	for _, got := range []http.Header{first, second} {
		require.Equal(t, "decrypted-secret", got.Get("X-Tenant"))
		require.Equal(t, "eu", got.Get("X-Region"))
		require.Equal(t, "Bearer upstream-token", got.Get("Authorization"))
	}
	require.Equal(t, "second", second.Get("X-Gram-Tunnel-Forward-Token"))
	require.Empty(t, second.Values("X-Gram-Tunnel-Consumer-Session"))
}

// A configured header owns every spelling of its name an upstream might fold
// together: the caller's X_Tenant cannot reach the upstream beside it.
func TestApplyRequestHeadersTunneledConfiguredHeaderOwnsFoldedSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		row  ConfiguredHeader
		want []string
	}{
		{name: "static value", row: ConfiguredHeader{IsRequired: false, Name: "X-Tenant", StaticValue: "tenant-1", ValueFromRequestHeader: ""}, want: []string{"tenant-1"}},
		{name: "empty optional pass-through", row: ConfiguredHeader{IsRequired: false, Name: "X-Tenant", StaticValue: "", ValueFromRequestHeader: "X-Client-Tenant"}, want: nil},
		{name: "suppressed invalid row", row: ConfiguredHeader{IsRequired: false, Name: "X-Tenant", StaticValue: "", ValueFromRequestHeader: "Gram-Key"}, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			userReq.Header["X_tenant"] = []string{"attacker"}
			userReq.Header.Set("Gram-Key", syntheticAPIKey)
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
			p := policyProxy(t, HeaderPolicyTunneled, []ConfiguredHeader{tc.row})

			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Empty(t, remoteReq.Header["X_tenant"])
			require.Equal(t, tc.want, remoteReq.Header.Values("X-Tenant"))
		})
	}
}

// A routing header owns every spelling of its name an upstream might fold
// together, so a caller cannot slip a second session id beside the pinned one.
func TestApplyRequestHeadersRoutingHeaderOwnsFoldedSpellings(t *testing.T) {
	t.Parallel()

	for _, policy := range []HeaderPolicy{HeaderPolicyRemote, HeaderPolicyTunneled} {
		t.Run(string(policy), func(t *testing.T) {
			t.Parallel()
			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			userReq.Header["Mcp_session_id"] = []string{"guessed"}
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
			p := policyProxy(t, policy, nil)
			p.RoutingHeaders = []ConfiguredHeader{{IsRequired: true, Name: McpSessionIDHeader, StaticValue: "backend-session", ValueFromRequestHeader: ""}}

			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Empty(t, remoteReq.Header["Mcp_session_id"])
			require.Equal(t, []string{"backend-session"}, remoteReq.Header.Values(McpSessionIDHeader))
		})
	}
}

package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// protectedInboundFixture is one synthetic value for every inbound header
// family the remote policy protects, keyed by the spelling a client sends.
var protectedInboundFixture = map[string]string{
	"Gram-Key":                     "synthetic-api-key",
	"Gram-Session":                 "synthetic-session",
	"Gram-Chat-Session":            "synthetic-chat-session",
	"Gram-Project":                 "synthetic-project",
	"Gram-Consent-State":           "synthetic-consent",
	"Gram_Key":                     "synthetic-alias-key",
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
	require.Len(t, remoteReq.Header, 2, "only the allowed client headers remain")
	require.Equal(t, "allowed", remoteReq.Header.Get("X-Client-Trace"))
	require.Equal(t, "2025-06-18", remoteReq.Header.Get("Mcp-Protocol-Version"))
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

	for _, source := range []string{"Authorization", "authorization", "GRAM-KEY", "gram_chat_session", "X-Gram-Tunnel-Forward-Token", "x_speakeasy_identity", "Cookie"} {
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
	require.Contains(t, message, "separate request header")
	require.NotContains(t, message, "synthetic-api-key")
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

func TestApplyRequestHeadersRemoteRejectsRequiredAuthorizationPassThroughWithoutOverride(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Authorization", "Bearer synthetic-speakeasy-token")
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{Name: "Authorization", StaticValue: "", ValueFromRequestHeader: "Authorization", IsRequired: true},
		},
	}
	requireBadRequest(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
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

// Rows arrive ordered by stored name, and sequential Set keeps the last one,
// as before names were canonicalized. Normalization must not pick a different
// credential.
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
		{name: "authorization source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "Authorization"}, wantErr: ErrProtectedSource},
		{name: "gram key source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "Gram-Key"}, wantErr: ErrProtectedSource},
		{name: "mixed case gram source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "gRaM-cHaT-sEsSiOn"}, wantErr: ErrProtectedSource},
		{name: "tunnel source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X-Gram-Tunnel-Id"}, wantErr: ErrProtectedSource},
		{name: "agent version source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X-Gram-Agent-Version"}, wantErr: ErrProtectedSource},
		{name: "assertion alias source", header: ConfiguredHeader{Name: "X-Upstream-Token", ValueFromRequestHeader: "X_Speakeasy_Identity"}, wantErr: ErrProtectedSource},
		{name: "set-cookie destination", header: ConfiguredHeader{Name: "Set-Cookie", StaticValue: "a=b"}, wantErr: ErrReservedHeader},
		{name: "proxy-authorization destination", header: ConfiguredHeader{Name: "proxy-authorization", StaticValue: "Basic x"}, wantErr: ErrReservedHeader},
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

// A resolved upstream token owns Authorization only. A custom header reading
// a protected source is still refused with one, and its remediation does not
// pretend upstream OAuth would supply it.
func TestApplyRequestHeadersRemoteOverrideDoesNotRescueCustomDestination(t *testing.T) {
	t.Parallel()

	userReq, remoteReq := newRemotePolicyRequests(t)
	userReq.Header.Set("Authorization", "Bearer synthetic-speakeasy-token")
	p := &Proxy{
		Logger:                testenv.NewLogger(t),
		AuthorizationOverride: "upstream-token",
		Headers: []ConfiguredHeader{
			{Name: "X-Upstream-Token", StaticValue: "", ValueFromRequestHeader: "Authorization", IsRequired: true},
		},
	}
	message := requireBadRequest(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
	require.Contains(t, message, "does not replace this header")
	require.NotContains(t, message, "connect the server's upstream OAuth")
}

func TestProtectedSourceRemediationOffersOAuthOnlyForAuthorization(t *testing.T) {
	t.Parallel()

	require.Contains(t, ProtectedSourceRemediation("authorization"), "connect the server's upstream OAuth")
	require.NotContains(t, ProtectedSourceRemediation("X-Upstream-Token"), "connect the server's upstream OAuth")
}

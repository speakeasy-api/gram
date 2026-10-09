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

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

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

// TestApplyRequestHeadersRemoteForwardingUnchanged pins the remote policy to
// its existing behaviour for the same input. It documents current behaviour;
// it does not endorse it.
func TestApplyRequestHeadersRemoteForwardingUnchanged(t *testing.T) {
	t.Parallel()

	userReq := credentialBearingRequest(t)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := policyProxy(t, HeaderPolicyRemote, exfiltrationRows())

	require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))

	// Dropped by the long-standing remote skip list and source denylist.
	require.Empty(t, remoteReq.Header.Get("Authorization"))
	require.Empty(t, remoteReq.Header.Get("Cookie"))
	require.Empty(t, remoteReq.Header.Values("X_speakeasy_identity"))
	require.Empty(t, remoteReq.Header.Get("X-Upstream-E"), "Cookie is a denied pass-through source")
	require.Empty(t, remoteReq.Header.Get("X-Upstream-F"), "assertion aliases are never a source")
	// Still forwarded under the remote policy.
	require.Equal(t, syntheticAPIKey, remoteReq.Header.Get("Gram-Key"))
	require.Equal(t, syntheticChatSession, remoteReq.Header.Get("Gram-Chat-Session"))
	require.Equal(t, "1", remoteReq.Header.Get("X-Gram-Tunnel-Require-Active"))
	require.Equal(t, syntheticBearer, remoteReq.Header.Get("X-Upstream-A"))
	require.Equal(t, syntheticAPIKey, remoteReq.Header.Get("X-Upstream-B"))
	require.Equal(t, "tools/call", remoteReq.Header.Get("Mcp-Method"))
	require.Equal(t, "kept", remoteReq.Header.Get("X-Client-Ok"))
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

package proxy

import (
	"context"
	"errors"
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

// syntheticEnvValue is a synthetic environment header value. Assertions compare
// against it only.
const syntheticEnvValue = "synthetic-env-value"

func inspectOne(t *testing.T, name, value string) EnvironmentHeaderInspection {
	t.Helper()
	inspections := InspectEnvironmentHeaders([]EnvironmentHeaderEntry{{Name: name, Value: value, Undecryptable: false}})
	require.Len(t, inspections, 1)
	return inspections[0]
}

func TestInspectEnvironmentHeadersNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entry      string
		wantHeader string
		wantStatus EnvironmentHeaderStatus
	}{
		{name: "canonicalizes casing", entry: "MCP_HEADER_x-instance-url", wantHeader: "X-Instance-Url", wantStatus: EnvironmentHeaderMapped},
		{name: "keeps underscores", entry: "MCP_HEADER_X_Instance", wantHeader: "X_instance", wantStatus: EnvironmentHeaderMapped},
		{name: "authorization is allowed", entry: "MCP_HEADER_Authorization", wantHeader: "Authorization", wantStatus: EnvironmentHeaderMapped},
		{name: "empty suffix", entry: "MCP_HEADER_", wantHeader: "", wantStatus: EnvironmentHeaderInvalidName},
		{name: "leading space", entry: "MCP_HEADER_ X-Foo", wantHeader: "", wantStatus: EnvironmentHeaderInvalidName},
		{name: "trailing space", entry: "MCP_HEADER_X-Foo ", wantHeader: "", wantStatus: EnvironmentHeaderInvalidName},
		{name: "control character", entry: "MCP_HEADER_X-Foo\r\nX-Bar", wantHeader: "", wantStatus: EnvironmentHeaderInvalidName},
		{name: "separator character", entry: "MCP_HEADER_X:Foo", wantHeader: "", wantStatus: EnvironmentHeaderInvalidName},
		{name: "speakeasy credential", entry: "MCP_HEADER_Gram-Key", wantHeader: "Gram-Key", wantStatus: EnvironmentHeaderReserved},
		{name: "speakeasy ai credential", entry: "MCP_HEADER_Speakeasy-AI-Key", wantHeader: "Speakeasy-Ai-Key", wantStatus: EnvironmentHeaderReserved},
		{name: "internal family", entry: "MCP_HEADER_X-Gram-Tunnel-Forward-Token", wantHeader: "X-Gram-Tunnel-Forward-Token", wantStatus: EnvironmentHeaderReserved},
		{name: "caller assertion", entry: "MCP_HEADER_X-Speakeasy-Identity", wantHeader: "X-Speakeasy-Identity", wantStatus: EnvironmentHeaderReserved},
		{name: "cookie", entry: "MCP_HEADER_Cookie", wantHeader: "Cookie", wantStatus: EnvironmentHeaderReserved},
		{name: "set-cookie", entry: "MCP_HEADER_Set-Cookie", wantHeader: "Set-Cookie", wantStatus: EnvironmentHeaderReserved},
		{name: "proxy authorization", entry: "MCP_HEADER_Proxy-Authorization", wantHeader: "Proxy-Authorization", wantStatus: EnvironmentHeaderReserved},
		{name: "framing", entry: "MCP_HEADER_Content-Length", wantHeader: "Content-Length", wantStatus: EnvironmentHeaderReserved},
		{name: "host", entry: "MCP_HEADER_Host", wantHeader: "Host", wantStatus: EnvironmentHeaderReserved},
		{name: "mcp session", entry: "MCP_HEADER_Mcp-Session-Id", wantHeader: "Mcp-Session-Id", wantStatus: EnvironmentHeaderReserved},
		{name: "mcp standard request header", entry: "MCP_HEADER_Mcp-Method", wantHeader: "Mcp-Method", wantStatus: EnvironmentHeaderReserved},
		{name: "underscore spelling of reserved", entry: "MCP_HEADER_Gram_Key", wantHeader: "Gram_key", wantStatus: EnvironmentHeaderReserved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := inspectOne(t, tc.entry, syntheticEnvValue)
			require.Equal(t, tc.entry, got.EntryName)
			require.Equal(t, tc.wantHeader, got.HeaderName)
			require.Equal(t, tc.wantStatus, got.Status)
		})
	}
}

func TestInspectEnvironmentHeadersValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		wantStatus EnvironmentHeaderStatus
	}{
		{name: "plain value", value: syntheticEnvValue, wantStatus: EnvironmentHeaderMapped},
		{name: "inner tab", value: "a\tb", wantStatus: EnvironmentHeaderMapped},
		{name: "empty", value: "", wantStatus: EnvironmentHeaderEmptyValue},
		{name: "spaces only", value: "   ", wantStatus: EnvironmentHeaderEmptyValue},
		{name: "tabs and spaces only", value: " \t ", wantStatus: EnvironmentHeaderEmptyValue},
		{name: "crlf", value: "a\r\nX-Injected: 1", wantStatus: EnvironmentHeaderInvalidValue},
		{name: "nul", value: "a\x00b", wantStatus: EnvironmentHeaderInvalidValue},
		{name: "del", value: "a\x7fb", wantStatus: EnvironmentHeaderInvalidValue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.wantStatus, inspectOne(t, "MCP_HEADER_X-Foo", tc.value).Status)
		})
	}
}

func TestInspectEnvironmentHeadersIgnoresUnprefixedEntries(t *testing.T) {
	t.Parallel()

	inspections := InspectEnvironmentHeaders([]EnvironmentHeaderEntry{
		{Name: "API_KEY", Value: "synthetic-unrelated", Undecryptable: false},
		{Name: "mcp_header_X-Foo", Value: "synthetic-unrelated", Undecryptable: false},
		{Name: "HEADER_X-Foo", Value: "synthetic-unrelated", Undecryptable: false},
		{Name: "Mcp_Header_X-Foo", Value: "synthetic-unrelated", Undecryptable: true},
		{Name: "MCP_HEADER_X-Foo", Value: syntheticEnvValue, Undecryptable: false},
	})
	require.Len(t, inspections, 1)
	require.Equal(t, "MCP_HEADER_X-Foo", inspections[0].EntryName)
}

func TestInspectEnvironmentHeadersDuplicates(t *testing.T) {
	t.Parallel()

	inspections := InspectEnvironmentHeaders([]EnvironmentHeaderEntry{
		{Name: "MCP_HEADER_X-Foo", Value: syntheticEnvValue, Undecryptable: false},
		{Name: "MCP_HEADER_x-foo", Value: syntheticEnvValue, Undecryptable: false},
		{Name: "MCP_HEADER_X_Foo", Value: syntheticEnvValue, Undecryptable: false},
		{Name: "MCP_HEADER_X-Bar", Value: syntheticEnvValue, Undecryptable: false},
	})
	require.Len(t, inspections, 4)
	for _, inspection := range inspections[:3] {
		require.Equal(t, EnvironmentHeaderDuplicate, inspection.Status, inspection.EntryName)
	}
	require.Equal(t, EnvironmentHeaderMapped, inspections[3].Status)
}

func TestInspectEnvironmentHeadersUndecryptable(t *testing.T) {
	t.Parallel()

	inspections := InspectEnvironmentHeaders([]EnvironmentHeaderEntry{{Name: "MCP_HEADER_X-Foo", Value: "", Undecryptable: true}})
	require.Len(t, inspections, 1)
	require.Equal(t, EnvironmentHeaderUndecryptable, inspections[0].Status)
	require.Equal(t, "X-Foo", inspections[0].HeaderName)
}

func TestEnvironmentHeaderRowsMapsValidEntries(t *testing.T) {
	t.Parallel()

	rows, err := EnvironmentHeaderRows(InspectEnvironmentHeaders([]EnvironmentHeaderEntry{
		{Name: "MCP_HEADER_x-instance-url", Value: syntheticEnvValue, Undecryptable: false},
	}))
	require.NoError(t, err)
	require.Equal(t, []ConfiguredHeader{{IsRequired: true, Name: "X-Instance-Url", StaticValue: syntheticEnvValue, ValueFromRequestHeader: ""}}, rows)
}

func TestEnvironmentHeaderRowsEmpty(t *testing.T) {
	t.Parallel()

	rows, err := EnvironmentHeaderRows(nil)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestEnvironmentHeaderRowsRefusesInvalidEntryWithoutValue(t *testing.T) {
	t.Parallel()

	_, err := EnvironmentHeaderRows(InspectEnvironmentHeaders([]EnvironmentHeaderEntry{
		{Name: "MCP_HEADER_X-Ok", Value: syntheticEnvValue, Undecryptable: false},
		{Name: "MCP_HEADER_X-Bad", Value: "synthetic-bad\r\nvalue", Undecryptable: false},
	}))
	require.ErrorIs(t, err, ErrInvalidEnvironmentHeader)
	require.Contains(t, err.Error(), "MCP_HEADER_X-Bad")
	require.NotContains(t, err.Error(), "synthetic-bad")
	require.NotContains(t, err.Error(), syntheticEnvValue)
}

func TestEnvironmentHeaderRowsUndecryptableIsDistinct(t *testing.T) {
	t.Parallel()

	_, err := EnvironmentHeaderRows(InspectEnvironmentHeaders([]EnvironmentHeaderEntry{
		{Name: "MCP_HEADER_Gram-Key", Value: syntheticEnvValue, Undecryptable: false},
		{Name: "MCP_HEADER_X-Foo", Value: "", Undecryptable: true},
	}))
	require.ErrorIs(t, err, ErrUndecryptableEnvironmentHeader)
	require.NotErrorIs(t, err, ErrInvalidEnvironmentHeader)
}

func envProxy(t *testing.T, policy HeaderPolicy, source []ConfiguredHeader, env []ConfiguredHeader) *Proxy {
	t.Helper()
	p := policyProxy(t, policy, source)
	p.EnvironmentHeaders = env
	return p
}

func envRow(name string) ConfiguredHeader {
	return ConfiguredHeader{IsRequired: true, Name: name, StaticValue: syntheticEnvValue, ValueFromRequestHeader: ""}
}

var bothPolicies = []struct {
	name   string
	policy HeaderPolicy
}{
	{name: "remote", policy: HeaderPolicyRemote},
	{name: "tunneled", policy: HeaderPolicyTunneled},
}

// An environment header replaces a source row of the same field, static or
// passed through, before any row resolves: a required pass-through it
// replaces imposes no requirement, and the client's copy does not survive.
func TestApplyRequestHeadersEnvironmentReplacesSourceRows(t *testing.T) {
	t.Parallel()

	for _, tc := range bothPolicies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := envProxy(t, tc.policy, []ConfiguredHeader{
				{IsRequired: true, Name: "X-Instance-Url", StaticValue: "source-value", ValueFromRequestHeader: ""},
				{IsRequired: true, Name: "X-Tenant", StaticValue: "", ValueFromRequestHeader: "X-Caller-Tenant"},
				{IsRequired: false, Name: "X-Region", StaticValue: "", ValueFromRequestHeader: "X-Caller-Region"},
				{IsRequired: false, Name: "X-Kept", StaticValue: "kept", ValueFromRequestHeader: ""},
			}, []ConfiguredHeader{envRow("X-Instance-Url"), envRow("X-Tenant"), envRow("X-Region")})

			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			userReq.Header.Set("X-Caller-Region", "client-region")
			userReq.Header.Set("X-Instance-Url", "client-value")
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

			// X-Caller-Tenant is missing: the replaced required row must not fire.
			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Equal(t, []string{syntheticEnvValue}, remoteReq.Header.Values("X-Instance-Url"))
			require.Equal(t, []string{syntheticEnvValue}, remoteReq.Header.Values("X-Tenant"))
			require.Equal(t, []string{syntheticEnvValue}, remoteReq.Header.Values("X-Region"))
			require.Equal(t, "kept", remoteReq.Header.Get("X-Kept"))
		})
	}
}

// A source row spelled with underscores addresses the same field as the
// environment header and is replaced too.
func TestApplyRequestHeadersEnvironmentReplacesFoldedSourceRow(t *testing.T) {
	t.Parallel()

	for _, tc := range bothPolicies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := envProxy(t, tc.policy, []ConfiguredHeader{
				{IsRequired: true, Name: "X_instance_url", StaticValue: "source-value", ValueFromRequestHeader: ""},
			}, []ConfiguredHeader{envRow("X-Instance-Url")})
			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Equal(t, http.Header{"X-Instance-Url": []string{syntheticEnvValue}}, remoteReq.Header)
		})
	}
}

// An environment header owns every spelling of its name on both policies, in
// both directions, so a caller cannot place a folded alias beside it.
func TestApplyRequestHeadersEnvironmentOwnsFoldedSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range bothPolicies {
		for _, envName := range []string{"X-Instance-Url", "X_instance_url"} {
			t.Run(tc.name+"/"+envName, func(t *testing.T) {
				t.Parallel()

				p := envProxy(t, tc.policy, nil, []ConfiguredHeader{envRow(envName)})
				userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
				userReq.Header["X_Instance_Url"] = []string{"client-underscore"}
				userReq.Header["X-Instance-Url"] = []string{"client-dash"}
				userReq.Header["x-instance-url"] = []string{"client-lower"}
				remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

				require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
				require.Equal(t, http.Header{http.CanonicalHeaderKey(envName): []string{syntheticEnvValue}}, remoteReq.Header)
			})
		}
	}
}

// Routing headers and a resolved upstream token still win over the
// environment, and a shadowed environment Authorization imposes nothing.
func TestApplyRequestHeadersEnvironmentPrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range bothPolicies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := envProxy(t, tc.policy, nil, []ConfiguredHeader{envRow("Authorization"), envRow("X-Tenant")})
			p.AuthorizationOverride = "upstream-token"
			p.RoutingHeaders = []ConfiguredHeader{{IsRequired: true, Name: "X-Tenant", StaticValue: "routing", ValueFromRequestHeader: ""}}
			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Equal(t, "Bearer upstream-token", remoteReq.Header.Get("Authorization"))
			require.Equal(t, "routing", remoteReq.Header.Get("X-Tenant"))
		})
	}
}

func TestApplyRequestHeadersEnvironmentAuthorizationWithoutOverride(t *testing.T) {
	t.Parallel()

	for _, tc := range bothPolicies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := envProxy(t, tc.policy, nil, []ConfiguredHeader{envRow("Authorization")})
			userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
			userReq.Header.Set("Authorization", syntheticBearer)
			remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

			require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
			require.Equal(t, []string{syntheticEnvValue}, remoteReq.Header.Values("Authorization"))
		})
	}
}

// Environment rows meet the strict policy on a remote server too: a row naming
// a Speakeasy credential, session, assertion or protocol field is refused even
// if it bypassed load-time inspection, so the environment can never
// reintroduce a header the remote client-header copy drops.
func TestApplyRequestHeadersEnvironmentStrictOnBothPolicies(t *testing.T) {
	t.Parallel()

	for _, tc := range bothPolicies {
		for _, name := range []string{"Gram-Key", "Gram-Chat-Session", "Speakeasy-Ai-Key", "X-Gram-Scope-Override", "X-Speakeasy-Identity", "Cookie", "Mcp-Session-Id", "Mcp-Method", "Host"} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, EnvironmentHeaderReserved, inspectOne(t, EnvironmentHeaderPrefix+name, syntheticEnvValue).Status)

				p := envProxy(t, tc.policy, nil, []ConfiguredHeader{envRow(name)})
				userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
				remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

				err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
				for _, values := range remoteReq.Header {
					require.NotContains(t, values, syntheticEnvValue)
				}
			})
		}
	}
}

func TestApplyRequestHeadersEnvironmentRefusesEmptyRow(t *testing.T) {
	t.Parallel()

	p := envProxy(t, HeaderPolicyRemote, nil, []ConfiguredHeader{{IsRequired: true, Name: "X-Foo", StaticValue: " ", ValueFromRequestHeader: ""}})
	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.True(t, errors.Is(err, ErrInvalidEnvironmentHeader))
}

// Redirect stripping covers every spelling of an environment header and the
// inbound source of a pass-through row the environment replaced.
func TestStripConfiguredCredentialsIncludesEnvironmentHeaders(t *testing.T) {
	t.Parallel()

	p := envProxy(t, HeaderPolicyRemote, []ConfiguredHeader{
		{IsRequired: true, Name: "X-Upstream-Key", StaticValue: "", ValueFromRequestHeader: "X-Caller-Key"},
	}, []ConfiguredHeader{envRow("X-Upstream-Key"), envRow("X-Instance-Url")})

	header := http.Header{}
	header.Set("X-Upstream-Key", syntheticEnvValue)
	header.Set("X-Caller-Key", "synthetic-caller-key")
	header.Set("X-Instance-Url", syntheticEnvValue)
	header["X_Instance_Url"] = []string{syntheticEnvValue}
	header.Set("X-Other", "kept")
	p.stripConfiguredCredentials(header)

	require.Equal(t, http.Header{"X-Other": []string{"kept"}}, header)
}

// A tunnel reroute replaces routing fields and keeps environment headers.
func TestForwardRequestWithRetryKeepsEnvironmentHeaders(t *testing.T) {
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
		Headers:              []ConfiguredHeader{{IsRequired: true, Name: "X-Instance-Url", StaticValue: "source-value", ValueFromRequestHeader: ""}},
		EnvironmentHeaders:   []ConfiguredHeader{envRow("X-Instance-Url")},
		RoutingHeaders:       []ConfiguredHeader{{IsRequired: true, Name: "X-Gram-Tunnel-Forward-Token", StaticValue: "first", ValueFromRequestHeader: ""}},
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
	_, upstreamResp, err := p.forwardRequestWithRetry(t.Context(), req, func() io.Reader { return strings.NewReader("") }, nil)
	require.NoError(t, err)
	defer func() { _ = upstreamResp.Body.Close() }()
	require.Equal(t, http.StatusOK, upstreamResp.StatusCode)

	first, second := <-seen, <-seen
	for _, got := range []http.Header{first, second} {
		require.Equal(t, []string{syntheticEnvValue}, got.Values("X-Instance-Url"))
	}
}

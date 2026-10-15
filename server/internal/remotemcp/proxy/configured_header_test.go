package proxy_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

func TestConfiguredHeader_Resolve_PassThroughPresent(t *testing.T) {
	t.Parallel()

	h := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   "X-Upstream",
		StaticValue:            "",
		ValueFromRequestHeader: "X-Inbound",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Inbound", "from-user")

	value, err := h.Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "from-user", value)
}

func TestConfiguredHeader_Resolve_PassThroughMissingOptional(t *testing.T) {
	t.Parallel()

	h := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   "X-Upstream",
		StaticValue:            "",
		ValueFromRequestHeader: "X-Inbound",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	value, err := h.Resolve(req)
	require.NoError(t, err)
	require.Empty(t, value)
}

func TestConfiguredHeader_Resolve_PassThroughMissingRequired(t *testing.T) {
	t.Parallel()

	h := proxy.ConfiguredHeader{
		IsRequired:             true,
		Name:                   "X-Upstream",
		StaticValue:            "",
		ValueFromRequestHeader: "X-Inbound",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	value, err := h.Resolve(req)
	require.Error(t, err)
	require.Empty(t, value)
	require.Contains(t, err.Error(), `"X-Upstream"`)
	require.Contains(t, err.Error(), `"X-Inbound"`)
}

func TestConfiguredHeader_Resolve_StaticValue(t *testing.T) {
	t.Parallel()

	h := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   "X-Upstream",
		StaticValue:            "fixed",
		ValueFromRequestHeader: "",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	value, err := h.Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "fixed", value)
}

func TestConfiguredHeader_Resolve_StaticValuePreferredOverPassThrough(t *testing.T) {
	t.Parallel()

	// When both fields are set the pass-through branch wins. This documents
	// current behavior; callers are expected to set exactly one.
	h := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   "X-Upstream",
		StaticValue:            "static",
		ValueFromRequestHeader: "X-Inbound",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Inbound", "from-user")

	value, err := h.Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "from-user", value)
}

func TestConfiguredHeader_Resolve_UnconfiguredOptional(t *testing.T) {
	t.Parallel()

	h := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   "X-Upstream",
		StaticValue:            "",
		ValueFromRequestHeader: "",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	value, err := h.Resolve(req)
	require.NoError(t, err)
	require.Empty(t, value)
}

func TestConfiguredHeader_Resolve_UnconfiguredRequired(t *testing.T) {
	t.Parallel()

	h := proxy.ConfiguredHeader{
		IsRequired:             true,
		Name:                   "X-Upstream",
		StaticValue:            "",
		ValueFromRequestHeader: "",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	value, err := h.Resolve(req)
	require.Error(t, err)
	require.Empty(t, value)
	require.Contains(t, err.Error(), `"X-Upstream"`)
}

func TestConfiguredHeader_Resolve_PassThroughDeniedSource(t *testing.T) {
	t.Parallel()

	// The proxy already refuses to copy Cookie upstream. Reading it as a
	// configured header's source would carry the dashboard's gram_session to
	// an arbitrary remote server under whatever name the operator picked.
	for _, source := range []string{"Cookie", "cookie", "Set-Cookie", "Proxy-Authorization"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
			require.NoError(t, err)
			req.Header.Set(source, "gram_session=secret")

			// An optional row is dropped, so one leftover row does not fail
			// every request to the server.
			optional := proxy.ConfiguredHeader{
				IsRequired:             false,
				Name:                   "X-Upstream",
				StaticValue:            "",
				ValueFromRequestHeader: source,
			}
			value, err := optional.Resolve(req)
			require.NoError(t, err)
			require.Empty(t, value)

			// A required row can never be satisfied.
			required := optional
			required.IsRequired = true
			value, err = required.Resolve(req)
			require.Error(t, err)
			require.Empty(t, value)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestConfiguredHeader_Resolve_PassThroughAuthorizationAllowed(t *testing.T) {
	t.Parallel()

	// Forwarding the caller's own upstream credential is the point of
	// pass-through identity, so Authorization stays available as a source.
	h := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   "Authorization",
		StaticValue:            "",
		ValueFromRequestHeader: "Authorization",
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer upstream-token")

	value, err := h.Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "Bearer upstream-token", value)
}

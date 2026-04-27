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

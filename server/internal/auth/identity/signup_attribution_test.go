package identity

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestNewSignupAttribution_NothingToCarry(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewSignupAttribution("", " ", "", "", "", "", "https://ai.speakeasy.com"))
}

func TestNewSignupAttribution_Properties(t *testing.T) {
	t.Parallel()

	a := NewSignupAttribution(" linkedin ", "paid", "q4-mcp-gateway", "", "", "sess-123", "https://ai.speakeasy.com")
	require.NotNil(t, a)

	require.Equal(t, map[string]any{
		"utm_source":   "linkedin",
		"utm_medium":   "paid",
		"utm_campaign": "q4-mcp-gateway",
		"$session_id":  "sess-123",
		"$host":        "ai.speakeasy.com",
		"$set_once": map[string]any{
			"$initial_utm_source":   "linkedin",
			"$initial_utm_medium":   "paid",
			"$initial_utm_campaign": "q4-mcp-gateway",
		},
	}, a.Properties())
}

func TestSignupAttribution_SessionOnly(t *testing.T) {
	t.Parallel()

	a := NewSignupAttribution("", "", "", "", "", "sess-123", "https://ai.speakeasy.com")
	require.Equal(t, map[string]any{
		"$session_id": "sess-123",
		"$host":       "ai.speakeasy.com",
	}, a.Properties())
}

func TestSignupAttribution_NilProperties(t *testing.T) {
	t.Parallel()

	var a *SignupAttribution
	props := a.Properties()
	require.Empty(t, props)
	// Callers add their own keys to the returned map.
	props["email"] = "x"
}

func TestNewSignupAttribution_Truncates(t *testing.T) {
	t.Parallel()

	// A multi-byte rune straddling the cut must not be split.
	long := strings.Repeat("a", maxAttributionValueLength-1) + "é" + "tail"
	a := NewSignupAttribution(long, "", "", "", "", "", "")
	require.NotNil(t, a)
	require.Len(t, a.UTMSource, maxAttributionValueLength-1)
	require.True(t, utf8.ValidString(a.UTMSource))
	require.Empty(t, a.Host)
}

package netingress

import (
	"mime"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTailscaleIdentityParser(t *testing.T) {
	t.Parallel()

	parser := TailscaleIdentityParser{}

	t.Run("plain identity", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{
			TailscaleUserLoginHeader:      {"user@example.com"},
			TailscaleUserNameHeader:       {"Example User"},
			TailscaleUserProfilePicHeader: {"https://example.com/profile.png"},
		}
		identity, err := parser.ParseIdentity(headers)
		require.NoError(t, err)
		require.Equal(t, "user@example.com", identity.Login)
		require.Equal(t, "Example User", identity.Name)
	})

	t.Run("RFC 2047 identity", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{
			TailscaleUserLoginHeader: {mime.QEncoding.Encode("utf-8", "usér@example.com")},
			TailscaleUserNameHeader:  {mime.QEncoding.Encode("utf-8", "Exámple User")},
		}
		identity, err := parser.ParseIdentity(headers)
		require.NoError(t, err)
		require.Equal(t, "usér@example.com", identity.Login)
		require.Equal(t, "Exámple User", identity.Name)
	})

	t.Run("tagged node has no identity", func(t *testing.T) {
		t.Parallel()
		identity, err := parser.ParseIdentity(http.Header{})
		require.NoError(t, err)
		require.Nil(t, identity)
	})

	for _, test := range []struct {
		name    string
		headers http.Header
	}{
		{
			name: "missing name",
			headers: http.Header{
				TailscaleUserLoginHeader: {"user@example.com"},
			},
		},
		{
			name: "duplicate login",
			headers: http.Header{
				TailscaleUserLoginHeader: {"user@example.com", "other@example.com"},
				TailscaleUserNameHeader:  {"Example User"},
			},
		},
		{
			name: "malformed encoded word",
			headers: http.Header{
				TailscaleUserLoginHeader: {"=?utf-8?q?unterminated"},
				TailscaleUserNameHeader:  {"Example User"},
			},
		},
		{
			name: "newline",
			headers: http.Header{
				TailscaleUserLoginHeader: {"user@example.com\nforged"},
				TailscaleUserNameHeader:  {"Example User"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identity, err := parser.ParseIdentity(test.headers)
			require.Error(t, err)
			require.Nil(t, identity)
		})
	}
}

func TestIdentityParsers(t *testing.T) {
	t.Parallel()

	parsers := IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}
	identity, err := parsers.Parse(ProviderTailscale, http.Header{
		TailscaleUserLoginHeader: {"user@example.com"},
		TailscaleUserNameHeader:  {"Example User"},
	})
	require.NoError(t, err)
	require.Equal(t, "user@example.com", identity.Login)

	_, err = parsers.Parse("unknown", http.Header{})
	require.ErrorContains(t, err, "unsupported network ingress provider")
}

func TestStripUnsupportedTailscaleHeaders(t *testing.T) {
	t.Parallel()

	headers := http.Header{
		"Authorization":                  {"Bearer mcp-token"},
		TailscaleUserLoginHeader:         {"user@example.com"},
		TailscaleUserNameHeader:          {"Example User"},
		TailscaleUserProfilePicHeader:    {"https://example.com/profile.png"},
		"Tailscale-Capability-Grant":     {"unsupported"},
		"Tailscale-App-Capabilities":     {"unsupported"},
		"Tailscale-Unknown-Provider-Key": {"unsupported"},
		"X-Forwarded-Proto":              {"https"},
	}

	StripUnsupportedTailscaleHeaders(headers)

	require.Equal(t, "Bearer mcp-token", headers.Get("Authorization"))
	require.Equal(t, "user@example.com", headers.Get(TailscaleUserLoginHeader))
	require.Equal(t, "Example User", headers.Get(TailscaleUserNameHeader))
	require.Equal(t, "https://example.com/profile.png", headers.Get(TailscaleUserProfilePicHeader))
	require.Empty(t, headers.Get("Tailscale-Capability-Grant"))
	require.Empty(t, headers.Get("Tailscale-App-Capabilities"))
	require.Empty(t, headers.Get("Tailscale-Unknown-Provider-Key"))
	require.Equal(t, "https", headers.Get("X-Forwarded-Proto"))
}

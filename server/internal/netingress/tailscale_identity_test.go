package netingress

import (
	"fmt"
	"mime"
	"net/http"
	"strings"
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

	t.Run("decoded identity may contain literal encoded-word marker", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{
			TailscaleUserLoginHeader: {mime.QEncoding.Encode("utf-8", "user@example.com")},
			TailscaleUserNameHeader:  {mime.BEncoding.Encode("utf-8", "Exámple =? User")},
		}
		identity, err := parser.ParseIdentity(headers)
		require.NoError(t, err)
		require.Equal(t, "Exámple =? User", identity.Name)
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
			name: "valid word followed by malformed marker",
			headers: http.Header{
				TailscaleUserLoginHeader: {mime.QEncoding.Encode("utf-8", "user@example.com") + " =?utf-8?q?unterminated"},
				TailscaleUserNameHeader:  {"Example User"},
			},
		},
		{
			name: "valid word followed by malformed marker in same token",
			headers: http.Header{
				TailscaleUserLoginHeader: {mime.QEncoding.Encode("utf-8", "user@example.com") + "=?utf-8?q?unterminated"},
				TailscaleUserNameHeader:  {"Example User"},
			},
		},
		{
			name: "malformed marker after plain prefix",
			headers: http.Header{
				TailscaleUserLoginHeader: {"prefix=?utf-8?q?unterminated"},
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

func TestTailscaleIdentityParserEncodedNames(t *testing.T) {
	t.Parallel()
	longName := strings.Repeat("É", 60) + " User"
	for _, test := range []struct{ name, raw, want string }{
		{name: "leading Q escape", raw: "=?utf-8?q?=C3=89xample_User?=", want: "Éxample User"},
		{name: "uppercase Q", raw: "=?UTF-8?Q?=C3=89xample_User?=", want: "Éxample User"},
		{name: "base64", raw: mime.BEncoding.Encode("utf-8", "Éxample User"), want: "Éxample User"},
		{name: "adjacent words", raw: "=?utf-8?q?=C3=89xample?= =?utf-8?q?_User?=", want: "Éxample User"},
		{name: "mixed plain text", raw: "User =?utf-8?q?=C3=89xample?=", want: "User Éxample"},
		{name: "long name", raw: mime.QEncoding.Encode("utf-8", longName), want: longName},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identity, err := (TailscaleIdentityParser{}).ParseIdentity(http.Header{
				TailscaleUserLoginHeader: {"user@example.com"},
				TailscaleUserNameHeader:  {test.raw},
			})
			require.NoError(t, err)
			require.NotNil(t, identity)
			require.Equal(t, test.want, identity.Name)
		})
	}
}

func TestTailscaleIdentityParserRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, header string
		values       []string
	}{
		{name: "empty login", header: TailscaleUserLoginHeader, values: []string{""}},
		{name: "empty name", header: TailscaleUserNameHeader, values: []string{""}},
		{name: "duplicate empty pictures", header: TailscaleUserProfilePicHeader, values: []string{"", ""}},
		{name: "empty and nonempty pictures", header: TailscaleUserProfilePicHeader, values: []string{"", "https://example.com/pic.png"}},
		{name: "whitespace picture", header: TailscaleUserProfilePicHeader, values: []string{" "}},
		{name: "unsafe picture", header: TailscaleUserProfilePicHeader, values: []string{"https://example.com/\x00"}},
		{name: "invalid Q payload", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q?User=ZZ?="}},
		{name: "invalid base64 payload", header: TailscaleUserNameHeader, values: []string{"=?utf-8?b?%%%?="}},
		{name: "missing charset separator", header: TailscaleUserNameHeader, values: []string{"=?utf-8"}},
		{name: "missing encoding separator", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q"}},
		{name: "empty charset", header: TailscaleUserNameHeader, values: []string{"=??q?User?="}},
		{name: "invalid encoding", header: TailscaleUserNameHeader, values: []string{"=?utf-8?x?User?="}},
		{name: "encoded newline", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q?User=0AName?="}},
		{name: "encoded carriage return", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q?User=0DName?="}},
		{name: "encoded NUL", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q?User=00Name?="}},
		{name: "encoded invalid UTF-8", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q?User=FF?="}},
		{name: "encoded whitespace", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q?_User?="}},
		{name: "encoded empty name", header: TailscaleUserNameHeader, values: []string{"=?utf-8?q??="}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			headers := http.Header{TailscaleUserLoginHeader: {"user@example.com"}, TailscaleUserNameHeader: {"Example User"}}
			headers[test.header] = test.values
			identity, err := (TailscaleIdentityParser{}).ParseIdentity(headers)
			require.Error(t, err)
			require.Nil(t, identity)
		})
	}
}

func TestTailscaleIdentityParserEmptyProfilePicture(t *testing.T) {
	t.Parallel()
	for _, withIdentity := range []bool{true, false} {
		t.Run(fmt.Sprintf("identity=%t", withIdentity), func(t *testing.T) {
			t.Parallel()
			headers := http.Header{TailscaleUserProfilePicHeader: {""}}
			if withIdentity {
				headers.Set(TailscaleUserLoginHeader, "user@example.com")
				headers.Set(TailscaleUserNameHeader, "Example User")
			}
			identity, err := (TailscaleIdentityParser{}).ParseIdentity(headers)
			require.NoError(t, err)
			if withIdentity {
				require.NotNil(t, identity)
				require.Equal(t, "user@example.com", identity.Login)
				require.Equal(t, "Example User", identity.Name)
			} else {
				require.Nil(t, identity)
			}
		})
	}
}

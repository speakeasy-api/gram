package requestorigin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		expect  string
		wantErr bool
	}{
		{name: "hostname", raw: "Example.COM", expect: "example.com"},
		{name: "port", raw: "Example.COM:443", expect: "example.com"},
		{name: "ipv6", raw: "[2001:db8::1]:8443", expect: "2001:db8::1"},
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace", raw: " example.com", wantErr: true},
		{name: "trailing dot", raw: "example.com.", wantErr: true},
		{name: "empty port", raw: "example.com:", wantErr: true},
		{name: "invalid port", raw: "example.com:not-a-port", wantErr: true},
		{name: "out of range port", raw: "example.com:99999", wantErr: true},
		{name: "userinfo", raw: "user@example.com", wantErr: true},
		{name: "path", raw: "example.com/path", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CanonicalHost(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expect, got)
		})
	}
}

func TestContext(t *testing.T) {
	t.Parallel()

	origin := Origin{Surface: SurfaceCustomDomain, BaseURL: "https://example.com", OrganizationID: "org"}
	ctx := WithContext(t.Context(), origin)

	got, ok := FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, origin, got)
	require.Equal(t, origin.BaseURL, BaseURL(ctx, "https://fallback.example"))
	require.Equal(t, "https://fallback.example", BaseURL(t.Context(), "https://fallback.example"))
}

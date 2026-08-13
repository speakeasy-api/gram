package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeReturnTo(t *testing.T) {
	t.Parallel()

	const fallback = "/"

	tests := []struct {
		name           string
		raw            string
		allowedOrigins []string
		want           string
	}{
		{
			name:           "empty input falls back",
			raw:            "",
			allowedOrigins: nil,
			want:           fallback,
		},
		{
			name:           "relative path survives with no allowed origins",
			raw:            "/organizations/example-org",
			allowedOrigins: nil,
			want:           "/organizations/example-org",
		},
		{
			name:           "relative path with query survives with no allowed origins",
			raw:            "/organizations/example-org?tab=members",
			allowedOrigins: nil,
			want:           "/organizations/example-org?tab=members",
		},
		{
			name:           "relative path keeps its fragment",
			raw:            "/organizations/example-org?tab=members#row-3",
			allowedOrigins: nil,
			want:           "/organizations/example-org?tab=members#row-3",
		},
		{
			name:           "absolute url falls back with no allowed origins",
			raw:            "https://admin.example.com/organizations/example-org?tab=members",
			allowedOrigins: nil,
			want:           fallback,
		},
		{
			name:           "absolute url survives when its origin is allowed",
			raw:            "https://admin.example.com/organizations/example-org?tab=members",
			allowedOrigins: []string{"https://admin.example.com"},
			want:           "https://admin.example.com/organizations/example-org?tab=members",
		},
		{
			name:           "absolute url falls back when only another origin is allowed",
			raw:            "https://evil.example.com/steal",
			allowedOrigins: []string{"https://admin.example.com"},
			want:           fallback,
		},
		{
			name:           "port is part of the origin",
			raw:            "https://admin.example.com:8443/organizations",
			allowedOrigins: []string{"https://admin.example.com"},
			want:           fallback,
		},
		{
			name:           "scheme is part of the origin",
			raw:            "http://admin.example.com/organizations",
			allowedOrigins: []string{"https://admin.example.com"},
			want:           fallback,
		},
		{
			name:           "protocol-relative url falls back",
			raw:            "//evil.example.com/steal",
			allowedOrigins: []string{"https://admin.example.com"},
			want:           fallback,
		},
		{
			name:           "path-less input falls back",
			raw:            "?tab=members",
			allowedOrigins: nil,
			want:           fallback,
		},
		{
			name:           "unparseable input falls back",
			raw:            "://\x7f",
			allowedOrigins: nil,
			want:           fallback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, sanitizeReturnTo(tt.raw, fallback, tt.allowedOrigins))
		})
	}
}

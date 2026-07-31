package middleware

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogSafeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no secrets untouched",
			in:   "/rpc/skills.list?limit=10",
			want: "/rpc/skills.list?limit=10",
		},
		{
			name: "token query parameter redacted",
			in:   "/rpc/skills.getShared?token=supersecret",
			want: "/rpc/skills.getShared?token=REDACTED",
		},
		{
			name: "token query parameter redacted among others",
			in:   "/rpc/chatSessions.revoke?a=1&token=supersecret",
			want: "/rpc/chatSessions.revoke?a=1&token=REDACTED",
		},
		{
			name: "shared skill path segment redacted",
			in:   "/shared/skills/supersecrettoken",
			want: "/shared/skills/REDACTED",
		},
		{
			name: "shared skill path with trailing segment redacted",
			in:   "/shared/skills/supersecrettoken/extra",
			want: "/shared/skills/REDACTED/extra",
		},
		{
			name: "shared skill path and token query both redacted",
			in:   "/shared/skills/supersecrettoken?token=alsosecret",
			want: "/shared/skills/REDACTED?token=REDACTED",
		},
		{
			name: "shared skills prefix without token untouched",
			in:   "/shared/skills/",
			want: "/shared/skills/",
		},
		{
			name: "unrelated shared path untouched",
			in:   "/shared/other/value",
			want: "/shared/other/value",
		},
		{
			name: "absolute referrer URL with tokenized path redacted",
			in:   "https://app.example.com/shared/skills/supersecrettoken",
			want: "https://app.example.com/shared/skills/REDACTED",
		},
		{
			name: "absolute referrer URL with token query redacted",
			in:   "https://app.example.com/page?token=supersecret",
			want: "https://app.example.com/page?token=REDACTED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, logSafeURL(u))
		})
	}
}

package migration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration"
)

func TestCleanRemoteURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
		err  error
	}{
		{name: "plain", raw: "https://mcp.example.com/mcp", want: "https://mcp.example.com/mcp", err: nil},
		{name: "bare template query", raw: "https://mcp.example.com/v1/mcp?{TOOLSETS}", want: "https://mcp.example.com/v1/mcp", err: nil},
		{name: "template value", raw: "https://mcp.example.com/mcp?toolsets={TOOLSETS}&region=us", want: "https://mcp.example.com/mcp?region=us", err: nil},
		{name: "encoded template", raw: "https://mcp.example.com/mcp?toolsets=%7BTOOLSETS%7D", want: "https://mcp.example.com/mcp", err: nil},
		{name: "concrete query kept", raw: "https://mcp.example.com/mcp?region=eu", want: "https://mcp.example.com/mcp?region=eu", err: nil},
		{name: "templated path", raw: "https://mcp.example.com/{TENANT}/mcp", want: "", err: migration.ErrTemplatedPath},
		{name: "encoded templated path", raw: "https://mcp.example.com/%7BTENANT%7D/mcp", want: "", err: migration.ErrTemplatedPath},
		{name: "json filter kept", raw: `https://mcp.example.com/mcp?filter={"team":"a"}`, want: `https://mcp.example.com/mcp?filter={"team":"a"}`, err: nil},
		{name: "encoded json filter kept", raw: "https://mcp.example.com/mcp?filter=%7B%22team%22%3A%22a%22%7D", want: "https://mcp.example.com/mcp?filter=%7B%22team%22%3A%22a%22%7D", err: nil},
		{name: "hostless", raw: "https:///mcp", want: "", err: migration.ErrInvalidURL},
		{name: "relative", raw: "/mcp", want: "", err: migration.ErrInvalidURL},
		{name: "non-http scheme", raw: "ftp://mcp.example.com/mcp", want: "", err: migration.ErrInvalidURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := migration.CleanRemoteURL(tc.raw)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

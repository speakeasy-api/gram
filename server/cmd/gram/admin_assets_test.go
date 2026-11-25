package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestAdminAssetFlagsOptional(t *testing.T) {
	t.Parallel()
	found := 0
	for _, flag := range newAdminCommand().Flags {
		if f, ok := flag.(*cli.StringFlag); ok && (f.Name == "assets-backend" || f.Name == "assets-uri") {
			found++
			require.False(t, f.Required, f.Name)
		}
	}
	require.Equal(t, 2, found)
}

func TestResolveAdminAssetStorage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, backend, uri, wantBackend string
		unavailable                     bool
	}{
		{"infer bucket", "", "gs://synthetic-bucket", "gcs", false},
		{"infer prefix", "", "gs://synthetic-bucket/assets", "gcs", false},
		{"explicit fs", "fs", "./local-assets", "fs", false},
		{"explicit gcs", "gcs", "gs://synthetic-bucket", "gcs", false},
		{"explicit fs is authoritative", "fs", "gs://synthetic-bucket", "fs", false},
		{"explicit invalid backend", "invalid", "gs://synthetic-bucket", "", true},
		{"no config", "", "", "", true},
		{"backend only", "fs", "", "", true},
		{"no local inference", "", "./local-assets", "", true},
		{"no https inference", "", "https://synthetic-bucket", "", true},
		{"missing bucket", "", "gs:///assets", "", true},
		{"malformed uri", "", "gs://%", "", true},
		{"reject userinfo", "", "gs://user@synthetic-bucket", "", true},
		{"reject query", "", "gs://synthetic-bucket?other=location", "", true},
		{"reject port", "", "gs://synthetic-bucket:80", "", true},
		{"explicit gcs needs gs", "gcs", "https://synthetic-bucket", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := resolveAdminAssetStorage(tc.backend, tc.uri)
			if tc.unavailable {
				require.Error(t, err)
				require.Empty(t, opts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantBackend, opts.assetsBackend)
			require.Equal(t, tc.uri, opts.assetsURI, "must preserve configured storage location")
		})
	}
}

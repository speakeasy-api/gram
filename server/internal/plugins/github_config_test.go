package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// NewGitHubConfig must hard-fail on any partial configuration so a deployment
// missing one of the four env vars surfaces immediately rather than silently
// running with publishing disabled.
func TestNewGitHubConfig_Validation(t *testing.T) {
	t.Parallel()

	full := plugins.GitHubConfigInput{
		AppID:          1,
		PrivateKey:     "pem",
		Org:            "acme",
		InstallationID: 2,
	}

	cases := []struct {
		name        string
		mutate      func(in *plugins.GitHubConfigInput)
		wantNilCfg  bool
		wantErr     bool
		errContains string
	}{
		{
			name:       "all unset returns nil config and nil error (feature disabled)",
			mutate:     func(in *plugins.GitHubConfigInput) { *in = plugins.GitHubConfigInput{} },
			wantNilCfg: true,
			wantErr:    false,
		},
		{
			name:        "missing app id",
			mutate:      func(in *plugins.GitHubConfigInput) { in.AppID = 0 },
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-app-id",
		},
		{
			name:        "missing private key",
			mutate:      func(in *plugins.GitHubConfigInput) { in.PrivateKey = "" },
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-private-key",
		},
		{
			name:        "missing org",
			mutate:      func(in *plugins.GitHubConfigInput) { in.Org = "" },
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-org",
		},
		{
			name:        "missing installation id",
			mutate:      func(in *plugins.GitHubConfigInput) { in.InstallationID = 0 },
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-installation-id",
		},
		{
			name: "multiple missing names all reported",
			mutate: func(in *plugins.GitHubConfigInput) {
				in.PrivateKey = ""
				in.Org = ""
			},
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-private-key, plugins-github-org",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := full
			tc.mutate(&in)

			cfg, err := plugins.NewGitHubConfig(in)

			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
			if tc.wantNilCfg {
				require.Nil(t, cfg)
			}
		})
	}
}

// All-set inputs reach the underlying ghclient.NewClient, which rejects
// invalid PEM. The error surfaces with the "create github client" prefix.
func TestNewGitHubConfig_InvalidPEMBubbles(t *testing.T) {
	t.Parallel()

	cfg, err := plugins.NewGitHubConfig(plugins.GitHubConfigInput{
		AppID:          1,
		PrivateKey:     "not a pem",
		Org:            "acme",
		InstallationID: 2,
	})
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "create github client", "expected client-creation error, got: %v", err)
}

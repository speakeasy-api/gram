package plugins_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// newTestGitHubClient builds a real *ghclient.Client for use in validation
// tests. The signing key is generated fresh per call and used only for the
// in-memory client construction — no network traffic is generated.
func newTestGitHubClient(t *testing.T) *ghclient.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	c, err := ghclient.NewClient(1, keyPEM, &guardian.HTTPClient{})
	require.NoError(t, err)
	return c
}

// NewGitHubConfig must hard-fail on any partial configuration so a deployment
// missing one of the inputs surfaces immediately rather than silently running
// with publishing disabled.
func TestNewGitHubConfig_Validation(t *testing.T) {
	t.Parallel()

	full := plugins.GitHubConfigInput{
		Client:         newTestGitHubClient(t),
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
			name:        "missing client",
			mutate:      func(in *plugins.GitHubConfigInput) { in.Client = nil },
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-client",
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
				in.Client = nil
				in.Org = ""
			},
			wantNilCfg:  true,
			wantErr:     true,
			errContains: "plugins-github-client, plugins-github-org",
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

package mockoidc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	mockoidc "github.com/speakeasy-api/gram/mock-oidc"
)

const configTemplate = `provider:
  users:
    - email: eng@speakeasyapi.dev
      name: Engineering User
      email_verified: true
  oauth_clients:
    - client_id: gram-local.apps.googleusercontent.com
      client_secret: GOCSPX-example_secret
      name: Gram (Google)
      redirect_uris:
        - "${GRAM_ADMIN_SERVER_URL}/admin/auth.callback"
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mock-oidc.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestLoadConfig_ExpandsEnvironmentVariables(t *testing.T) {
	t.Setenv("GRAM_ADMIN_SERVER_URL", "https://localhost:33575")

	cfg, err := mockoidc.LoadConfig(writeConfig(t, configTemplate))
	require.NoError(t, err)

	client, ok := cfg.FindClient("gram-local.apps.googleusercontent.com")
	require.True(t, ok)
	require.True(t, client.AllowsRedirect("https://localhost:33575/admin/auth.callback"))
}

func TestLoadConfig_RejectsUnsetEnvironmentVariables(t *testing.T) {
	t.Parallel()

	// An unset variable expands to an empty string, which yields a redirect URI
	// that only fails once a browser reaches the provider.
	body := strings.ReplaceAll(configTemplate, "GRAM_ADMIN_SERVER_URL", "GRAM_MOCK_OIDC_UNSET_FOR_TEST")

	_, err := mockoidc.LoadConfig(writeConfig(t, body))
	require.ErrorContains(t, err, "GRAM_MOCK_OIDC_UNSET_FOR_TEST")
}

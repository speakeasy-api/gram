package bootstrap

import (
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeUsesOpenSSLTrustStoreWithFreshEnvironment(t *testing.T) {
	t.Parallel()

	// Exercise both launch paths with a real TLS connection. Trust is supplied
	// solely by OpenSSL's CA file, with no inherited NODE_OPTIONS or extra roots.
	for _, prepare := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepare=%t", prepare), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("trusted"))
			}))
			t.Cleanup(server.Close)

			workDir := t.TempDir()
			var command string
			var args []string
			var err error
			if prepare {
				command, args, err = prepareProgram(workDir, "javascript")
			} else {
				command, args, err = ResolveProgram("javascript", workDir)
			}
			require.NoError(t, err)
			require.Equal(t, "node", command)

			// Replace the entrypoint's application logic while retaining the real
			// launcher arguments and the subprocess's isolated environment.
			entry := filepath.Join(workDir, "gram-start.js")
			if prepare {
				require.NoError(t, os.Remove(entry))
			}
			script := fmt.Sprintf(`fetch(%q).then(r => r.text()).then(console.log).catch(e => {
  console.error(e.cause?.code ?? e.message);
  process.exitCode = 1;
});`, server.URL)
			require.NoError(t, os.WriteFile(entry, []byte(script), 0600))

			caFile := filepath.Join(workDir, "ca.pem")
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: nil, Bytes: server.Certificate().Raw})
			require.NoError(t, os.WriteFile(caFile, ca, 0600))

			cmd := exec.CommandContext(t.Context(), "node", args...) //nolint:gosec // Exercises launcher-generated arguments; none come from external input.
			cmd.Env = []string{"SSL_CERT_FILE=" + caFile, "SSL_CERT_DIR=" + workDir}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Equal(t, "trusted\n", string(output))
		})
	}
}

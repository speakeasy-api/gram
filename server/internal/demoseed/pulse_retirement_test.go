package demoseed

import (
	"os"
	"strings"
	"testing"
)

func TestLocalFixturesDoNotInsertRetiredCatalog(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"local.go", "queries/local.sql"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), "api.pulsemcp.com") || strings.Contains(string(source), "localMCPRegistrySQL") || strings.Contains(string(source), "LocalEnsureMCPRegistry") {
			t.Fatalf("%s: local fixtures must rely on native catalog bootstrap, not insert the retired catalog", path)
		}
	}
}

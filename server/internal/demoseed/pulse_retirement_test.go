package demoseed

import (
	"os"
	"strings"
	"testing"
)

func TestLocalFixturesDoNotInsertRetiredCatalog(t *testing.T) {
	source, err := os.ReadFile("local.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "api.pulsemcp.com") || strings.Contains(string(source), "localMCPRegistrySQL") {
		t.Fatal("local fixtures must rely on native catalog bootstrap, not insert the retired catalog")
	}
}

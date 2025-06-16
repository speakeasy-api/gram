package assetstest

import (
	"os"
	"testing"

	"github.com/speakeasy-api/gram/internal/assets"
)

func NewTestBlobStore(t *testing.T) assets.BlobStore {
	t.Helper()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("failed to open root: %v", err)
	}

	return &assets.FSBlobStore{Root: root}
}

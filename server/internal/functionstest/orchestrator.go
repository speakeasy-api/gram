// Package functionstest provides helpers for constructing
// [functions.Orchestrator] instances in tests.
package functionstest

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// NewOrchestrator builds a [functions.Orchestrator] backed by the local
// runner and a temporary code root directory.
func NewOrchestrator(t *testing.T, assetStore assets.BlobStore) functions.Orchestrator {
	t.Helper()

	codeRoot := t.TempDir()
	return functions.NewLocalRunner(testenv.NewLogger(t), testenv.NewTracerProvider(t), codeRoot, testenv.DefaultSiteURL(t), assetStore)
}

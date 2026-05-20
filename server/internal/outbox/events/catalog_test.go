package events_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/outbox/cataloggen"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// TestCatalogIsUpToDate fails if either catalog_gen.go or catalog_gen.yaml is out of
// sync with the event definition files. Run 'mise gen:webhooks-server' to fix.
func TestCatalogIsUpToDate(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(thisFile)

	require.NoError(t, cataloggen.Check(dir))
	require.NoError(t, cataloggen.CheckYAML(dir, events.All))
}

package otel

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The connection logs route and the MCP feedback route each trust a provenance
// source, so only the connection recorder's sink may stamp the first and only the
// feedback recorder the second. A new producer or ingest path that names either
// must be reviewed here.
func TestRouteProvenanceSourcesAreStampedOnlyByTheirRecorders(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	uses := map[string][]string{"ConnectionLogSource": nil, "FeedbackLogSource": nil, "WithProvenanceSource(": nil}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "node_modules" || name == "gen" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", path, err)
		}
		for needle := range uses {
			if strings.Contains(string(content), needle) {
				uses[needle] = append(uses[needle], filepath.ToSlash(rel))
			}
		}
		return nil
	})
	require.NoError(t, err)

	for needle := range uses {
		slices.Sort(uses[needle])
	}
	require.Equal(t, []string{
		"internal/mcpconnectionattempts/otelsink/sink.go",
		"internal/otel/dialect/log_gateway.go",
		"internal/otel/enrich/keys.go",
		"internal/otel/handler_log_relay.go",
	}, uses["ConnectionLogSource"])
	require.Equal(t, []string{
		"internal/mcpfeedback/export.go",
		"internal/otel/dialect/log_gateway.go",
		"internal/otel/enrich/keys.go",
		// Reads the source only, to leave feedback without directory enrichment.
		"internal/otel/enrich/log_directory.go",
		"internal/otel/handler_log_relay.go",
	}, uses["FeedbackLogSource"])
	require.Equal(t, []string{
		"internal/mcpconnectionattempts/otelsink/sink.go",
		"internal/mcpfeedback/export.go",
		"internal/otelpub/logger.go",
	}, uses["WithProvenanceSource("])
}

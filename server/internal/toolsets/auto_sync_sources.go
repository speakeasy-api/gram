package toolsets

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// validateAutoSyncSources enforces the entry format for the toolsets
// auto_sync_sources column: each entry must be "<kind>:<source>" where
// <kind> is a known urn.ToolKind. Today only "function:" entries are
// accepted; other kinds are reserved for future expansion (OpenAPI,
// external MCP, ...) and rejected with a stable 400 so callers see a
// clear "not yet supported" error rather than silent acceptance.
func validateAutoSyncSources(entries []string) error {
	for _, entry := range entries {
		kind, source, ok := strings.Cut(entry, ":")
		if !ok || kind == "" || source == "" {
			return oops.E(oops.CodeBadRequest, nil,
				"auto_sync_sources entry %q must be \"<kind>:<source>\"", entry)
		}
		if urn.ToolKind(kind) != urn.ToolKindFunction {
			return oops.E(oops.CodeBadRequest, nil,
				"auto_sync_sources kind %q is not supported (only %q is accepted today)",
				kind, urn.ToolKindFunction)
		}
	}
	return nil
}

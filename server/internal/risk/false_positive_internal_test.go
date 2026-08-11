package risk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The FP mirror republishes batch-scanned Postgres rows, so its surfaces must
// match the offline backfill's per-source mapping — NOT the live stream
// defaults (batch gitleaks offsets index the composed scan surface, batch
// presidio offsets a YAML transform). A drift here would let a dismiss/undo
// rewrite a backfilled row's reveal semantics.
func TestFPMirrorSurface(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"gitleaks":         "scan_surface",
		"presidio":         "legacy_presidio",
		"prompt_injection": "none",
		"llm_judge":        "none",
		"shadow_mcp":       "derived",
		"account_identity": "derived",
		"destructive_tool": "derived",
		"cli_destructive":  "derived",
		"custom":           "",
		"":                 "",
	}
	for source, want := range cases {
		require.Equal(t, want, fpMirrorSurface(source), "fpMirrorSurface(%q)", source)
	}
}

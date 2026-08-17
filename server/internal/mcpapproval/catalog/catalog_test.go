package catalog

// White-box tests: declarations and annotationHint are the seams where the
// registry's raw JSON becomes stored evidence, and their edge cases — absent
// tool metadata, non-boolean hints — are exactly the ones that must not read
// as declarations the registry never made.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
)

// details builds a ServerDetails through JSON, the same route the registry
// client uses, so absent-versus-empty tool lists survive exactly as the wire
// distinguishes them.
func details(t *testing.T, raw string) *externalmcp.ServerDetails {
	t.Helper()

	var out externalmcp.ServerDetails
	//nolint:musttag // ServerDetails is not a wire type; JSON is just how the
	// test constructs a slice of the unexported tool struct from outside its
	// package, matching field presence as the registry client decodes it.
	require.NoError(t, json.Unmarshal([]byte(raw), &out))

	return &out
}

// A details fetch that succeeded without carrying tool metadata — no PulseMCP
// extension, unmatched remote slot — must stay nil: the assembler records the
// tool-declarations gap only on nil, and mapping absent metadata onto an
// empty slice would render "unknown" as "the registry declared zero tools".
func TestDeclarations_AbsentToolMetadataStaysNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, declarations(details(t, `{"Name": "com.example/server"}`)))
}

// A registry entry that genuinely declared an empty tool list is a real (if
// odd) declaration, distinct from absent metadata.
func TestDeclarations_DeclaredEmptyListStaysEmpty(t *testing.T) {
	t.Parallel()

	out := declarations(details(t, `{"Name": "com.example/server", "Tools": []}`))
	require.NotNil(t, out)
	require.Empty(t, out)
}

func TestDeclarations_MapsToolsAndHints(t *testing.T) {
	t.Parallel()

	out := declarations(details(t, `{"Tools": [{
		"name": "delete_page",
		"description": "Deletes a page",
		"inputSchema": {"type": "object"},
		"annotations": {"readOnlyHint": false, "destructiveHint": true}
	}]}`))
	require.Len(t, out, 1)
	require.Equal(t, "delete_page", out[0].Name)
	require.Equal(t, "Deletes a page", out[0].Description)
	require.JSONEq(t, `{"type": "object"}`, out[0].InputSchema)

	require.NotNil(t, out[0].ReadOnly)
	require.False(t, *out[0].ReadOnly, "an explicit false is a real declaration and survives")
	require.NotNil(t, out[0].Destructive)
	require.True(t, *out[0].Destructive)
	require.Nil(t, out[0].Idempotent, "an absent hint stays undeclared")
	require.Nil(t, out[0].OpenWorld)
}

// Annotation values are registry-authored JSON: a hint that is not a boolean
// — a string "true", a number, a null — is tolerated as undeclared rather
// than failing the mapping or, worse, reading as declared.
func TestAnnotationHint_NonBooleanValuesReadAsUndeclared(t *testing.T) {
	t.Parallel()

	annotations := map[string]any{
		"readOnlyHint":    "true",
		"destructiveHint": float64(1),
		"idempotentHint":  nil,
		"openWorldHint":   false,
	}

	require.Nil(t, annotationHint(annotations, "readOnlyHint"))
	require.Nil(t, annotationHint(annotations, "destructiveHint"))
	require.Nil(t, annotationHint(annotations, "idempotentHint"))
	require.Nil(t, annotationHint(annotations, "missingHint"))

	openWorld := annotationHint(annotations, "openWorldHint")
	require.NotNil(t, openWorld)
	require.False(t, *openWorld)
}

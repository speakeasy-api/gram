package capability_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
)

// A server publishing no annotations has declared nothing about its authority,
// which must not be read as declaring it harmless.
func TestAssess_NoAnnotationsIsUnannotatedNotSafe(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "do_something", Description: "", InputSchema: "",
		ReadOnly: nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})

	require.True(t, got.Unannotated)
	require.False(t, got.ActsOnBehalf, "an omitted hint is not a claim in either direction")
	require.Empty(t, got.Declared)
}

// readOnlyHint=false is the server stating the tool does more than read, which
// is the approver's actual question.
func TestAssess_ReadOnlyFalseMeansActsOnBehalf(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "send_email", Description: "", InputSchema: "",
		ReadOnly: new(false), Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})

	require.True(t, got.ActsOnBehalf)
	require.False(t, got.Unannotated)
}

func TestAssess_ReadOnlyTrueDoesNotActOnBehalf(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "get_weather", Description: "", InputSchema: "",
		ReadOnly: new(true), Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})

	require.False(t, got.ActsOnBehalf)
	require.Empty(t, got.Declared)
}

func TestAssess_DeclaredCapabilities(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "delete_everything", Description: "", InputSchema: "",
		ReadOnly: new(false), Destructive: new(true), Idempotent: new(false), OpenWorld: new(true),
	})

	require.Contains(t, got.Declared, capability.CapabilityDestructive)
	require.Contains(t, got.Declared, capability.CapabilityOpenWorld)
	require.True(t, got.ActsOnBehalf)
}

// destructiveHint=false is a claim of non-destructiveness, not a declaration of
// destructive capability.
func TestAssess_DestructiveFalseIsNotDeclaredDestructive(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "read_doc", Description: "", InputSchema: "",
		ReadOnly: nil, Destructive: new(false), Idempotent: nil, OpenWorld: nil,
	})

	require.NotContains(t, got.Declared, capability.CapabilityDestructive)
}

func TestAssess_SchemaImpliesDangerousParameters(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "run", Description: "",
		InputSchema: `{
			"type": "object",
			"properties": {
				"command": {"type": "string"},
				"workingPath": {"type": "string"},
				"callbackUrl": {"type": "string"},
				"apiKey": {"type": "string"}
			}
		}`,
		ReadOnly: nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})

	require.Contains(t, got.SchemaImplied, capability.CapabilityArbitraryCommand)
	require.Contains(t, got.SchemaImplied, capability.CapabilityFilesystemPath)
	require.Contains(t, got.SchemaImplied, capability.CapabilityArbitraryURL)
	require.Contains(t, got.SchemaImplied, capability.CapabilityCredentialInput)
}

// A JSON Schema `format` catches a parameter whose name gives nothing away.
func TestAssess_SchemaFormatIsASignal(t *testing.T) {
	t.Parallel()

	got := capability.Assess(capability.Declaration{
		Name: "fetch", Description: "",
		InputSchema: `{"type":"object","properties":{"target":{"type":"string","format":"uri"}}}`,
		ReadOnly:    nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})

	require.Contains(t, got.SchemaImplied, capability.CapabilityArbitraryURL)
}

// A dangerous parameter one level down is still a dangerous parameter.
func TestAssess_NestedAndArrayParametersAreWalked(t *testing.T) {
	t.Parallel()

	nested := capability.Assess(capability.Declaration{
		Name: "run", Description: "",
		InputSchema: `{"type":"object","properties":{"options":{"type":"object","properties":{"shell":{"type":"string"}}}}}`,
		ReadOnly:    nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})
	require.Contains(t, nested.SchemaImplied, capability.CapabilityArbitraryCommand)

	inArray := capability.Assess(capability.Declaration{
		Name: "batch", Description: "",
		InputSchema: `{"type":"object","properties":{"jobs":{"type":"array","items":{"type":"object","properties":{"scriptBody":{"type":"string"}}}}}}`,
		ReadOnly:    nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})
	require.Contains(t, inArray.SchemaImplied, capability.CapabilityArbitraryCommand)
}

// An unreadable or absent schema yields nothing. That is not a statement that
// the tool takes no dangerous input.
func TestAssess_UnparseableSchemaYieldsNothing(t *testing.T) {
	t.Parallel()

	for _, schema := range []string{"", "   ", "not json", "[]"} {
		got := capability.Assess(capability.Declaration{
			Name: "x", Description: "", InputSchema: schema,
			ReadOnly: nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
		})
		require.Empty(t, got.SchemaImplied, "schema %q", schema)
	}
}

// Schemas come from the server under review, so a self-referential or very
// deep one must terminate rather than exhaust the stack.
func TestAssess_DeeplyNestedSchemaTerminates(t *testing.T) {
	t.Parallel()

	schema := `{"type":"object","properties":{"a":{"type":"string"}}}`
	for range 60 {
		schema = `{"type":"object","properties":{"nested":` + schema + `}}`
	}

	got := capability.Assess(capability.Declaration{
		Name: "deep", Description: "", InputSchema: schema,
		ReadOnly: nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
	})
	require.Empty(t, got.SchemaImplied)
}

func TestHintOf(t *testing.T) {
	t.Parallel()

	require.Equal(t, capability.HintUndeclared, capability.HintOf(nil))
	require.Equal(t, capability.HintTrue, capability.HintOf(new(true)))
	require.Equal(t, capability.HintFalse, capability.HintOf(new(false)))
}

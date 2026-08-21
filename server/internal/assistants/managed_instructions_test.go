package assistants

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
)

// The managed assistant's prompts name every platform tool inline so a turn
// needs no discovery search. That only holds while the names are the ones the
// runtime serves: the runner dispatches on exact names and advertises no MCP
// schemas, so a stale name in the prompt costs the model a fruitless catalog
// search rather than failing loudly. These tests hold each prompt to the
// registry it describes, in both directions.

// promptToolNames extracts the runtime-prefixed tool names a prompt lists for
// one platform toolset, returning them with the prefix stripped.
func promptToolNames(t *testing.T, prompt, serverID string) []string {
	t.Helper()

	pattern := regexp.MustCompile(regexp.QuoteMeta("mcp__"+serverID+"_") + `[a-z0-9_]+`)
	prefix := "mcp__" + serverID + "_"

	var names []string
	for _, match := range pattern.FindAllString(prompt, -1) {
		name := strings.TrimPrefix(match, prefix)
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func TestLegacyInstructionsNameTheManagedToolset(t *testing.T) {
	t.Parallel()

	prompt := managedAssistantInstructionsFor(feature.VariantAssistantToolsLegacy)
	require.Equal(t,
		platformtoolsruntime.ManagedAssistantToolsetToolNames(),
		promptToolNames(t, prompt, "p-managed-assistant"),
		"legacy prompt must name exactly the managed-assistant toolset's tools",
	)
}

func TestPlatformMCPInstructionsNameThePlatformToolset(t *testing.T) {
	t.Parallel()

	prompt := managedAssistantInstructionsFor(feature.VariantAssistantToolsPlatformMCP)
	require.Equal(t,
		platformmcp.AssistantToolNames(),
		promptToolNames(t, prompt, "p-platform"),
		"platformmcp prompt must name exactly the tools admitted to the assistant audience",
	)
}

func TestBothInstructionsNameTheBaseAssistantsToolset(t *testing.T) {
	t.Parallel()

	base := platformtoolsruntime.AssistantsToolsetToolNames()
	for _, variant := range []feature.Variant{
		feature.VariantAssistantToolsLegacy,
		feature.VariantAssistantToolsPlatformMCP,
	} {
		prompt := managedAssistantInstructionsFor(variant)
		require.Equal(t, base, promptToolNames(t, prompt, "p-assistants"),
			"prompt for variant %q must name exactly the base assistants toolset's tools", variant)
	}
}

// An unrecognized variant must land on the same prompt as the toolset gate's
// fallback, or the assistant would be told about tools it was not granted.
func TestUnknownVariantUsesLegacyInstructions(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		managedAssistantInstructionsFor(feature.VariantAssistantToolsLegacy),
		managedAssistantInstructionsFor(feature.Variant("something-else")),
	)
}

// Tool names must not appear bare in a prompt without the runtime prefix
// somewhere: a bare name is not callable, and a model that copies one spends
// the turn recovering from an unknown-tool error.
func TestInstructionsExplainTheNamePrefix(t *testing.T) {
	t.Parallel()

	for _, variant := range []feature.Variant{
		feature.VariantAssistantToolsLegacy,
		feature.VariantAssistantToolsPlatformMCP,
	} {
		prompt := managedAssistantInstructionsFor(variant)
		require.Contains(t, prompt, "short name",
			"prompt for variant %q must explain that short names are shorthand for the prefixed ones", variant)
		require.Contains(t, prompt, "select:",
			"prompt for variant %q must tell the model to batch schema lookups", variant)
	}
}

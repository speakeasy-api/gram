package mcpversions_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

func TestKnownExtensionMethodRecognizesTheSkillsExtension(t *testing.T) {
	t.Parallel()

	for _, method := range []string{mcpversions.MethodSkillsList, mcpversions.MethodSkillsGet, mcpversions.MethodResourcesDirectoryRead} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			require.True(t, mcpversions.KnownExtensionMethod(method))
			require.Equal(t, mcpversions.ExtensionSkills, mcpversions.MethodExtension(method))
		})
	}
}

// TestExtensionMethodsAreOutsideEveryCoreRevision pins the split the request
// census and method gating rely on: an extension method is never a core
// method, so KnownMethod and DefinesMethod keep reporting it as undefined and
// a surface serves it only on the strength of its capability declaration.
func TestExtensionMethodsAreOutsideEveryCoreRevision(t *testing.T) {
	t.Parallel()

	for _, method := range mcpversions.ExtensionMethods() {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			require.False(t, mcpversions.KnownMethod(method))
			for _, version := range mcpversions.All() {
				require.False(t, mcpversions.DefinesMethod(method, version), "%s/%s", version, method)
			}
		})
	}
}

// TestCoreMethodsAreNotExtensionMethods covers the tasks family, which
// 2026-07-28 moved into an extension: it stays a core method here because an
// earlier revision defined it, and this package recognizes no tasks extension.
func TestCoreMethodsAreNotExtensionMethods(t *testing.T) {
	t.Parallel()

	for _, method := range []string{mcpversions.MethodToolsList, mcpversions.MethodResourcesRead, mcpversions.MethodTasksGet, mcpversions.MethodServerDiscover} {
		require.False(t, mcpversions.KnownExtensionMethod(method), method)
		require.Empty(t, mcpversions.MethodExtension(method), method)
	}
}

func TestKnownExtensionMethodRejectsUnrecognizedInput(t *testing.T) {
	t.Parallel()

	require.False(t, mcpversions.KnownExtensionMethod("skills/update"))
	require.False(t, mcpversions.KnownExtensionMethod("skills/"))
	require.False(t, mcpversions.KnownExtensionMethod(""))
	require.Empty(t, mcpversions.MethodExtension("skills/update"))
}

func TestExtensionMethodsIsSortedAndComplete(t *testing.T) {
	t.Parallel()

	methods := mcpversions.ExtensionMethods()
	require.True(t, slices.IsSorted(methods))
	require.Equal(t, []string{mcpversions.MethodResourcesDirectoryRead, mcpversions.MethodSkillsGet, mcpversions.MethodSkillsList}, methods)
}

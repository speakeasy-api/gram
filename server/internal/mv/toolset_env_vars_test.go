package mv_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mv"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestAssembleEnvironmentVariablesForToolset_DistinctDisplayNamesPerToolset(t *testing.T) {
	t.Parallel()

	// Both toolsets share a tool that needs the same security key "apiKey",
	// but each toolset has its own display-name override configured via its
	// own mcp_metadata row. Batching the raw security fetch must not leak
	// toolset A's display name into toolset B's result.
	sharedEntry := tsr.HttpSecurity{
		ID:           uuid.New(),
		Key:          "apiKey",
		EnvVariables: []string{"API_KEY"},
	}
	securityEntriesByKey := map[string]tsr.HttpSecurity{"apiKey": sharedEntry}

	toolA := []mv.ToolEnvLookupParams{{Security: []byte(`[{"apiKey":[]}]`)}}
	toolB := []mv.ToolEnvLookupParams{{Security: []byte(`[{"apiKey":[]}]`)}}

	varsA, _ := mv.AssembleEnvironmentVariablesForToolset(toolA, securityEntriesByKey, map[string]string{"API_KEY": "Toolset A Key"})
	varsB, _ := mv.AssembleEnvironmentVariablesForToolset(toolB, securityEntriesByKey, map[string]string{"API_KEY": "Toolset B Key"})

	require.Len(t, varsA, 1)
	require.Len(t, varsB, 1)
	require.Equal(t, "Toolset A Key", *varsA[0].DisplayName)
	require.Equal(t, "Toolset B Key", *varsB[0].DisplayName)
}

func TestAssembleEnvironmentVariablesForToolset_ServerVarsComputedPerToolsetNoLeak(t *testing.T) {
	t.Parallel()

	securityEntriesByKey := map[string]tsr.HttpSecurity{}

	toolA := []mv.ToolEnvLookupParams{{ServerEnvVar: "TOOLSET_A_SERVER_URL"}}
	toolB := []mv.ToolEnvLookupParams{{ServerEnvVar: "TOOLSET_B_SERVER_URL"}}

	_, serverVarsA := mv.AssembleEnvironmentVariablesForToolset(toolA, securityEntriesByKey, nil)
	_, serverVarsB := mv.AssembleEnvironmentVariablesForToolset(toolB, securityEntriesByKey, nil)

	require.Len(t, serverVarsA, 1)
	require.Equal(t, []string{"TOOLSET_A_SERVER_URL"}, serverVarsA[0].EnvVariables)
	require.Len(t, serverVarsB, 1)
	require.Equal(t, []string{"TOOLSET_B_SERVER_URL"}, serverVarsB[0].EnvVariables)
}

var _ = types.SecurityVariable{} // keep import if unused by future edits

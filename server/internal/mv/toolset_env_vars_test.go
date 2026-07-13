package mv_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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
	securityEntriesByKey := map[mv.SecurityDefinitionKey]tsr.HttpSecurity{{Key: "apiKey"}: sharedEntry}

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

	securityEntriesByKey := map[mv.SecurityDefinitionKey]tsr.HttpSecurity{}

	toolA := []mv.ToolEnvLookupParams{{ServerEnvVar: "TOOLSET_A_SERVER_URL"}}
	toolB := []mv.ToolEnvLookupParams{{ServerEnvVar: "TOOLSET_B_SERVER_URL"}}

	_, serverVarsA := mv.AssembleEnvironmentVariablesForToolset(toolA, securityEntriesByKey, nil)
	_, serverVarsB := mv.AssembleEnvironmentVariablesForToolset(toolB, securityEntriesByKey, nil)

	require.Len(t, serverVarsA, 1)
	require.Equal(t, []string{"TOOLSET_A_SERVER_URL"}, serverVarsA[0].EnvVariables)
	require.Len(t, serverVarsB, 1)
	require.Equal(t, []string{"TOOLSET_B_SERVER_URL"}, serverVarsB[0].EnvVariables)
}

func TestAssembleEnvironmentVariablesForToolset_DisambiguatesSameKeyAcrossDocuments(t *testing.T) {
	t.Parallel()

	// Two different OpenAPI documents (and deployments) in the same project
	// both define a security scheme named "apiKey", but each backs a
	// different environment variable. A bare-key lookup would let one
	// tool's security variable win arbitrarily for both; resolving by
	// (deployment, document, key) must always pick the entry that actually
	// belongs to the tool being resolved.
	depA := uuid.New()
	depB := uuid.New()
	docA := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	docB := uuid.NullUUID{UUID: uuid.New(), Valid: true}

	entryA := tsr.HttpSecurity{
		ID:                  uuid.New(),
		DeploymentID:        depA,
		Openapiv3DocumentID: docA,
		Key:                 "apiKey",
		EnvVariables:        []string{"DOC_A_API_KEY"},
	}
	entryB := tsr.HttpSecurity{
		ID:                  uuid.New(),
		DeploymentID:        depB,
		Openapiv3DocumentID: docB,
		Key:                 "apiKey",
		EnvVariables:        []string{"DOC_B_API_KEY"},
	}

	securityEntriesByKey := map[mv.SecurityDefinitionKey]tsr.HttpSecurity{
		{DeploymentID: depA, OpenAPIv3DocumentID: docA, Key: "apiKey"}: entryA,
		{DeploymentID: depB, OpenAPIv3DocumentID: docB, Key: "apiKey"}: entryB,
	}

	toolFromDocA := []mv.ToolEnvLookupParams{{
		DeploymentID:        depA,
		OpenAPIv3DocumentID: docA,
		Security:            []byte(`[{"apiKey":[]}]`),
	}}

	vars, _ := mv.AssembleEnvironmentVariablesForToolset(toolFromDocA, securityEntriesByKey, nil)

	require.Len(t, vars, 1)
	require.Equal(t, []string{"DOC_A_API_KEY"}, vars[0].EnvVariables, "tool from doc A must resolve doc A's entry, never doc B's")
}

func TestAssembleEnvironmentVariablesForToolset_StableSortedOrder(t *testing.T) {
	t.Parallel()

	// Ordering previously fell out of Go map iteration, which is randomized
	// per call, so the API shuffled variables on every request and the
	// dashboard env vars table reordered on each refresh (AGE-2985).
	securityEntriesByKey := map[mv.SecurityDefinitionKey]tsr.HttpSecurity{
		{Key: "zulu"}:  {ID: uuid.New(), Key: "zulu", EnvVariables: []string{"ZULU_KEY"}},
		{Key: "mike"}:  {ID: uuid.New(), Key: "mike", EnvVariables: []string{"MIKE_KEY"}},
		{Key: "alpha"}: {ID: uuid.New(), Key: "alpha", EnvVariables: []string{"ALPHA_KEY"}},
	}

	tools := []mv.ToolEnvLookupParams{
		{Security: []byte(`[{"zulu":[]},{"mike":[]}]`), ServerEnvVar: "SERVER_URL_B"},
		{Security: []byte(`[{"alpha":[]}]`), ServerEnvVar: "SERVER_URL_A"},
	}

	for range 20 {
		securityVars, serverVars := mv.AssembleEnvironmentVariablesForToolset(tools, securityEntriesByKey, nil)

		require.Len(t, securityVars, 3)
		require.Equal(t, []string{"ALPHA_KEY"}, securityVars[0].EnvVariables)
		require.Equal(t, []string{"MIKE_KEY"}, securityVars[1].EnvVariables)
		require.Equal(t, []string{"ZULU_KEY"}, securityVars[2].EnvVariables)

		require.Len(t, serverVars, 1)
		require.Equal(t, []string{"SERVER_URL_A", "SERVER_URL_B"}, serverVars[0].EnvVariables)
	}
}

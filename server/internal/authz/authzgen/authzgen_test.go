package authzgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// TestGeneratedFilesAreCurrent fails when a file generated from authz.Model
// is stale. `mise run gen:authz` rewrites them.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	t.Parallel()

	files, err := Render(authz.Model)
	require.NoError(t, err)
	for _, file := range files {
		got, err := os.ReadFile(filepath.Join("..", "..", "..", file.Path))
		require.NoError(t, err, file.Path)
		require.Equal(t, string(file.Content), string(got), "%s is stale; run `mise run gen:authz`", file.Path)
	}
}

func TestScopeConstantName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "ScopeMCPBlockedRead", ScopeConstantName(authz.ScopeMCPBlockedRead))
	require.Equal(t, "ScopeOrgDeviceAgentSync", ScopeConstantName(authz.ScopeOrgDeviceAgentSync))
	require.Equal(t, "ScopeRiskPolicyBypass", ScopeConstantName(authz.ScopeRiskPolicyBypass))
	require.Equal(t, "ScopeRoot", ScopeConstantName(authz.ScopeRoot))
}

func TestRender_RejectsNameCollisions(t *testing.T) {
	t.Parallel()

	model := authz.Model
	model.Types = append([]authz.ResourceType{}, authz.Model.Types...)
	model.Types = append(model.Types, authz.ResourceType{
		Kind:       "org_read",
		Parent:     "",
		Category:   authz.ScopeCategoryOrg,
		Match:      authz.MatchLoose,
		Dimensions: nil,
		Permissions: []authz.Permission{{
			Action: "x", LegacyScope: "org_read:x", Implies: nil, Exclusions: nil, Dimensions: nil, Source: authz.GrantSourceRoles,
			Category: "", Label: "L", Description: "D.", Sensitivity: authz.SensitivityNone, Agent: authz.AgentUnsafe(),
		}},
	})
	// "org:read:x" is not a scope, but org_read:x and an org:read_x would
	// both render ScopeOrgReadX; check the guard on a real collision.
	model.Types[len(model.Types)-1].Permissions = append(model.Types[len(model.Types)-1].Permissions, authz.Permission{
		Action: "y", LegacyScope: "org:read_x", Implies: nil, Exclusions: nil, Dimensions: nil, Source: authz.GrantSourceRoles,
		Category: "", Label: "L", Description: "D.", Sensitivity: authz.SensitivityNone, Agent: authz.AgentUnsafe(),
	})
	_, err := Render(model)
	require.ErrorContains(t, err, "both render as ScopeOrgReadX")
}

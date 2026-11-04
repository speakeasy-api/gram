package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestDescribeAccessPermissionsFollowsTheCatalogue(t *testing.T) {
	t.Parallel()

	output := describeAccessPermissions()
	require.NotEmpty(t, output.Scopes)
	require.NotEmpty(t, output.Tasks)

	bySlug := make(map[string]AccessPermission, len(output.Scopes))
	for _, permission := range output.Scopes {
		require.NotContains(t, bySlug, permission.Slug, "scope listed twice")
		bySlug[permission.Slug] = permission
	}
	for _, permission := range output.Scopes {
		require.NotEmpty(t, permission.Label)
		require.NotEmpty(t, permission.Description)
		require.NotEmpty(t, permission.Category)
		require.NotEmpty(t, permission.ResourceKind)
		require.NotNil(t, permission.SubScopes)
		require.Equal(t, permission.Sensitive, permission.Sensitivity != "")
		require.NotContains(t, permission.Slug, ":blocked_", "exception scopes are not permissions a person chooses")
		// What a permission includes is itself a permission the tool describes.
		for _, sub := range permission.SubScopes {
			require.Contains(t, bySlug, sub, "%s includes %s, which is not described", permission.Slug, sub)
		}
	}

	// The sensitive set is the product decision the catalogue records.
	sensitive := make([]string, 0)
	for _, permission := range output.Scopes {
		if permission.Sensitive {
			sensitive = append(sensitive, permission.Slug)
		}
	}
	want := make([]string, 0)
	for _, scope := range authz.SensitiveScopes() {
		want = append(want, string(scope))
	}
	require.Equal(t, want, sensitive)
	require.Equal(t, "tool_logs", bySlug["log:read"].Sensitivity)
	require.Equal(t, "View tool logs", bySlug["log:read"].Label)

	for _, task := range output.Tasks {
		require.NotEmpty(t, task.Slug)
		require.NotEmpty(t, task.Label)
		for _, scope := range task.Scopes {
			require.Contains(t, bySlug, scope, "task %s names a scope the tool does not describe", task.Slug)
		}
	}
}

func TestDescribeAccessPermissionsOutputsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	output := DescribeAccessPermissionsOutput{
		Scopes: []AccessPermission{{
			Slug: "log:read", Label: "View tool logs", Category: "log", ResourceKind: "project",
			Description: "Read tool calls.", Sensitive: true, Sensitivity: "tool_logs", SubScopes: []string{},
		}},
		Tasks: []AccessTask{{Slug: "session_review", Label: "Review other members' sessions", Scopes: []string{"chat:read", "log:read"}}},
	}
	require.ElementsMatch(t, []string{
		"scopes", "slug", "label", "category", "resource_kind", "description", "sensitive", "sensitivity", "sub_scopes",
		"tasks", "slug", "label", "scopes",
	}, decodeKeys(t, output))

	encoded, err := json.Marshal(describeAccessPermissions())
	require.NoError(t, err)
	for _, forbidden := range []string{"principal_urn", "user_id", "role_id", "email", "selector", "resource_id"} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

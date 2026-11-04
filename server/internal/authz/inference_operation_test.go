package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInferenceExecuteMatchesEngineCheck holds Inference(project).Execute()
// to the engine's InferenceExecuteCheck in every migration mode: a grant
// bounded to the project or to every project passes, and one bounded to
// another project does not.
func TestInferenceExecuteMatchesEngineCheck(t *testing.T) {
	t.Parallel()

	const role = "role:member"
	inference := func(projectID string) Grant {
		grant := grantOf(role, string(ScopeInferenceExecute), ResourceKindInference, WildcardResource)
		if projectID != "" {
			grant.Selector[SelectorKeyProjectID] = projectID
		}
		return grant
	}
	cases := []struct {
		name   string
		grants []Grant
		allow  bool
	}{
		{name: "grant on the project", grants: []Grant{inference("proj_1")}, allow: true},
		{name: "grant on every project", grants: []Grant{inference("")}, allow: true},
		{name: "grant on another project", grants: []Grant{inference("proj_2")}, allow: false},
		{name: "no grants", grants: nil, allow: false},
	}
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		client, engine := newRoutedTestClient(t, nil, fixedModes(mode), nil)
		for _, tc := range cases {
			t.Run(mode.String()+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				ctx := humanClientContext(t, tc.grants...)
				engineErr := engine.Require(ctx, InferenceExecuteCheck("proj_1"))
				require.Equal(t, tc.allow, engineErr == nil, "the case must describe the engine's answer")
				require.Equal(t, tc.allow, client.Require(ctx, Inference("proj_1").Execute()) == nil)
				allowed, err := client.Evaluate(ctx, Inference("proj_1").Execute())
				require.NoError(t, err)
				require.Equal(t, tc.allow, allowed)
			})
		}
	}
}

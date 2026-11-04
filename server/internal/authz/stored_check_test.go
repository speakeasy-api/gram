package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateGrantsForStoredCheckMatchesGrantsAuthorize(t *testing.T) {
	t.Parallel()

	owner := "role:organization:role_1"
	declared := Check{Scope: ScopeProjectRead, ResourceKind: ResourceKindProject, ResourceID: "proj_1", Dimensions: nil}.WithStrictSelectorMatch()
	undeclared := Check{Scope: ScopeAgentRead, ResourceKind: "", ResourceID: "org_1", Dimensions: nil}.WithStrictSelectorMatch()
	cases := []struct {
		name   string
		grants []Grant
		check  Check
	}{
		{name: "declared and granted", grants: []Grant{grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_1")}, check: declared},
		{name: "declared wildcard under strict match", grants: []Grant{grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "*")}, check: declared},
		{name: "declared and blocked", grants: []Grant{
			grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_1"),
			grantOf(owner, string(ScopeProjectBlockedRead), ResourceKindProject, "proj_1"),
		}, check: declared},
		{name: "declared without grants", grants: nil, check: declared},
		{name: "undeclared and granted", grants: []Grant{grantOf(owner, string(ScopeAgentRead), "", "org_1")}, check: undeclared},
		{name: "undeclared without grants", grants: nil, check: undeclared},
	}

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			ctx := humanClientContext(t)
			for _, tc := range cases {
				want, wantErr := GrantsAuthorize(tc.grants, tc.check)
				got, err := client.EvaluateGrantsForStoredCheck(ctx, tc.grants, tc.check)
				require.Equal(t, wantErr == nil, err == nil, tc.name)
				require.Equal(t, want, got, tc.name)
			}
			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
			if mode == MigrationModeShadow {
				// Only the four declared checks have a registry answer to
				// compare; undeclared ones stay with the engine.
				require.Len(t, recorder.outcomes(), 4)
			}
		})
	}
}

package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateGrantsForStoredCheckMatchesGrantsAuthorize(t *testing.T) {
	t.Parallel()

	owner := "role:organization:role_1"
	declared := Check{Scope: ScopeProjectRead, ResourceKind: ResourceKindProject, ResourceID: "proj_1", Dimensions: nil}.WithStrictSelectorMatch()
	workload := Check{Scope: ScopeWorkloadRead, ResourceKind: "", ResourceID: "org_1", Dimensions: nil}.WithStrictSelectorMatch()
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
		{name: "organization-wide scope granted", grants: []Grant{grantOf(owner, string(ScopeWorkloadRead), "", "org_1")}, check: workload},
		{name: "organization-wide scope without grants", grants: nil, check: workload},
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
				// Every stored scope is declared, so every check has a
				// registry answer to compare.
				require.Len(t, recorder.outcomes(), len(cases))
			}
		})
	}
}

// A stored check can name a scope the registry no longer declares, such as a
// challenge recorded before the scope was retired. It is decided as
// GrantsAuthorize decides it and never reaches the registry.
func TestEvaluateGrantsForStoredCheckDecidesUndeclaredScopeFromGrants(t *testing.T) {
	t.Parallel()

	owner := "role:organization:role_1"
	retired := Scope("retired:read")
	check := Check{Scope: retired, ResourceKind: "", ResourceID: "org_1", Dimensions: nil}.WithStrictSelectorMatch()
	cases := []struct {
		name   string
		grants []Grant
	}{
		{name: "granted", grants: []Grant{grantOf(owner, string(retired), "", "org_1")}},
		{name: "granted elsewhere", grants: []Grant{grantOf(owner, string(retired), "", "org_2")}},
		{name: "without grants", grants: nil},
	}

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			ctx := humanClientContext(t)
			for _, tc := range cases {
				want, wantErr := GrantsAuthorize(tc.grants, check)
				got, err := client.EvaluateGrantsForStoredCheck(ctx, tc.grants, check)
				require.Equal(t, wantErr, err, tc.name)
				require.Equal(t, want, got, tc.name)
			}
			require.Empty(t, recorder.outcomes())
		})
	}
}

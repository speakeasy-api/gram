package authz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEvaluateGrantsRiskPolicyFoundationCorpus runs the frozen risk policy
// applicability decisions through EvaluateGrants in every migration mode. The
// expectations come from the fixtures, so the old engine path and the
// registry's evaluate permission must both reproduce them.
func TestEvaluateGrantsRiskPolicyFoundationCorpus(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join(foundationCompatibilityDir, "precedence.json"))
	require.NoError(t, err)
	var file foundationFixtureFile
	require.NoError(t, json.Unmarshal(raw, &file))

	var fixtures []foundationFixture
	for _, fixture := range file.Cases {
		if fixture.Operation == "risk_policy_applies" {
			fixtures = append(fixtures, fixture)
		}
	}
	require.Len(t, fixtures, 4, "the frozen risk policy corpus changed; check every new case runs")

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			ctx := humanClientContext(t)
			for _, fixture := range fixtures {
				require.Len(t, fixture.Checks, 1, fixture.Name)
				check := fixture.Checks[0]
				require.Equal(t, ScopeRiskPolicyEvaluate, check.Scope, fixture.Name)
				op := RiskPolicy(check.ResourceID, RiskPolicyDimensions{
					ServerURL:      check.Dimensions[SelectorKeyServerURL],
					ServerIdentity: check.Dimensions[SelectorKeyServerIdentity],
				}).Evaluate()
				got, err := client.EvaluateGrants(ctx, foundationGrants(fixture.Grants), op)
				require.NoError(t, err, fixture.Name)
				require.Equal(t, fixture.Want == "allow", got, fixture.Name)
			}
			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
		})
	}
}

// TestEvaluateGrantsRiskPolicyBypassAgreesAcrossModes pins the strict
// matching of bypass grants: a grant bounded to one server never covers a
// policy-wide question, and an unbounded grant covers every server.
func TestEvaluateGrantsRiskPolicyBypassAgreesAcrossModes(t *testing.T) {
	t.Parallel()

	user := "user:" + fixtureUserID
	bounded := grantOf(user, string(ScopeRiskPolicyBypass), ResourceKindRiskPolicy, "pol_1", SelectorKeyServerURL, "https://a.example")
	unbounded := grantOf(user, string(ScopeRiskPolicyBypass), ResourceKindRiskPolicy, "pol_1")
	serverA := RiskPolicyDimensions{ServerURL: "https://a.example", ServerIdentity: ""}
	serverB := RiskPolicyDimensions{ServerURL: "https://b.example", ServerIdentity: ""}
	whole := RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}
	cases := []struct {
		name   string
		grants []Grant
		op     Operation
		want   bool
	}{
		{name: "bounded bypass, same server", grants: []Grant{bounded}, op: RiskPolicy("pol_1", serverA).Bypass(), want: true},
		{name: "bounded bypass, other server", grants: []Grant{bounded}, op: RiskPolicy("pol_1", serverB).Bypass(), want: false},
		{name: "bounded bypass, whole policy", grants: []Grant{bounded}, op: RiskPolicy("pol_1", whole).Bypass(), want: false},
		{name: "unbounded bypass, any server", grants: []Grant{unbounded}, op: RiskPolicy("pol_1", serverB).Bypass(), want: true},
		{name: "unbounded bypass, other policy", grants: []Grant{unbounded}, op: RiskPolicy("pol_2", serverA).Bypass(), want: false},
		{name: "no grants", grants: nil, op: RiskPolicy("pol_1", serverA).Bypass(), want: false},
	}

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			ctx := humanClientContext(t)
			for _, tc := range cases {
				got, err := client.EvaluateGrants(ctx, tc.grants, tc.op)
				require.NoError(t, err, tc.name)
				require.Equal(t, tc.want, got, tc.name)
			}
			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
			if mode == MigrationModeShadow {
				require.Len(t, recorder.outcomes(), len(cases))
			}
		})
	}
}

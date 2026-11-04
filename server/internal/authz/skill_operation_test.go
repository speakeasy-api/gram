package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSkillReadAcceptsProjectOrSkillGrants holds Project(org, project).Skill(skill).Read()
// to the engine's RequireAnyUnblocked over the project-wide and the per-skill
// check, in every migration mode: either grant passes, and a block on either
// refuses.
func TestSkillReadAcceptsProjectOrSkillGrants(t *testing.T) {
	t.Parallel()

	const role = "role:member"
	skill := Project("org_1", "proj_1").Skill("skill_1")
	cases := []struct {
		name   string
		grants []Grant
		allow  bool
	}{
		{name: "project-wide grant", grants: []Grant{grantOf(role, string(ScopeSkillRead), ResourceKindSkill, "proj_1")}, allow: true},
		{name: "grant on the skill", grants: []Grant{grantOf(role, string(ScopeSkillRead), ResourceKindSkill, "skill_1")}, allow: true},
		{name: "skill write implies read", grants: []Grant{grantOf(role, string(ScopeSkillWrite), ResourceKindSkill, "skill_1")}, allow: true},
		{name: "grant on another skill", grants: []Grant{grantOf(role, string(ScopeSkillRead), ResourceKindSkill, "skill_2")}, allow: false},
		{name: "block on the skill vetoes the project grant", grants: []Grant{
			grantOf(role, string(ScopeSkillRead), ResourceKindSkill, "proj_1"),
			grantOf(role, string(ScopeSkillBlockedRead), ResourceKindSkill, "skill_1"),
		}, allow: false},
		{name: "project-wide block vetoes the skill grant", grants: []Grant{
			grantOf(role, string(ScopeSkillRead), ResourceKindSkill, "skill_1"),
			grantOf(role, string(ScopeSkillBlockedRead), ResourceKindSkill, "proj_1"),
		}, allow: false},
		{name: "no grants", grants: nil, allow: false},
	}
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		client, engine := newRoutedTestClient(t, nil, fixedModes(mode), nil)
		for _, tc := range cases {
			t.Run(mode.String()+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				ctx := humanClientContext(t, tc.grants...)
				engineErr := engine.RequireAnyUnblocked(ctx,
					Check{Scope: ScopeSkillRead, ResourceKind: "", ResourceID: "proj_1", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""},
					Check{Scope: ScopeSkillRead, ResourceKind: ResourceKindSkill, ResourceID: "skill_1", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""},
				)
				require.Equal(t, tc.allow, engineErr == nil, "the case must describe the engine's answer")
				err := client.Require(ctx, skill.Read())
				require.Equal(t, tc.allow, err == nil)
				allowed, err := client.Evaluate(ctx, skill.Read())
				require.NoError(t, err)
				require.Equal(t, tc.allow, allowed)
			})
		}
	}
}

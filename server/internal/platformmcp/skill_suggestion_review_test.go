package platformmcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	testSuggestionID  = "77777777-7777-4777-8777-777777777777"
	testChangeIDFirst = "88888888-8888-4888-8888-888888888888"
	testChangeIDOther = "99999999-9999-4999-8999-999999999999"
	testNewVersionID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

func testSuggestion(status string, changeIDs ...string) *types.SkillEditSuggestion {
	changes := make([]*types.SkillEditSuggestionChange, 0, len(changeIDs))
	for _, id := range changeIDs {
		changes = append(changes, &types.SkillEditSuggestionChange{
			ID:                   id,
			ProposedDiff:         "--- a/SKILL.md\n+++ b/SKILL.md\n",
			Rationale:            "tighten the instructions",
			AppliesCleanly:       true,
			FeedbackCount:        1,
			FeedbackSessionCount: 1,
			CreatedAt:            "2026-09-30T00:00:00Z",
		})
	}
	return &types.SkillEditSuggestion{
		ID:                   testSuggestionID,
		SkillID:              testSkillID,
		SkillName:            "add-mcp",
		SkillDisplayName:     "Add MCP",
		BaseVersionID:        testSkillVersionID,
		Changes:              changes,
		ProposedContent:      "---\nname: add-mcp\n---\nproposed\n",
		AppliesCleanly:       true,
		Rationale:            "feedback shows a gap",
		Status:               status,
		FeedbackCount:        2,
		FeedbackSessionCount: 2,
		ScoredSessionCount:   5,
		ApprovedByUserID:     nil,
		ApprovedAt:           nil,
		CreatedAt:            "2026-09-30T00:00:00Z",
		UpdatedAt:            "2026-09-30T00:00:00Z",
	}
}

// approvedSkill is the skill as the read-back after approval finds it: its
// latest version is the one the approval recorded.
func approvedSkill() *types.Skill {
	skill := testSkill()
	latest := testNewVersionID
	skill.LatestVersionID = &latest
	skill.VersionCount = 3
	return skill
}

func newVersion() *types.SkillVersion {
	version := testSkillVersion("---\nname: add-mcp\n---\nproposed\n")
	version.ID = testNewVersionID
	return version
}

func TestApproveSkillSuggestionTakesTheNamedChangesAndReportsTheCommittedVersion(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill: approvedSkill(),
		approveOut: &genskills.ApproveSkillSuggestionResult{
			Suggestion: testSuggestion("approved", testChangeIDFirst, testChangeIDOther),
			Outcome:    string(SkillSuggestionApplied),
			Version:    newVersion(),
		},
	}
	service := testSkillsService(t, skills)

	output, err := service.ApproveSkillSuggestion(t.Context(), testPrincipal(), ApproveSkillSuggestionInput{
		ProjectSlug:  testSkillProjectSlug,
		SuggestionID: testSuggestionID,
		ChangeIDs:    []string{testChangeIDFirst, testChangeIDOther},
		Content:      "",
	})

	require.NoError(t, err)
	require.NotNil(t, skills.approved)
	require.Equal(t, testSuggestionID, skills.approved.ID)
	require.Equal(t, []string{testChangeIDFirst, testChangeIDOther}, skills.approved.ChangeIds)
	require.Nil(t, skills.approved.Content)
	require.Equal(t, SkillSuggestionApplied, output.Outcome)
	require.NotNil(t, output.Version)
	require.Equal(t, testNewVersionID, output.Version.ID)
	require.Empty(t, output.Version.Content, "the result never echoes a whole manifest")
	require.Equal(t, testNewVersionID, output.Skill.LatestVersionID, "the skill is read back after the write")
	require.Nil(t, output.Remaining)
}

func TestApproveSkillSuggestionRecordsEditedContentWithoutChangeIDs(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill: approvedSkill(),
		approveOut: &genskills.ApproveSkillSuggestionResult{
			Suggestion: testSuggestion("approved", testChangeIDFirst),
			Outcome:    string(SkillSuggestionApplied),
			Version:    newVersion(),
		},
	}
	service := testSkillsService(t, skills)
	edited := "---\nname: add-mcp\n---\ncorrected\n"

	_, err := service.ApproveSkillSuggestion(t.Context(), testPrincipal(), ApproveSkillSuggestionInput{
		ProjectSlug:  testSkillProjectSlug,
		SuggestionID: testSuggestionID,
		ChangeIDs:    nil,
		Content:      edited,
	})

	require.NoError(t, err)
	require.NotNil(t, skills.approved.Content)
	require.Equal(t, edited, *skills.approved.Content)
	require.Empty(t, skills.approved.ChangeIds)
}

func TestApproveSkillSuggestionRequiresExactlyOneOfChangeIDsOrContent(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		changeIDs []string
		content   string
	}{
		{name: "neither", changeIDs: nil, content: ""},
		{name: "both", changeIDs: []string{testChangeIDFirst}, content: "---\nname: add-mcp\n---\n"},
		{name: "malformed change id", changeIDs: []string{"not-a-uuid"}, content: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			skills := &recordingSkillsManagement{skill: testSkill()}
			service := testSkillsService(t, skills)

			_, err := service.ApproveSkillSuggestion(t.Context(), testPrincipal(), ApproveSkillSuggestionInput{
				ProjectSlug:  testSkillProjectSlug,
				SuggestionID: testSuggestionID,
				ChangeIDs:    test.changeIDs,
				Content:      test.content,
			})

			require.ErrorIs(t, err, ErrRegistrationInvalid)
			require.Nil(t, skills.approved, "nothing reaches the skills service")
		})
	}
}

func TestApproveSkillSuggestionReturnsWhatStaysProposedAfterAPartialApproval(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill: approvedSkill(),
		approveOut: &genskills.ApproveSkillSuggestionResult{
			Suggestion: testSuggestion("open", testChangeIDOther),
			Outcome:    string(SkillSuggestionPartiallyApplied),
			Version:    newVersion(),
		},
	}
	service := testSkillsService(t, skills)

	output, err := service.ApproveSkillSuggestion(t.Context(), testPrincipal(), ApproveSkillSuggestionInput{
		ProjectSlug:  testSkillProjectSlug,
		SuggestionID: testSuggestionID,
		ChangeIDs:    []string{testChangeIDFirst},
		Content:      "",
	})

	require.NoError(t, err)
	require.Equal(t, SkillSuggestionPartiallyApplied, output.Outcome)
	require.NotNil(t, output.Remaining)
	require.Len(t, output.Remaining.Changes, 1)
	require.Equal(t, testChangeIDOther, output.Remaining.Changes[0].ID)
	require.Empty(t, output.Remaining.ProposedContent)
}

func TestApproveSkillSuggestionReportsASupersededSuggestionRecordedNothing(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill: testSkill(),
		approveOut: &genskills.ApproveSkillSuggestionResult{
			Suggestion: testSuggestion("superseded", testChangeIDFirst),
			Outcome:    string(SkillSuggestionSuperseded),
			Version:    nil,
		},
	}
	service := testSkillsService(t, skills)

	output, err := service.ApproveSkillSuggestion(t.Context(), testPrincipal(), ApproveSkillSuggestionInput{
		ProjectSlug:  testSkillProjectSlug,
		SuggestionID: testSuggestionID,
		ChangeIDs:    []string{testChangeIDFirst},
		Content:      "",
	})

	require.NoError(t, err)
	require.Equal(t, SkillSuggestionSuperseded, output.Outcome)
	require.Nil(t, output.Version)
	require.Contains(t, output.NextAction, "Nothing was recorded")
	require.Equal(t, testSkillVersionID, output.Skill.LatestVersionID)
}

func TestDismissSkillSuggestionForwardsTheSuggestionAndReportsItsState(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill(), dismissOut: testSuggestion("dismissed", testChangeIDFirst)}
	service := testSkillsService(t, skills)

	output, err := service.DismissSkillSuggestion(t.Context(), testPrincipal(), DismissSkillSuggestionInput{
		ProjectSlug:  testSkillProjectSlug,
		SuggestionID: testSuggestionID,
	})

	require.NoError(t, err)
	require.NotNil(t, skills.dismissed)
	require.Equal(t, testSuggestionID, skills.dismissed.ID)
	require.Equal(t, "dismissed", output.Suggestion.Status)
}

func TestSkillSuggestionReviewSurfacesServiceConflictsAsStructuredRefusals(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill(), err: oops.E(oops.CodeConflict, nil, "skill suggestion is not open")}
	_, registrar := newTestServer(t, func(services *Services) { services.Skills = testSkillsService(t, skills) })
	descriptor := skillDescriptor(t, registrar, "approve_skill_suggestion")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(fmt.Sprintf(`{"project_slug":%q,"suggestion_id":%q,"change_ids":[%q],"confirmed":true}`, testSkillProjectSlug, testSuggestionID, testChangeIDFirst)))

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var body skillsRefusalResult
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &body))
	require.Equal(t, "conflict", body.Code)
	require.Contains(t, body.Message, "not open")
}

func TestSkillSuggestionReviewToolsRefuseWithoutConfirmation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		tool string
		args string
	}{
		{tool: "approve_skill_suggestion", args: fmt.Sprintf(`{"project_slug":%q,"suggestion_id":%q,"change_ids":[%q],"confirmed":false}`, testSkillProjectSlug, testSuggestionID, testChangeIDFirst)},
		{tool: "dismiss_skill_suggestion", args: fmt.Sprintf(`{"project_slug":%q,"suggestion_id":%q,"confirmed":false}`, testSkillProjectSlug, testSuggestionID)},
	} {
		t.Run(test.tool, func(t *testing.T) {
			t.Parallel()

			skills := &recordingSkillsManagement{skill: testSkill()}
			_, registrar := newTestServer(t, func(services *Services) { services.Skills = testSkillsService(t, skills) })
			descriptor := skillDescriptor(t, registrar, test.tool)

			_, err := descriptor.Invoke(t.Context(), json.RawMessage(test.args))

			var refusal *ToolRefusalError
			require.ErrorAs(t, err, &refusal)
			var body skillsRefusalResult
			require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &body))
			require.Equal(t, "confirmation_required", body.Code)
			require.Contains(t, body.Message, "confirmed: true")
			require.Nil(t, skills.approved)
			require.Nil(t, skills.dismissed)
		})
	}
}

// Approving publishes a new version to everyone who already carries the skill,
// so review stays on the external Platform MCP rather than the in-product
// assistant. The declaration must hold whether or not skills are composed.
func TestSkillSuggestionReviewToolsAreExternalWritesWithAndWithoutTheirDependencies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		build func() *SkillsService
	}{
		{name: "composed", build: func() *SkillsService { return testSkillsService(t, &recordingSkillsManagement{skill: testSkill()}) }},
		{name: "absent", build: func() *SkillsService { return nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, registrar := newTestServer(t, func(services *Services) { services.Skills = test.build() })
			for _, name := range []string{"approve_skill_suggestion", "dismiss_skill_suggestion"} {
				descriptor := skillDescriptor(t, registrar, name)
				require.Equal(t, externalOnly, descriptor.Meta.Audiences)
				require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
				require.Equal(t, discoverySkillWrite, descriptor.Meta.DiscoveryScopes)
				require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
				require.NotNil(t, descriptor.Annotations)
				require.False(t, descriptor.Annotations.ReadOnlyHint)
				require.NotNil(t, descriptor.Annotations.DestructiveHint)
				require.Equal(t, name == "dismiss_skill_suggestion", *descriptor.Annotations.DestructiveHint)
				require.Equal(t, name == "dismiss_skill_suggestion", descriptor.Annotations.IdempotentHint)
			}
		})
	}
}

func skillDescriptor(t *testing.T, registrar *Registrar, name string) Descriptor {
	t.Helper()
	for _, descriptor := range registrar.Descriptors() {
		if descriptor.Name == name {
			return descriptor
		}
	}
	require.Failf(t, "tool not registered", "%s", name)
	return Descriptor{}
}

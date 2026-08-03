package skills_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestTriggerSkillSuggestionSignalsForcedAnalysis(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "trigger-suggestion", "Trigger suggestion.")

	err := ti.service.TriggerSuggestion(ctx, &gen.TriggerSuggestionPayload{
		ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []suggestionSignal{{projectID: ti.projectID, skillID: uuid.MustParse(created.Skill.ID)}}, ti.signaler.recorded())
}

func TestTriggerSkillSuggestionRequiresWriteAccessAndExistingSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "trigger-suggestion-access", "Trigger suggestion access.")
	payload := &gen.TriggerSuggestionPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}

	err := ti.service.TriggerSuggestion(authztest.WithExactGrants(t, ctx), payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.signaler.recorded())

	payload.ID = uuid.NewString()
	err = ti.service.TriggerSuggestion(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)

	payload.ID = "invalid"
	err = ti.service.TriggerSuggestion(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestTriggerSkillSuggestionReportsSignalerFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "trigger-suggestion-failure", "Trigger suggestion failure.")
	ti.signaler.err = errors.New("workflow unavailable")

	err := ti.service.TriggerSuggestion(ctx, &gen.TriggerSuggestionPayload{
		ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

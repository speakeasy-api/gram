package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
	feedbackrecorder "github.com/speakeasy-api/gram/server/internal/skills/feedback"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type feedbackInput struct {
	Skill   string                       `json:"skill" jsonschema:"Canonical name of the skill that was used."`
	Outcome domainskills.FeedbackOutcome `json:"outcome" jsonschema:"How the skill affected the task."`
	Note    *string                      `json:"note,omitempty" jsonschema:"Optional concise context about the outcome."`
}

type Feedback struct {
	db         *pgxpool.Pool
	recorder   *feedbackrecorder.Recorder
	descriptor core.ToolDescriptor
}

func NewAssistantFeedbackTool(db *pgxpool.Pool, recorder *feedbackrecorder.Recorder) *Feedback {
	readOnly, destructive, idempotent, openWorld := false, false, false, false
	minSkillLength, maxSkillLength, maxNoteLength := 1, 64, domainskills.MaxFeedbackNoteRunes
	return &Feedback{
		db:       db,
		recorder: recorder,
		descriptor: core.ToolDescriptor{
			SourceSlug:  "skills",
			HandlerName: "assistant_feedback",
			Name:        platformtools.ToolNamePlatformSkillFeedback,
			Description: "Record feedback only after materially relying on a loaded skill while completing a task. Call skills_load for that skill first.",
			InputSchema: core.BuildInputSchema[feedbackInput](
				core.WithPropertyMutator("skill", func(prop *jsonschema.Schema) {
					prop.MinLength = &minSkillLength
					prop.MaxLength = &maxSkillLength
					prop.Pattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
				}),
				core.WithPropertyEnum("outcome",
					string(domainskills.FeedbackOutcomeHelped),
					string(domainskills.FeedbackOutcomePartiallyHelped),
					string(domainskills.FeedbackOutcomeDidNotHelp),
					string(domainskills.FeedbackOutcomeMisleading),
					string(domainskills.FeedbackOutcomeHarmful),
				),
				core.WithPropertyMutator("note", func(prop *jsonschema.Schema) {
					prop.MaxLength = &maxNoteLength
				}),
			),
			Variables: nil,
			Annotations: &types.ToolAnnotations{
				Title:           nil,
				ReadOnlyHint:    &readOnly,
				DestructiveHint: &destructive,
				IdempotentHint:  &idempotent,
				OpenWorldHint:   &openWorld,
			},
			Managed:   true,
			OwnerKind: nil,
			OwnerID:   nil,
		},
	}
}

func (t *Feedback) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *Feedback) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "skill feedback requires a project auth context")
	}

	var input feedbackInput
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	input.Skill = strings.TrimSpace(input.Skill)
	note := conv.PtrValOr(input.Note, "")
	switch {
	case !domainskills.ValidSpecName(input.Skill):
		return oops.E(oops.CodeBadRequest, nil, "skill must be a canonical 1-64 character skill name")
	case !input.Outcome.Valid():
		return oops.E(oops.CodeBadRequest, nil, "invalid feedback outcome")
	case utf8.RuneCountInString(note) > domainskills.MaxFeedbackNoteRunes:
		return oops.E(oops.CodeBadRequest, nil, "feedback note must be at most %d Unicode characters", domainskills.MaxFeedbackNoteRunes)
	}

	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "skill feedback requires an assistant principal")
	}
	if principal.ThreadID == uuid.Nil {
		return oops.E(oops.CodeUnauthorized, nil, "skill feedback requires an assistant thread")
	}

	observation, err := assistantrepo.New(t.db).GetAssistantSkillFeedbackObservation(ctx, assistantrepo.GetAssistantSkillFeedbackObservationParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: principal.AssistantID,
		ThreadID:    principal.ThreadID,
		SkillName:   input.Skill,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeBadRequest, nil, "no loaded observation for this skill; call skills_load for this skill before submitting feedback")
	case err != nil:
		return fmt.Errorf("resolve assistant skill observation: %w", err)
	}

	if _, err := t.recorder.Record(ctx, feedbackrecorder.RecordInput{
		ProjectID:      *authCtx.ProjectID,
		SkillID:        uuid.NullUUID{UUID: observation.SkillID, Valid: true},
		SkillVersionID: uuid.NullUUID{UUID: observation.SkillVersionID, Valid: true},
		SkillName:      observation.SkillName,
		Source:         domainskills.FeedbackSourceAssistant,
		Outcome:        input.Outcome,
		Note:           note,
		SessionID:      observation.ChatID.String(),
		UserID:         authCtx.UserID,
		UserEmail:      conv.PtrValOr(authCtx.Email, ""),
	}); err != nil {
		return fmt.Errorf("record skill feedback: %w", err)
	}
	return core.EncodeResult(wr, map[string]bool{"recorded": true})
}

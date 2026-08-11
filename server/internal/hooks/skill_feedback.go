package hooks

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills"
	feedbackrecorder "github.com/speakeasy-api/gram/server/internal/skills/feedback"
)

const hookSkillFeedbackSchemaV1 = "hook.skill-feedback.v1"

func (s *Service) SkillFeedback(ctx context.Context, payload *gen.SkillFeedbackPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if s.productFeatures == nil {
		return oops.E(oops.CodeUnexpected, nil, "skill feedback settings are unavailable")
	}

	skillsEnabled, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check skills entitlement")
	}
	if !skillsEnabled {
		return nil
	}

	if payload == nil || payload.SchemaVersion != hookSkillFeedbackSchemaV1 {
		return oops.E(oops.CodeBadRequest, nil, "unsupported skill feedback schema_version")
	}
	outcome := skills.FeedbackOutcome(payload.Outcome)
	if !outcome.Valid() {
		return oops.E(oops.CodeBadRequest, nil, "invalid feedback outcome")
	}

	recorder := feedbackrecorder.NewRecorder(s.db, s.logger, s.suggestionSignaler)
	if _, err := recorder.Record(ctx, feedbackrecorder.RecordInput{
		ProjectID:      *authCtx.ProjectID,
		SkillID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SkillVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SkillName:      payload.Skill,
		Source:         skills.FeedbackSourceDev,
		Outcome:        outcome,
		Note:           conv.PtrValOr(payload.Note, ""),
		SessionID:      "",
		UserID:         authCtx.UserID,
		UserEmail:      conv.PtrValOr(authCtx.Email, ""),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record skill feedback").LogError(ctx, s.logger)
	}
	return nil
}

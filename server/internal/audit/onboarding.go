package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	onboardinggen "github.com/speakeasy-api/gram/server/gen/onboarding"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Onboarding events are recorded against the organization subject: onboarding
// has no id of its own, one organization has exactly one onboarding.
const (
	ActionOrganizationOnboardingAnswersUpdated Action = "organization:onboarding_answers_updated"
	ActionOrganizationOnboardingStepVerified   Action = "organization:onboarding_step_verified"
)

type LogOrganizationOnboardingAnswersUpdatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	AnswersSnapshotBefore *onboardinggen.OnboardingAnswers
	AnswersSnapshotAfter  *onboardinggen.OnboardingAnswers
}

func (l *Logger) LogOrganizationOnboardingAnswersUpdated(ctx context.Context, dbtx repo.DBTX, event LogOrganizationOnboardingAnswersUpdatedEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AnswersSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionOrganizationOnboardingAnswersUpdated, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.AnswersSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionOrganizationOnboardingAnswersUpdated, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionOrganizationOnboardingAnswersUpdated),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationOnboardingV1})
}

type LogOrganizationOnboardingStepVerifiedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	StepSlug string
	UseCase  string
	Evidence string
	// Completed is true when this verification covered the use case and
	// finished onboarding.
	Completed bool
}

func (l *Logger) LogOrganizationOnboardingStepVerified(ctx context.Context, dbtx repo.DBTX, event LogOrganizationOnboardingStepVerifiedEvent) error {
	metadata, err := marshalAuditPayload(map[string]any{
		"step_slug": event.StepSlug,
		"use_case":  event.UseCase,
		"evidence":  event.Evidence,
		"completed": event.Completed,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", ActionOrganizationOnboardingStepVerified, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionOrganizationOnboardingStepVerified),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationOnboardingV1})
}

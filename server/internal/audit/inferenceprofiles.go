package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/inference_profiles"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionInferenceProfileCreate Action = "inference_profile:create"
	ActionInferenceProfileUpdate Action = "inference_profile:update"
	ActionInferenceProfileDelete Action = "inference_profile:delete"
)

type LogInferenceProfileCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InferenceProfileURN urn.InferenceProfile
	ProfileName         string

	InferenceProfileSnapshotAfter *gen.InferenceProfile
}

type LogInferenceProfileUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InferenceProfileURN urn.InferenceProfile
	ProfileName         string

	InferenceProfileSnapshotBefore *gen.InferenceProfile
	InferenceProfileSnapshotAfter  *gen.InferenceProfile
}

type LogInferenceProfileDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InferenceProfileURN urn.InferenceProfile
	ProfileName         string

	InferenceProfileSnapshotBefore *gen.InferenceProfile
}

func (l *Logger) LogInferenceProfileCreate(ctx context.Context, dbtx repo.DBTX, event LogInferenceProfileCreateEvent) error {
	after, err := marshalAuditPayload(event.InferenceProfileSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionInferenceProfileCreate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      inferenceProfileAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionInferenceProfileCreate, subjectTypeInferenceProfile, event.InferenceProfileURN.ID.String(), event.ProfileName, nil, after),
		OutboxEvent: events.InferenceProfileV1,
	})
}

func (l *Logger) LogInferenceProfileUpdate(ctx context.Context, dbtx repo.DBTX, event LogInferenceProfileUpdateEvent) error {
	before, err := marshalAuditPayload(event.InferenceProfileSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionInferenceProfileUpdate, err)
	}
	after, err := marshalAuditPayload(event.InferenceProfileSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionInferenceProfileUpdate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      inferenceProfileAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionInferenceProfileUpdate, subjectTypeInferenceProfile, event.InferenceProfileURN.ID.String(), event.ProfileName, before, after),
		OutboxEvent: events.InferenceProfileV1,
	})
}

func (l *Logger) LogInferenceProfileDelete(ctx context.Context, dbtx repo.DBTX, event LogInferenceProfileDeleteEvent) error {
	before, err := marshalAuditPayload(event.InferenceProfileSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionInferenceProfileDelete, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      inferenceProfileAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionInferenceProfileDelete, subjectTypeInferenceProfile, event.InferenceProfileURN.ID.String(), event.ProfileName, before, nil),
		OutboxEvent: events.InferenceProfileV1,
	})
}

// inferenceProfileAuditParams builds the org-scoped entry shared by the
// inference_profile and inference_profile_rule subjects.
func inferenceProfileAuditParams(
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	action Action,
	subject subjectType,
	subjectID string,
	subjectDisplayName string,
	beforeSnapshot []byte,
	afterSnapshot []byte,
) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          subjectID,
		SubjectType:        string(subject),
		SubjectDisplayName: conv.ToPGTextEmpty(subjectDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}
}

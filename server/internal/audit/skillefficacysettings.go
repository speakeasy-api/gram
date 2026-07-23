package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const ActionSkillEfficacySettingsUpsert Action = "skill_efficacy_settings:upsert"

type SkillEfficacySettingsSnapshot struct {
	Enabled bool `json:"enabled"`
	// DailyCap counts session evaluations per UTC day. Older entries carry the
	// pre-unification per_skill_daily_cap/org_daily_cap/new_version_burst
	// fields instead.
	DailyCap int32 `json:"daily_cap"`
}

type LogSkillEfficacySettingsUpsertEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SkillEfficacySettingsURN urn.SkillEfficacySettings

	SkillEfficacySettingsSnapshotBefore *SkillEfficacySettingsSnapshot
	SkillEfficacySettingsSnapshotAfter  *SkillEfficacySettingsSnapshot
}

func (l *Logger) LogSkillEfficacySettingsUpsert(ctx context.Context, dbtx repo.DBTX, event LogSkillEfficacySettingsUpsertEvent) error {
	action := ActionSkillEfficacySettingsUpsert

	beforeSnapshot, err := marshalAuditPayload(event.SkillEfficacySettingsSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.SkillEfficacySettingsSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SkillEfficacySettingsURN.ID,
		SubjectType:        string(subjectTypeSkillEfficacySettings),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SkillEfficacySettingsV1})
}

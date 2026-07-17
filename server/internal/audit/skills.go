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

const (
	ActionSkillCreate     Action = "skill:create"
	ActionSkillAddVersion Action = "skill:add_version"
	ActionSkillArchive    Action = "skill:archive"
)

// SkillSnapshot captures content-free parent state for skill audit events. It
// deliberately excludes Summary because that value comes from manifest content.
type SkillSnapshot struct {
	ID              string
	ProjectID       string
	Name            string
	DisplayName     string
	SourceKind      string
	Classification  string
	LatestVersionID string
	VersionCount    int64
	CreatedAt       string
	UpdatedAt       string
	ArchivedAt      *string
}

type LogSkillCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SkillURN         urn.Skill
	SkillName        string
	SkillDisplayName string
}

func (l *Logger) LogSkillCreate(ctx context.Context, dbtx repo.DBTX, event LogSkillCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSkillCreate),

		SubjectID:          event.SkillURN.ID.String(),
		SubjectType:        string(subjectTypeSkill),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SkillDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(event.SkillName),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SkillV1})
}

type LogSkillAddVersionEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SkillURN            urn.Skill
	SkillName           string
	SkillDisplayName    string
	SkillSnapshotBefore *SkillSnapshot
	SkillSnapshotAfter  *SkillSnapshot
}

func (l *Logger) LogSkillAddVersion(ctx context.Context, dbtx repo.DBTX, event LogSkillAddVersionEvent) error {
	action := ActionSkillAddVersion

	beforeSnapshot, err := marshalAuditPayload(event.SkillSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SkillSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SkillURN.ID.String(),
		SubjectType:        string(subjectTypeSkill),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SkillDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(event.SkillName),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SkillV1})
}

type LogSkillArchiveEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SkillURN            urn.Skill
	SkillName           string
	SkillDisplayName    string
	SkillSnapshotBefore *SkillSnapshot
	SkillSnapshotAfter  *SkillSnapshot
}

func (l *Logger) LogSkillArchive(ctx context.Context, dbtx repo.DBTX, event LogSkillArchiveEvent) error {
	action := ActionSkillArchive

	beforeSnapshot, err := marshalAuditPayload(event.SkillSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SkillSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SkillURN.ID.String(),
		SubjectType:        string(subjectTypeSkill),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SkillDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(event.SkillName),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SkillV1})
}

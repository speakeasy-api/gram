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
	ActionDirectoryRoleMappingSet    Action = "directory_role_mapping:set"
	ActionDirectoryRoleMappingDelete Action = "directory_role_mapping:delete"
)

// directoryRoleMappingMetadata describes one mapping row. A source's role set
// changes by adding and removing rows, each with its own entry, so an entry
// never replaces one role with another.
type directoryRoleMappingMetadata struct {
	RoleURN string `json:"role_urn"`
}

type LogDirectoryRoleMappingSetEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MappingURN  urn.DirectoryRoleMapping
	SourceLabel string
	RoleURN     string //nolint:glint // auditeventurntyping: TODO(AGE-1954): RBAC role identifiers have no URN type yet; pending team discussion
}

func (l *Logger) LogDirectoryRoleMappingSet(ctx context.Context, dbtx repo.DBTX, event LogDirectoryRoleMappingSetEvent) error {
	action := ActionDirectoryRoleMappingSet

	metadata, err := marshalAuditPayload(&directoryRoleMappingMetadata{
		RoleURN: event.RoleURN,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MappingURN.ID.String(),
		SubjectType:        string(subjectTypeDirectoryRoleMapping),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SourceLabel),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DirectoryRoleMappingV1})
}

type LogDirectoryRoleMappingDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MappingURN  urn.DirectoryRoleMapping
	SourceLabel string
	RoleURN     string //nolint:glint // auditeventurntyping: TODO(AGE-1954): RBAC role identifiers have no URN type yet; pending team discussion
}

func (l *Logger) LogDirectoryRoleMappingDelete(ctx context.Context, dbtx repo.DBTX, event LogDirectoryRoleMappingDeleteEvent) error {
	action := ActionDirectoryRoleMappingDelete

	metadata, err := marshalAuditPayload(&directoryRoleMappingMetadata{
		RoleURN: event.RoleURN,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MappingURN.ID.String(),
		SubjectType:        string(subjectTypeDirectoryRoleMapping),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SourceLabel),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DirectoryRoleMappingV1})
}

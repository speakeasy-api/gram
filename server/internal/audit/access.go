package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAccessRoleCreate       Action = "access_role:create"
	ActionAccessRoleUpdate       Action = "access_role:update"
	ActionAccessRoleDelete       Action = "access_role:delete"
	ActionAccessMemberRoleUpdate Action = "access_member:update_role"
)

type LogAccessRoleCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RoleID   string //nolint:glint // TODO(AGE-1954): discuss URN treatment for RBAC role identifiers; pending team discussion
	RoleName string
	RoleSlug string
}

func (l *Logger) LogAccessRoleCreate(ctx context.Context, dbtx repo.DBTX, event LogAccessRoleCreateEvent) error {
	action := ActionAccessRoleCreate

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RoleID,
		SubjectType:        string(subjectTypeAccessRole),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RoleName),
		SubjectSlug:        conv.ToPGTextEmpty(event.RoleSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogAccessRoleUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RoleID   string //nolint:glint // TODO(AGE-1954): discuss URN treatment for RBAC role identifiers; pending team discussion
	RoleName string
	RoleSlug string

	RoleSnapshotBefore *accessgen.Role
	RoleSnapshotAfter  *accessgen.Role
}

func (l *Logger) LogAccessRoleUpdate(ctx context.Context, dbtx repo.DBTX, event LogAccessRoleUpdateEvent) error {
	action := ActionAccessRoleUpdate

	beforeSnapshot, err := marshalAuditPayload(event.RoleSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.RoleSnapshotAfter)
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

		SubjectID:          event.RoleID,
		SubjectType:        string(subjectTypeAccessRole),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RoleName),
		SubjectSlug:        conv.ToPGTextEmpty(event.RoleSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogAccessRoleDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RoleID   string //nolint:glint // TODO(AGE-1954): discuss URN treatment for RBAC role identifiers; pending team discussion
	RoleName string
	RoleSlug string
}

func (l *Logger) LogAccessRoleDelete(ctx context.Context, dbtx repo.DBTX, event LogAccessRoleDeleteEvent) error {
	action := ActionAccessRoleDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RoleID,
		SubjectType:        string(subjectTypeAccessRole),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RoleName),
		SubjectSlug:        conv.ToPGTextEmpty(event.RoleSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogAccessMemberRoleUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MemberID    string //nolint:glint // TODO(AGE-1954): discuss URN treatment for RBAC member identifiers; pending team discussion
	MemberName  string
	MemberEmail string

	MemberSnapshotBefore *accessgen.AccessMember
	MemberSnapshotAfter  *accessgen.AccessMember
}

func (l *Logger) LogAccessMemberRoleUpdate(ctx context.Context, dbtx repo.DBTX, event LogAccessMemberRoleUpdateEvent) error {
	action := ActionAccessMemberRoleUpdate

	beforeSnapshot, err := marshalAuditPayload(event.MemberSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.MemberSnapshotAfter)
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

		SubjectID:          event.MemberID,
		SubjectType:        string(subjectTypeAccessMember),
		SubjectDisplayName: conv.ToPGTextEmpty(event.MemberName),
		SubjectSlug:        conv.ToPGTextEmpty(event.MemberEmail),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

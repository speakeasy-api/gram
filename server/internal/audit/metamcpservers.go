package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionMetaMcpServerCreate       Action = "meta-mcp:create"
	ActionMetaMcpServerUpdate       Action = "meta-mcp:update"
	ActionMetaMcpServerDelete       Action = "meta-mcp:delete"
	ActionMetaMcpServerAddMember    Action = "meta-mcp:add_member"
	ActionMetaMcpServerUpdateMember Action = "meta-mcp:update_member"
	ActionMetaMcpServerRemoveMember Action = "meta-mcp:remove_member"
)

// MetaMcpMemberMetadata is the metadata payload carried by membership events
// on the meta_mcp_server subject.
type MetaMcpMemberMetadata struct {
	MembershipID string `json:"membership_id"`
	McpServerID  string `json:"mcp_server_id"`
	SortOrder    int32  `json:"sort_order"`
}

type LogMetaMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MetaMcpServerURN urn.MetaMcpServer
	Name             string
}

func (l *Logger) LogMetaMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogMetaMcpServerCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionMetaMcpServerCreate),

		SubjectID:          event.MetaMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMetaMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.MetaMcpServerV1})
}

type LogMetaMcpServerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MetaMcpServerURN            urn.MetaMcpServer
	Name                        string
	MetaMcpServerSnapshotBefore *types.MetaMcpServer
	MetaMcpServerSnapshotAfter  *types.MetaMcpServer
}

func (l *Logger) LogMetaMcpServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogMetaMcpServerUpdateEvent) error {
	action := ActionMetaMcpServerUpdate

	beforeSnapshot, err := marshalAuditPayload(event.MetaMcpServerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.MetaMcpServerSnapshotAfter)
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

		SubjectID:          event.MetaMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMetaMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.MetaMcpServerV1})
}

type LogMetaMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MetaMcpServerURN urn.MetaMcpServer
	Name             string
}

func (l *Logger) LogMetaMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogMetaMcpServerDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionMetaMcpServerDelete),

		SubjectID:          event.MetaMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMetaMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.MetaMcpServerV1})
}

type LogMetaMcpMemberEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MetaMcpServerURN urn.MetaMcpServer
	Name             string

	MembershipURN urn.MetaMcpServerMember
	McpServerURN  urn.McpServer
	SortOrder     int32
}

func (l *Logger) logMetaMcpMember(ctx context.Context, dbtx repo.DBTX, action Action, event LogMetaMcpMemberEvent) error {
	metadata, err := marshalAuditPayload(MetaMcpMemberMetadata{
		MembershipID: event.MembershipURN.ID.String(),
		McpServerID:  event.McpServerURN.ID.String(),
		SortOrder:    event.SortOrder,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MetaMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMetaMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.MetaMcpServerV1})
}

func (l *Logger) LogMetaMcpMemberAdd(ctx context.Context, dbtx repo.DBTX, event LogMetaMcpMemberEvent) error {
	return l.logMetaMcpMember(ctx, dbtx, ActionMetaMcpServerAddMember, event)
}

func (l *Logger) LogMetaMcpMemberUpdate(ctx context.Context, dbtx repo.DBTX, event LogMetaMcpMemberEvent) error {
	return l.logMetaMcpMember(ctx, dbtx, ActionMetaMcpServerUpdateMember, event)
}

func (l *Logger) LogMetaMcpMemberRemove(ctx context.Context, dbtx repo.DBTX, event LogMetaMcpMemberEvent) error {
	return l.logMetaMcpMember(ctx, dbtx, ActionMetaMcpServerRemoveMember, event)
}

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
	ActionChatSessionAccess Action = "chat_session:access"
)

type LogChatSessionAccessEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ChatSessionURN urn.ChatSession
	ChatTitle      string
	// OwnerUserID is the user id of the session owner, recorded so reviewers can
	// see whose session was accessed even when the accessor differs. It is the
	// raw WorkOS user id (stored as the subject slug), not a URN subject.
	OwnerUserID string //nolint:glint // owner user id is auxiliary context, not the audit subject (which is ChatSessionURN)
}

// LogChatSessionAccess records that a chat session transcript was read. Unlike
// most audit events this describes a read, not a mutation, so callers pass the
// pool directly as dbtx — there is no surrounding transaction to be atomic with.
func (l *Logger) LogChatSessionAccess(ctx context.Context, dbtx repo.DBTX, event LogChatSessionAccessEvent) error {
	action := ActionChatSessionAccess

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ChatSessionURN.ID.String(),
		SubjectType:        string(subjectTypeChatSession),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ChatTitle),
		SubjectSlug:        conv.ToPGTextEmpty(event.OwnerUserID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChatSessionV1})
}

const ActionChatSessionMove Action = "chat_session:move"

type LogChatSessionMoveEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ChatSessionURN urn.ChatSession
	ChatTitle      string
	// OwnerUserID mirrors LogChatSessionAccessEvent: auxiliary context naming
	// whose session moved, not the audit subject.
	OwnerUserID string //nolint:glint // owner user id is auxiliary context, not the audit subject (which is ChatSessionURN)

	// TargetHarness is where the session was moved to (e.g. cursor, codex).
	TargetHarness string
	// SourceSurface is the harness the session originated in, when known.
	SourceSurface string
	// DeviceSerial and DeviceHostname attribute the machine the move happened
	// on. Optional; agents that cannot read them send nothing.
	DeviceSerial   string
	DeviceHostname string
}

// LogChatSessionMove records that a captured agent session was moved to
// another harness on a device (session portability). The move itself happens
// client-side — this entry is the governance record, deliberately free of
// session content. Like LogChatSessionAccess there is no surrounding mutation
// to be atomic with, so callers pass the pool directly as dbtx.
func (l *Logger) LogChatSessionMove(ctx context.Context, dbtx repo.DBTX, event LogChatSessionMoveEvent) error {
	action := ActionChatSessionMove

	meta := map[string]any{
		"target_harness": event.TargetHarness,
	}
	if event.SourceSurface != "" {
		meta["source_surface"] = event.SourceSurface
	}
	if event.DeviceSerial != "" {
		meta["device_serial"] = event.DeviceSerial
	}
	if event.DeviceHostname != "" {
		meta["device_hostname"] = event.DeviceHostname
	}
	metadata, err := marshalAuditPayload(meta)
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

		SubjectID:          event.ChatSessionURN.ID.String(),
		SubjectType:        string(subjectTypeChatSession),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ChatTitle),
		SubjectSlug:        conv.ToPGTextEmpty(event.OwnerUserID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChatSessionV1})
}

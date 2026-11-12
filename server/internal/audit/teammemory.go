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
	// ActionTeamMemoryRead records one read of a project's team memory through
	// a Platform MCP tool: a search, a recall or a zoom.
	ActionTeamMemoryRead Action = "team_memory:read"
	// ActionMemoryMemoCreate records a memo saved to a project's team memory.
	ActionMemoryMemoCreate Action = "memory_memo:create"
)

// LogTeamMemoryReadEvent records that a caller read a project's team memory.
// The subject is the project. The metadata names the tool and the sources the
// caller was shown, as kind:id references, so a reviewer can reconstruct what
// was disclosed without the audit log repeating any memory text.
type LogTeamMemoryReadEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ProjectSlug    string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// Tool is the Platform MCP tool that read memory, such as
	// search_team_memory.
	Tool string
	// Sources lists each returned item as kind:id, for example chat:<uuid>.
	Sources []string
}

// LogTeamMemoryRead records a memory read. Like chat session access it
// describes a read, so callers pass the pool as dbtx.
func (l *Logger) LogTeamMemoryRead(ctx context.Context, dbtx repo.DBTX, event LogTeamMemoryReadEvent) error {
	action := ActionTeamMemoryRead
	sources := event.Sources
	if sources == nil {
		sources = []string{}
	}
	metadata, err := marshalAuditPayload(map[string]any{
		"tool":    event.Tool,
		"sources": sources,
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

		SubjectID:          event.ProjectID.String(),
		SubjectType:        string(subjectTypeTeamMemory),
		SubjectDisplayName: conv.PtrToPGTextEmpty(nil),
		SubjectSlug:        conv.ToPGTextEmpty(event.ProjectSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TeamMemoryV1})
}

// LogMemoryMemoCreateEvent records a memo saved through remember. The memo
// text is not repeated in the audit log; the memo row holds it.
type LogMemoryMemoCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MemoryMemoURN urn.MemoryMemo
	// ChatID is the chat the memo was saved from, when the caller named one.
	ChatID uuid.NullUUID //nolint:glint // auditeventurnnaming: source chat is auxiliary context, not the audit subject (which is MemoryMemoURN)
}

// LogMemoryMemoCreate records a new memo. Callers pass the transaction that
// inserted the memo so the row and its audit entry commit together.
func (l *Logger) LogMemoryMemoCreate(ctx context.Context, dbtx repo.DBTX, event LogMemoryMemoCreateEvent) error {
	action := ActionMemoryMemoCreate
	var metadata []byte
	if event.ChatID.Valid {
		var err error
		metadata, err = marshalAuditPayload(map[string]string{"chat_id": event.ChatID.UUID.String()})
		if err != nil {
			return fmt.Errorf("marshal %s metadata: %w", action, err)
		}
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MemoryMemoURN.ID.String(),
		SubjectType:        string(subjectTypeMemoryMemo),
		SubjectDisplayName: conv.PtrToPGTextEmpty(nil),
		SubjectSlug:        conv.PtrToPGTextEmpty(nil),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TeamMemoryV1})
}

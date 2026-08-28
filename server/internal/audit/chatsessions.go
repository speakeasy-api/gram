package audit

import (
	"context"
	"fmt"
	"time"

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
	// TargetSessionID is the native session id minted for the continuation,
	// when the daemon knew it at launch time. Empty for targets whose ids are
	// minted server-side (Cursor).
	TargetSessionID string //nolint:glint // native harness session id, not a Gram resource with a URN
	// SourceSurface is the harness the session originated in, when known.
	SourceSurface string
	// DeviceSerial and DeviceHostname attribute the machine the move happened
	// on. Optional; agents that cannot read them send nothing.
	DeviceSerial   string
	DeviceHostname string
}

// LogChatSessionMove records that an agent session was moved to another
// harness on a device (session portability). Sessions that have not been
// captured yet are recorded too, so callers never gate reporting on capture.
// The move itself happens client-side — this entry is the governance record,
// deliberately free of session content. Callers record the move's lineage edge
// (chat_session_links) in the same transaction and pass that dbtx here so the
// edge and the governance record commit together.
func (l *Logger) LogChatSessionMove(ctx context.Context, dbtx repo.DBTX, event LogChatSessionMoveEvent) error {
	action := ActionChatSessionMove

	meta := map[string]any{
		"target_harness": event.TargetHarness,
	}
	if event.TargetSessionID != "" {
		meta["target_session_id"] = event.TargetSessionID
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

const ActionChatSessionRecall Action = "chat_session:recall"

type LogChatSessionRecallEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor urn.Principal

	ChatSessionURN urn.ChatSession
	ChatTitle      string
	// OwnerUserID mirrors LogChatSessionAccessEvent: auxiliary context naming
	// whose session was recalled, not the audit subject.
	OwnerUserID string //nolint:glint // owner user id is auxiliary context, not the audit subject (which is ChatSessionURN)

	// SourceSessionID is the native harness session id the recalled chat was
	// captured from, when known.
	SourceSessionID string //nolint:glint // native harness session id, not a Gram resource with a URN

	// The remaining fields describe the digest that was served — counts and
	// sizes only. Like the handoff-export event this entry records that content
	// left, not what it said.
	RedactToolPayloads bool
	FindingsMasked     int
	UnanalyzedMessages int
	DigestBytes        int
	TurnsIncluded      int
	TurnsDropped       int
}

// LogChatSessionRecall records that a captured agent session was recalled as a
// redacted handoff digest so work can continue in another harness (session
// portability). The digest itself never lands in the audit log — the entry is
// deliberately content-free. Callers record the recall's lineage edge
// (chat_session_links) in the same transaction and pass that dbtx here so the
// edge and the governance record commit together.
func (l *Logger) LogChatSessionRecall(ctx context.Context, dbtx repo.DBTX, event LogChatSessionRecallEvent) error {
	action := ActionChatSessionRecall

	meta := map[string]any{
		"redact_tool_payloads": event.RedactToolPayloads,
		"findings_masked":      event.FindingsMasked,
		"unanalyzed_messages":  event.UnanalyzedMessages,
		"digest_bytes":         event.DigestBytes,
		"turns_included":       event.TurnsIncluded,
		"turns_dropped":        event.TurnsDropped,
	}
	if event.SourceSessionID != "" {
		meta["source_session_id"] = event.SourceSessionID
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
		ActorDisplayName: conv.ToPGTextEmpty(""),
		ActorSlug:        conv.ToPGTextEmpty(""),

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

const ActionChatSessionHandoffExport Action = "chat_session:handoff_export"

type LogChatSessionHandoffExportEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ChatSessionURN urn.ChatSession
	ChatTitle      string

	// SourceSurface is the harness the session originated in, when known.
	SourceSurface string
	// ContentBytes is the size of the uploaded handoff document. The document
	// itself never lands in the audit log — this entry records that content
	// left the device, not what it said.
	ContentBytes int
	// TTLSeconds and ExpiresAt bound the minted link's exposure window.
	TTLSeconds int
	ExpiresAt  time.Time
	// DeviceSerial and DeviceHostname attribute the minting machine. Optional.
	DeviceSerial   string
	DeviceHostname string
}

// LogChatSessionHandoffExport records that a rendered session handoff was
// uploaded and a short-lived capability URL minted for it (session
// portability). This is the governance moment where session content first
// transits the server, so the entry is deliberately content-free: size, TTL,
// and attribution only — never the document or its token. Like the move
// event there is no surrounding mutation beyond the link insert itself, which
// callers pass as dbtx for atomicity.
func (l *Logger) LogChatSessionHandoffExport(ctx context.Context, dbtx repo.DBTX, event LogChatSessionHandoffExportEvent) error {
	action := ActionChatSessionHandoffExport

	meta := map[string]any{
		"content_bytes": event.ContentBytes,
		"ttl_seconds":   event.TTLSeconds,
		"expires_at":    event.ExpiresAt.UTC().Format(time.RFC3339),
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
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChatSessionV1})
}

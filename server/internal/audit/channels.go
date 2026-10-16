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
	// ActionChannelsSettingsUpdate records an organization turning Channels on
	// or off. The subject is the organization's Channels settings, because the
	// change applies to everyone in it.
	ActionChannelsSettingsUpdate Action = "channels:update_settings"
	// ActionChannelsAccessUpdate records an administrator changing one
	// member's Channels participation, or removing their exception so the
	// member falls back to their role's permissions. The subject is the member
	// whose access changed, not the administrator who changed it.
	ActionChannelsAccessUpdate Action = "channels:update_access"
)

// ChannelsSettingsSnapshot is the organization's Channels availability.
//
// Channels settings carry no secret and no conversation content, so the
// snapshot is the whole of what changed rather than a redaction of it.
type ChannelsSettingsSnapshot struct {
	Enabled bool `json:"enabled"`
}

// ChannelsAccessSnapshot is one member's Channels participation exception.
//
// It records the two permissions an exception can carry and never the
// member's conversations, instance labels, or introduction notes. Channels
// administration is deliberately absent: it is granted through access
// management as channels:admin and is never settable as an exception, so a
// field for it here would imply a change this subsystem cannot make.
type ChannelsAccessSnapshot struct {
	CanConnect bool `json:"can_connect"`
	CanReadAll bool `json:"can_read_all"`
}

type LogChannelsSettingsUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SnapshotBefore *ChannelsSettingsSnapshot
	SnapshotAfter  *ChannelsSettingsSnapshot
}

// LogChannelsSettingsUpdate records a change to Channels availability.
//
// It must be called inside the transaction that made the change. Channels
// availability decides whether an organization's members and agents can read
// or post at all, so a committed change with no audit record would leave the
// organization unable to say who switched it.
func (l *Logger) LogChannelsSettingsUpdate(ctx context.Context, dbtx repo.DBTX, event LogChannelsSettingsUpdateEvent) error {
	action := ActionChannelsSettingsUpdate

	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
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

		SubjectID:          event.OrganizationID,
		SubjectType:        string(subjectTypeChannelsSettings),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChannelsV1})
}

type LogChannelsAccessUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// MemberID is the account whose Channels participation changed.
	//
	//nolint:glint // auditeventurnnaming: TODO(AGE-1954): discuss URN treatment for RBAC member identifiers; pending team discussion. Channels exceptions are keyed on the same member identifier the access-management entries use.
	MemberID string

	// Removed reports that the member's exception was withdrawn, so their
	// Channels permissions fall back to the ones their roles grant. The after
	// snapshot is nil in that case: there is no exception left to describe,
	// and recording the role-derived permissions here would imply this change
	// set them.
	Removed bool

	SnapshotBefore *ChannelsAccessSnapshot
	SnapshotAfter  *ChannelsAccessSnapshot
}

// LogChannelsAccessUpdate records a change to one member's Channels
// participation.
//
// It must be called inside the transaction that made the change. The subject
// is the member, so an organization can answer "who changed this person's
// access" from the subject alone, the same way the access-management entries
// for channels:connect, channels:admin and channels:read_all do.
func (l *Logger) LogChannelsAccessUpdate(ctx context.Context, dbtx repo.DBTX, event LogChannelsAccessUpdateEvent) error {
	action := ActionChannelsAccessUpdate

	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(map[string]bool{"exception_removed": event.Removed})
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

		SubjectID:          event.MemberID,
		SubjectType:        string(subjectTypeChannelsAccess),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChannelsV1})
}

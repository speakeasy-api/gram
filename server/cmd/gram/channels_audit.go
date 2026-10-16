package gram

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/channels"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newChannelsAdminAudit records administrative Channels changes through the
// repository's audit logger.
//
// The hook runs inside the handler's own transaction and before commit, so the
// entries land with the change or neither does. A nil logger refuses rather
// than silently dropping the record: an administrative change we cannot
// account for is worse than a refused one.
func newChannelsAdminAudit(logger *audit.Logger) channels.AdminAudit {
	return func(ctx context.Context, tx pgx.Tx, event channels.AdminAuditEvent) error {
		if logger == nil {
			return errors.New("channels administration requires audit logging")
		}
		settings, access, err := channelsAuditEntries(event)
		if err != nil {
			return err
		}
		if settings != nil {
			if err := logger.LogChannelsSettingsUpdate(ctx, tx, *settings); err != nil {
				return fmt.Errorf("record channels settings change: %w", err)
			}
		}
		if access != nil {
			if err := logger.LogChannelsAccessUpdate(ctx, tx, *access); err != nil {
				return fmt.Errorf("record channels access change: %w", err)
			}
		}
		return nil
	}
}

// channelsAuditEntries translates one administrative change into the audit
// entries it must produce.
//
// One operation can change organization availability and one member's
// participation together, so this can produce both. They are separate entries
// rather than one row because "who turned Channels off" and "who changed this
// person's access" are questions over different subjects, and the audit feed
// facets on subject type.
func channelsAuditEntries(event channels.AdminAuditEvent) (*audit.LogChannelsSettingsUpdateEvent, *audit.LogChannelsAccessUpdateEvent, error) {
	if event.OrganizationID == "" || event.ActorSubjectID == "" {
		return nil, nil, errors.New("channels administrative audit requires an organization and an actor")
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, event.ActorSubjectID)

	var settings *audit.LogChannelsSettingsUpdateEvent
	if event.EnabledBefore != nil || event.EnabledAfter != nil {
		settings = &audit.LogChannelsSettingsUpdateEvent{
			OrganizationID:   event.OrganizationID,
			Actor:            actor,
			ActorDisplayName: nil,
			ActorSlug:        nil,
			SnapshotBefore:   channelsSettingsSnapshot(event.EnabledBefore),
			SnapshotAfter:    channelsSettingsSnapshot(event.EnabledAfter),
		}
	}
	if event.TargetSubjectID == "" {
		return settings, nil, nil
	}
	// A withdrawn exception has no state left to describe. Recording the
	// permissions the member's roles happen to grant would read as though this
	// change had set them.
	var after *audit.ChannelsAccessSnapshot
	if !event.Removed {
		after = &audit.ChannelsAccessSnapshot{
			CanConnect: event.CanConnect != nil && event.CanConnect.After,
			CanReadAll: event.CanReadAll != nil && event.CanReadAll.After,
		}
	}
	return settings, &audit.LogChannelsAccessUpdateEvent{
		OrganizationID:   event.OrganizationID,
		Actor:            actor,
		ActorDisplayName: nil,
		ActorSlug:        nil,
		MemberID:         event.TargetSubjectID,
		Removed:          event.Removed,
		SnapshotBefore: &audit.ChannelsAccessSnapshot{
			CanConnect: event.CanConnect != nil && event.CanConnect.Before,
			CanReadAll: event.CanReadAll != nil && event.CanReadAll.Before,
		},
		SnapshotAfter: after,
	}, nil
}

func channelsSettingsSnapshot(enabled *bool) *audit.ChannelsSettingsSnapshot {
	if enabled == nil {
		return nil
	}
	return &audit.ChannelsSettingsSnapshot{Enabled: *enabled}
}

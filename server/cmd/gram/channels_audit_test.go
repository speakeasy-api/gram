package gram

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/channels"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func channelsAuditChange(before, after bool) *channels.AdminAuditChange {
	return &channels.AdminAuditChange{Before: before, After: after}
}

func TestChannelsAuditEntriesRecordEachSubjectThatChanged(t *testing.T) {
	t.Parallel()
	enabled, disabled := true, false

	t.Run("enablement alone writes only the settings entry", func(t *testing.T) {
		t.Parallel()
		settings, access, err := channelsAuditEntries(channels.AdminAuditEvent{
			OrganizationID: "org_example", ActorSubjectID: "user_actor",
			EnabledBefore: &disabled, EnabledAfter: &enabled,
		})
		require.NoError(t, err)
		require.Nil(t, access)
		require.NotNil(t, settings)
		require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, "user_actor"), settings.Actor)
		require.Equal(t, &audit.ChannelsSettingsSnapshot{Enabled: false}, settings.SnapshotBefore)
		require.Equal(t, &audit.ChannelsSettingsSnapshot{Enabled: true}, settings.SnapshotAfter)
	})

	t.Run("an exception alone writes only the access entry, subject the member", func(t *testing.T) {
		t.Parallel()
		settings, access, err := channelsAuditEntries(channels.AdminAuditEvent{
			OrganizationID: "org_example", ActorSubjectID: "user_actor", TargetSubjectID: "user_target",
			CanConnect: channelsAuditChange(false, true), CanReadAll: channelsAuditChange(false, false),
		})
		require.NoError(t, err)
		require.Nil(t, settings)
		require.NotNil(t, access)
		require.Equal(t, "user_target", access.MemberID)
		require.False(t, access.Removed)
		require.Equal(t, &audit.ChannelsAccessSnapshot{CanConnect: false, CanReadAll: false}, access.SnapshotBefore)
		require.Equal(t, &audit.ChannelsAccessSnapshot{CanConnect: true, CanReadAll: false}, access.SnapshotAfter)
	})

	t.Run("one operation changing both writes both entries", func(t *testing.T) {
		t.Parallel()
		settings, access, err := channelsAuditEntries(channels.AdminAuditEvent{
			OrganizationID: "org_example", ActorSubjectID: "user_actor", TargetSubjectID: "user_target",
			EnabledBefore: &enabled, EnabledAfter: &disabled,
			CanReadAll: channelsAuditChange(true, false),
		})
		require.NoError(t, err)
		require.NotNil(t, settings)
		require.NotNil(t, access)
		require.Equal(t, "org_example", settings.OrganizationID)
		require.Equal(t, "org_example", access.OrganizationID)
	})

	// A withdrawn exception leaves the member on their roles' permissions.
	// Recording an after snapshot would read as though this change set them.
	t.Run("a withdrawn exception records no after state", func(t *testing.T) {
		t.Parallel()
		_, access, err := channelsAuditEntries(channels.AdminAuditEvent{
			OrganizationID: "org_example", ActorSubjectID: "user_actor", TargetSubjectID: "user_target",
			CanConnect: channelsAuditChange(true, false), Removed: true,
		})
		require.NoError(t, err)
		require.NotNil(t, access)
		require.True(t, access.Removed)
		require.Equal(t, &audit.ChannelsAccessSnapshot{CanConnect: true, CanReadAll: false}, access.SnapshotBefore)
		require.Nil(t, access.SnapshotAfter)
	})

	t.Run("an unattributable change is refused", func(t *testing.T) {
		t.Parallel()
		for _, event := range []channels.AdminAuditEvent{
			{OrganizationID: "", ActorSubjectID: "user_actor"},
			{OrganizationID: "org_example", ActorSubjectID: ""},
		} {
			_, _, err := channelsAuditEntries(event)
			require.Error(t, err)
		}
	})
}

// Administration cannot run unaccounted for.
func TestChannelsAdminAuditRefusesWithoutALogger(t *testing.T) {
	t.Parallel()
	err := newChannelsAdminAudit(nil)(t.Context(), nil, channels.AdminAuditEvent{OrganizationID: "org_example", ActorSubjectID: "user_actor"})
	require.ErrorContains(t, err, "requires audit logging")
}

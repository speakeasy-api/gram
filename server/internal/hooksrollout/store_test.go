package hooksrollout_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
)

const operator = "operator@example.com"

func TestStore_NoPinsIsUnset(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	store := hooksrollout.NewStore(conn)
	orgID := createOrganization(t, conn)

	pins, err := store.OrganizationPins(t.Context(), orgID)
	require.NoError(t, err)
	require.Nil(t, pins.Override)
	require.Nil(t, pins.Default)

	pin, source := pins.Effective()
	require.Nil(t, pin)
	require.Equal(t, hooksrollout.SourceUnset, source)
}

func TestStore_DefaultPinAppliesToEveryOrganization(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	store := hooksrollout.NewStore(conn)
	orgID := createOrganization(t, conn)

	require.NoError(t, store.SetDefault(t.Context(), 40, operator))
	require.NoError(t, store.SetDefault(t.Context(), 42, "second@example.com"))

	pins, err := store.OrganizationPins(t.Context(), orgID)
	require.NoError(t, err)
	pin, source := pins.Effective()
	require.Equal(t, hooksrollout.SourceDefault, source)
	require.NotNil(t, pin)
	require.Equal(t, 42, pin.Version, "the newest default pin wins")
	require.Equal(t, "second@example.com", pin.SetBy)
	require.False(t, pin.SetAt.IsZero())
}

func TestStore_OverrideWinsUntilCleared(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	store := hooksrollout.NewStore(conn)
	orgID := createOrganization(t, conn)
	otherOrgID := createOrganization(t, conn)

	require.NoError(t, store.SetDefault(t.Context(), 42, operator))
	require.NoError(t, store.SetOrganizationOverride(t.Context(), orgID, 40, operator))

	pins, err := store.OrganizationPins(t.Context(), orgID)
	require.NoError(t, err)
	pin, source := pins.Effective()
	require.Equal(t, hooksrollout.SourceOrganization, source)
	require.Equal(t, 40, pin.Version)

	otherPins, err := store.OrganizationPins(t.Context(), otherOrgID)
	require.NoError(t, err)
	otherPin, otherSource := otherPins.Effective()
	require.Equal(t, hooksrollout.SourceDefault, otherSource, "an override applies to its own organization only")
	require.Equal(t, 42, otherPin.Version)

	require.NoError(t, store.ClearOrganizationOverride(t.Context(), orgID, operator))
	pins, err = store.OrganizationPins(t.Context(), orgID)
	require.NoError(t, err)
	require.Nil(t, pins.Override)
	pin, source = pins.Effective()
	require.Equal(t, hooksrollout.SourceDefault, source)
	require.Equal(t, 42, pin.Version)

	require.NoError(t, store.SetOrganizationOverride(t.Context(), orgID, 41, operator))
	pins, err = store.OrganizationPins(t.Context(), orgID)
	require.NoError(t, err)
	require.Equal(t, 41, pins.Override.Version, "an override can be set again after it was cleared")
}

func TestStore_ListOverridesSkipsClearedOverrides(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	store := hooksrollout.NewStore(conn)
	keptOrgID := createOrganization(t, conn)
	clearedOrgID := createOrganization(t, conn)

	require.NoError(t, store.SetOrganizationOverride(t.Context(), keptOrgID, 40, operator))
	require.NoError(t, store.SetOrganizationOverride(t.Context(), keptOrgID, 41, operator))
	require.NoError(t, store.SetOrganizationOverride(t.Context(), clearedOrgID, 40, operator))
	require.NoError(t, store.ClearOrganizationOverride(t.Context(), clearedOrgID, operator))

	overrides, err := store.ListOverrides(t.Context())
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	require.Equal(t, keptOrgID, overrides[0].OrganizationID)
	require.Equal(t, keptOrgID, overrides[0].OrganizationSlug)
	require.Equal(t, "Hooks Rollout Target", overrides[0].OrganizationName)
	require.Equal(t, 41, overrides[0].Pin.Version)
}

func TestStore_RecentChangesNewestFirstAndSkipsNoOps(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	store := hooksrollout.NewStore(conn)
	orgID := createOrganization(t, conn)

	require.NoError(t, store.SetDefault(t.Context(), 42, operator))
	require.NoError(t, store.SetDefault(t.Context(), 42, operator), "repeating the current default is a no-op")
	require.NoError(t, store.ClearOrganizationOverride(t.Context(), orgID, operator), "clearing a missing override is a no-op")
	require.NoError(t, store.SetOrganizationOverride(t.Context(), orgID, 40, operator))
	require.NoError(t, store.SetOrganizationOverride(t.Context(), orgID, 40, operator), "repeating the current override is a no-op")
	require.NoError(t, store.ClearOrganizationOverride(t.Context(), orgID, operator))

	changes, err := store.ListRecentChanges(t.Context(), hooksrollout.RecentChangesLimit)
	require.NoError(t, err)
	require.Len(t, changes, 3)

	require.Equal(t, orgID, changes[0].OrganizationID)
	require.Equal(t, orgID, changes[0].OrganizationSlug)
	require.Nil(t, changes[0].Version, "the newest change cleared the override")

	require.Equal(t, orgID, changes[1].OrganizationID)
	require.Equal(t, 40, *changes[1].Version)

	require.Empty(t, changes[2].OrganizationID, "the oldest change set the default")
	require.Equal(t, 42, *changes[2].Version)

	limited, err := store.ListRecentChanges(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
}

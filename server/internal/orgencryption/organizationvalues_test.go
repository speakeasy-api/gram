package orgencryption_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// valuesStore reports fixed organizations with values and fixed counts. Its
// other CopyStore methods are not called by the helpers under test.
type valuesStore struct {
	orgencryption.CopyStore

	withValues []orgencryption.OrganizationValues
	counts     orgencryption.CopyCounts
	err        error
}

func (s valuesStore) Columns() orgencryption.CopyColumns { return fakeColumns }

func (s valuesStore) ListOrganizationsWithValues(context.Context) ([]orgencryption.OrganizationValues, error) {
	return s.withValues, s.err
}

func (s valuesStore) Count(context.Context, string) (orgencryption.CopyCounts, error) {
	return s.counts, s.err
}

func TestListOrganizationsWithValuesMergesStoresMostRecentlyWrittenFirst(t *testing.T) {
	t.Parallel()

	earlier := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)
	latest := later.Add(time.Hour)
	stores := []orgencryption.CopyStore{
		valuesStore{withValues: []orgencryption.OrganizationValues{
			{OrganizationID: "org_a", Values: 2, LastWrittenAt: later},
			{OrganizationID: "org_b", Values: 1, LastWrittenAt: earlier},
		}},
		valuesStore{withValues: []orgencryption.OrganizationValues{
			{OrganizationID: "org_a", Values: 3, LastWrittenAt: earlier},
			{OrganizationID: "org_c", Values: 4, LastWrittenAt: latest},
		}},
	}

	organizations, err := orgencryption.ListOrganizationsWithValues(t.Context(), stores)
	require.NoError(t, err)
	require.Equal(t, []orgencryption.OrganizationValues{
		{OrganizationID: "org_c", Values: 4, LastWrittenAt: latest},
		{OrganizationID: "org_a", Values: 5, LastWrittenAt: later},
		{OrganizationID: "org_b", Values: 1, LastWrittenAt: earlier},
	}, organizations)
}

func TestListOrganizationsWithValuesReportsAFailingStore(t *testing.T) {
	t.Parallel()

	stores := []orgencryption.CopyStore{valuesStore{err: errors.New("list failed")}}

	_, err := orgencryption.ListOrganizationsWithValues(t.Context(), stores)
	require.ErrorContains(t, err, "list failed")
}

func TestCountStoresSumsValuesAndKeepsTheLatestWrite(t *testing.T) {
	t.Parallel()

	earlier := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)
	stores := []orgencryption.CopyStore{
		valuesStore{counts: orgencryption.CopyCounts{Waiting: 1, PlatformCopies: 1, Values: 2, LastWrittenAt: later}},
		valuesStore{counts: orgencryption.CopyCounts{}},
		valuesStore{counts: orgencryption.CopyCounts{Unattributed: 1, Values: 1, LastWrittenAt: earlier}},
	}

	counts, err := orgencryption.CountStores(t.Context(), "org_a", stores)
	require.NoError(t, err)
	require.Equal(t, orgencryption.CopyCounts{Waiting: 1, Unreadable: 0, Unattributed: 1, PlatformCopies: 1, Values: 3, LastWrittenAt: later}, counts)
}

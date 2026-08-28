//nolint:paralleltest // Integration fixtures share an isolated cloned database per test.
package killswitches

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

func TestCustomerListWatermarkKeepsPagesStableAcrossConcurrentCommit(t *testing.T) {
	conn, orgID := newLifecycleDatabase(t, "killswitch_customer_list_watermark")
	seedLifecycle := newLifecycleServiceForTest(t, conn, func(OrganizationID, PrincipalKey) bool { return true }, func(OrganizationID, ResourceKey) bool { return true }, nil)
	seedFacade, err := NewFacade(seedLifecycle)
	require.NoError(t, err)

	for _, principal := range []string{"user:alpha", "user:bravo", "user:charlie"} {
		request := testActivateRequest(orgID, uuid.New())
		request.PrincipalInput = principal
		_, err := seedFacade.ActivatePrescription(t.Context(), ActivatePrescriptionInput{
			MutationContext: request.MutationContext, Definition: request.Definition, PrincipalKind: request.PrincipalKind,
			PrincipalInput: request.PrincipalInput, ResourceKind: request.ResourceKind, Desired: request.Desired,
		})
		require.NoError(t, err)
	}

	baselineSnapshot := captureCustomerListSnapshot(t, conn, orgID)
	baseline, err := seedFacade.ListCustomerPrescriptions(t.Context(), customerListRequest(orgID, baselineSnapshot, 100, nil))
	require.NoError(t, err)
	require.Len(t, baseline.Items, 3)
	target := baseline.Items[len(baseline.Items)-1]

	transactionStarted := make(chan struct{})
	releaseTransaction := make(chan struct{})
	registry, err := BuildRegistry(validRegistration())
	require.NoError(t, err)
	concurrentLifecycle, err := NewLifecycleService(conn, registry, lifecycleValidatorFunc(func(ctx context.Context, _ LifecycleTransactionQueries, _ CurrentReferenceBatch) error {
		close(transactionStarted)
		select {
		case <-releaseTransaction:
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}), nil)
	require.NoError(t, err)
	concurrentFacade, err := NewFacade(concurrentLifecycle)
	require.NoError(t, err)

	change := testChangeRequest(orgID, target.ID, target.Version, uuid.New(), []string{"tool:changed"})
	scheduledStart := time.Now().Add(time.Hour).UTC()
	change.Desired.StartMode = StartModeAt
	change.Desired.StartsAt = &scheduledStart

	mutationDone := make(chan error, 1)
	go func() {
		_, changeErr := concurrentFacade.ChangePrescription(t.Context(), change)
		mutationDone <- changeErr
	}()
	select {
	case <-transactionStarted:
	case changeErr := <-mutationDone:
		require.NoError(t, changeErr)
		t.Fatal("concurrent transaction completed before reaching the validation barrier")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent transaction to start")
	}

	pageSnapshot := captureCustomerListSnapshot(t, conn, orgID)
	page1, err := seedFacade.ListCustomerPrescriptions(t.Context(), customerListRequest(orgID, pageSnapshot, 1, nil))
	require.NoError(t, err)
	require.Len(t, page1.Items, 1)
	require.NotNil(t, page1.NextCursor)
	require.NotEqual(t, target.ID, page1.Items[0].ID)

	close(releaseTransaction)
	select {
	case changeErr := <-mutationDone:
		require.NoError(t, changeErr, "the transaction begun before page one must commit before page two")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent transaction to commit")
	}

	items := slices.Clone(page1.Items)
	cursor := page1.NextCursor
	for cursor != nil {
		page, listErr := seedFacade.ListCustomerPrescriptions(t.Context(), customerListRequest(orgID, pageSnapshot, 1, cursor))
		require.NoError(t, listErr)
		items = append(items, page.Items...)
		cursor = page.NextCursor
	}
	require.Len(t, items, len(baseline.Items))

	baselineIDs := make([]PrescriptionID, len(baseline.Items))
	pageIDs := make([]PrescriptionID, len(items))
	for i := range baseline.Items {
		baselineIDs[i] = baseline.Items[i].ID
		pageIDs[i] = items[i].ID
	}
	require.ElementsMatch(t, baselineIDs, pageIDs)
	for _, item := range items {
		if item.ID == target.ID {
			require.Equal(t, int64(1), item.Version, "later pages must read the version visible at the page-one watermark")
			require.Equal(t, CustomerStatusActive, item.Status)
			require.Equal(t, CustomerStartModeNow, item.StartMode)
			require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a")}, item.SelectedResourceKeys)
		}
	}

	freshSnapshot := captureCustomerListSnapshot(t, conn, orgID)
	fresh, err := seedFacade.ListCustomerPrescriptions(t.Context(), customerListRequest(orgID, freshSnapshot, 100, nil))
	require.NoError(t, err)
	for _, item := range fresh.Items {
		if item.ID == target.ID {
			require.Equal(t, int64(2), item.Version, "a new first page must observe the committed edit")
			require.Equal(t, CustomerStatusScheduled, item.Status)
			require.Equal(t, CustomerStartModeScheduled, item.StartMode)
			require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:changed")}, item.SelectedResourceKeys)
			return
		}
	}
	t.Fatal("edited prescription missing from fresh list")
}

type customerListSnapshot struct {
	watermark  int64
	statusAsOf time.Time
}

func captureCustomerListSnapshot(t *testing.T, conn *pgxpool.Pool, orgID string) customerListSnapshot {
	t.Helper()
	row, err := repo.New(conn).CaptureKillswitchCustomerListWatermark(t.Context(), repo.CaptureKillswitchCustomerListWatermarkParams{
		OrganizationID: orgID, DefinitionKey: "block-tools", PrincipalKind: "user", ResourceKind: "tool",
	})
	require.NoError(t, err)
	require.True(t, row.StatusAsOf.Valid)
	return customerListSnapshot{watermark: row.Watermark, statusAsOf: row.StatusAsOf.Time}
}

func customerListRequest(orgID string, snapshot customerListSnapshot, limit int32, cursor *CustomerListCursor) ListCustomerPrescriptionsRequest {
	return ListCustomerPrescriptionsRequest{
		OrganizationID: OrganizationID(orgID), Definition: "block-tools", PrincipalKind: "user", ResourceKind: "tool",
		Limit: limit, Cursor: cursor, StatusAsOf: snapshot.statusAsOf, SnapshotWatermark: snapshot.watermark,
	}
}

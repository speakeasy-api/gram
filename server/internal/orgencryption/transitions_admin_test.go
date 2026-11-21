package orgencryption_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// readyToFinalize leaves the use case in dual_write with one unreadable value,
// a clean verification, and the minimum time in dual_write elapsed.
func (ti *transitionInstance) readyToFinalize(t *testing.T) {
	t.Helper()

	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put(ti.orgID, "broken", orgencryption.Copies{Platform: conv.ToPGText("not a ciphertext"), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.True(t, result.VerificationPassed)
	ti.backdate(t, time.Hour)
}

func TestTransitions_FinalizeRefusesWhenTheUnreadableCountDiffersFromTheAcknowledgedCount(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.readyToFinalize(t)

	_, err := ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: []orgencryption.CopyStore{ti.store}, ExpectedUnreadable: new(int64(0))}, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrUnreadableCountChanged)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStateDualWrite, transition.State, "a refused finalize changes nothing")

	finalized, err := ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: []orgencryption.CopyStore{ti.store}, ExpectedUnreadable: new(int64(1))}, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStateOrgKMS, finalized.State)
}

// racingClearStore lets another clear finish completely after this clear's
// first batch, as when the hourly resume and an on-demand clear overlap.
type racingClearStore struct {
	*fakeCopyStore

	once sync.Once
	race func(ctx context.Context)
}

func (r *racingClearStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (int64, error) {
	n, err := r.fakeCopyStore.ClearPlatformCopies(ctx, organizationID, generation, limit)
	r.once.Do(func() { r.race(ctx) })

	return n, err
}

func TestTransitions_ClearReportsSuccessWhenAnotherClearFinishedFirst(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	ti.finalize(t)

	var raceErr error
	racing := &racingClearStore{fakeCopyStore: ti.store, race: func(ctx context.Context) {
		_, raceErr = ti.transitions.ClearPlatformCopies(ctx, ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	}}

	cleared, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{racing}, orgencryption.SystemActor())
	require.NoError(t, err)
	require.NoError(t, raceErr)
	require.NotNil(t, cleared.PlatformCopiesClearedAt)

	count, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionTransitionClearPlatformCopies)
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "only the clear that finished first records the event")
}

func TestMigrator_RunOnDemandFinishesTheClearAfterFinalize(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	ti.finalize(t)
	migrator := ti.migrator(t)

	_, err := migrator.RunOnDemand(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.NotNil(t, transition.PlatformCopiesClearedAt)
	require.False(t, ti.store.get("a").Platform.Valid)
	require.True(t, ti.store.get("a").Org.Valid)

	_, err = migrator.RunOnDemand(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed, "nothing is left to do once clearing finished")
}

func TestMigrator_RunOnDemandBackfillsInDualWrite(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	result, err := ti.migrator(t).RunOnDemand(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Converted)
	require.True(t, result.VerificationPassed)
}

func TestMigrator_RunOnDemandRefusesPlatformKey(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	_, err := ti.migrator(t).RunOnDemand(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
}

func TestFinalizeBlockers_ReportsEveryReasonFinalizeWouldRefuse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	enrolled := orgencryption.Transition{State: orgencryption.TransitionStateDualWrite, StateChangedAt: now.Add(-time.Hour)}

	blockers := orgencryption.FinalizeBlockers(enrolled, orgencryption.CopyCounts{Waiting: 2, Unreadable: 5, Unattributed: 1, PlatformCopies: 8}, now, 24*time.Hour)
	require.Len(t, blockers, 3, "the soak time, waiting values and unattributed values each block; unreadable values do not")

	verified := now.Add(-30 * time.Minute)
	ready := orgencryption.Transition{State: orgencryption.TransitionStateDualWrite, StateChangedAt: now.Add(-48 * time.Hour), LastVerifiedAt: &verified}
	require.Empty(t, orgencryption.FinalizeBlockers(ready, orgencryption.CopyCounts{Waiting: 0, Unreadable: 5, Unattributed: 0, PlatformCopies: 5}, now, 24*time.Hour))

	running := ready
	running.LastBackfill = orgencryption.BackfillRecord{StartedAt: new(now.Add(-time.Minute))}
	require.Len(t, orgencryption.FinalizeBlockers(running, orgencryption.CopyCounts{}, now, 24*time.Hour), 1, "a backfill still running blocks")

	finalized := orgencryption.Transition{State: orgencryption.TransitionStateOrgKMS}
	require.Len(t, orgencryption.FinalizeBlockers(finalized, orgencryption.CopyCounts{}, now, 24*time.Hour), 1, "only dual_write can be finalized")
}

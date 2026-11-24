package orgencryption_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func (ti *transitionInstance) migrator(t *testing.T) *orgencryption.Migrator {
	t.Helper()

	migrator, err := orgencryption.NewMigrator(testenv.NewLogger(t), ti.conn, ti.runner, ti.transitions, []orgencryption.UseCaseStores{{UseCase: useCase, Stores: []orgencryption.CopyStore{ti.store}}}, testenv.NewMeterProvider(t))
	require.NoError(t, err)

	return migrator
}

// setRunTimes places an organization's last backfill and verification at the
// given times; a nil time clears the column.
func (ti *transitionInstance) setRunTimes(t *testing.T, organizationID string, started, completed, verified *time.Time) {
	t.Helper()

	err := repo.New(ti.conn).SetEncryptionTransitionRunTimesFixture(t.Context(), repo.SetEncryptionTransitionRunTimesFixtureParams{
		LastBackfillStartedAt:   timestamptz(started),
		LastBackfillCompletedAt: timestamptz(completed),
		LastVerifiedAt:          timestamptz(verified),
		OrganizationID:          organizationID,
		UseCase:                 string(useCase),
	})
	require.NoError(t, err)
}

func timestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}

	return pgtype.Timestamptz{Time: *v, InfinityModifier: pgtype.Finite, Valid: true}
}

// finalize runs a clean backfill and verification and moves the use case to
// org_kms without clearing.
func (ti *transitionInstance) finalize(t *testing.T) {
	t.Helper()

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.True(t, result.VerificationPassed)
	ti.backdate(t, time.Hour)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: []orgencryption.CopyStore{ti.store}}, orgencryption.SystemActor())
	require.NoError(t, err)
}

func dueOrganizations(t *testing.T, migrator *orgencryption.Migrator) []string {
	t.Helper()

	due, err := migrator.ListDueBackfills(t.Context())
	require.NoError(t, err)

	ids := make([]string, 0, len(due))
	for _, transition := range due {
		ids = append(ids, transition.OrganizationID)
	}

	return ids
}

func TestRunner_RefusesWhileAnotherRunHoldsTheClaim(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	ti.setRunTimes(t, ti.orgID, new(time.Now()), nil, nil)

	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.ErrorIs(t, err, orgencryption.ErrBackfillRunning)
	require.False(t, ti.store.get("a").Org.Valid, "a refused run seals nothing")

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Nil(t, transition.LastBackfill.CompletedAt, "a refused run leaves the other run's bookkeeping alone")
}

func TestRunner_TakesOverAClaimPastItsLease(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	ti.setRunTimes(t, ti.orgID, new(time.Now().Add(-time.Hour)), nil, nil)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Converted)
	require.True(t, result.VerificationPassed)
}

func TestRunner_StartsAgainOnceTheLastRunCompleted(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)

	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)

	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Converted)
}

func TestNewMigrator_RejectsInvalidUseCaseStores(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	_, err := orgencryption.NewMigrator(testenv.NewLogger(t), ti.conn, ti.runner, ti.transitions, []orgencryption.UseCaseStores{{UseCase: orgencryption.UseCaseOAuthClientSecrets, Stores: []orgencryption.CopyStore{ti.store}}}, testenv.NewMeterProvider(t))
	require.Error(t, err, "the store belongs to another use case")

	_, err = orgencryption.NewMigrator(testenv.NewLogger(t), ti.conn, ti.runner, ti.transitions, nil, testenv.NewMeterProvider(t))
	require.Error(t, err, "at least one use case is required")

	twice := orgencryption.UseCaseStores{UseCase: useCase, Stores: []orgencryption.CopyStore{ti.store}}
	_, err = orgencryption.NewMigrator(testenv.NewLogger(t), ti.conn, ti.runner, ti.transitions, []orgencryption.UseCaseStores{twice, twice}, testenv.NewMeterProvider(t))
	require.Error(t, err, "a use case is listed once")
}

func TestMigrator_BackfillRefusesAUseCaseWithoutStores(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	_, err := ti.migrator(t).Backfill(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets)
	require.ErrorIs(t, err, orgencryption.ErrNoCopyStores)
}

func TestMigrator_ListDueBackfillsOffersANewlyEnrolledOrganization(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)

	require.Equal(t, []string{ti.orgID}, dueOrganizations(t, ti.migrator(t)), "it has never been verified")
}

func TestMigrator_ListDueBackfillsSkipsOrganizationsNotInDualWrite(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})

	require.Empty(t, dueOrganizations(t, ti.migrator(t)), "platform_key organizations wait for enroll")

	ti.enroll(t)
	ti.finalize(t)

	require.Empty(t, dueOrganizations(t, ti.migrator(t)), "org_kms organizations are done")
}

func TestMigrator_ListDueBackfillsSkipsVerifiedOrganizationsWithNothingWaiting(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	migrator := ti.migrator(t)

	result, err := migrator.Backfill(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.True(t, result.VerificationPassed)

	require.Empty(t, dueOrganizations(t, migrator))

	ti.store.put(ti.orgID, "b", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "beta")), Org: orgencryption.ErasedCopies().Org})
	require.Equal(t, []string{ti.orgID}, dueOrganizations(t, migrator), "a value written without an organization-key copy makes it due again")
}

func TestMigrator_ListDueBackfillsWaitsBeforeRetryingAFailedVerification(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	migrator := ti.migrator(t)

	recent := time.Now().Add(-time.Hour)
	ti.setRunTimes(t, ti.orgID, &recent, &recent, nil)
	require.Empty(t, dueOrganizations(t, migrator), "nothing waits and the failed verification is recent")

	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	require.Equal(t, []string{ti.orgID}, dueOrganizations(t, migrator), "waiting values are due whatever the verification")

	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: conv.ToPGText("sealed elsewhere")})
	old := time.Now().Add(-orgencryption.VerificationRetryInterval - time.Minute)
	ti.setRunTimes(t, ti.orgID, &old, &old, nil)
	require.Equal(t, []string{ti.orgID}, dueOrganizations(t, migrator), "the retry interval has passed")
}

func TestMigrator_ListDueBackfillsOffersAVerificationOlderThanTheLastBackfill(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	ti.backdate(t, 9*time.Hour)
	migrator := ti.migrator(t)

	verified := time.Now().Add(-8 * time.Hour)
	started := time.Now().Add(-7 * time.Hour)
	completed := started.Add(time.Minute)
	ti.setRunTimes(t, ti.orgID, &started, &completed, &verified)

	require.Equal(t, []string{ti.orgID}, dueOrganizations(t, migrator))
}

func TestTransitions_SweepPlatformCopiesClearsStragglersWithoutChangingState(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	ti.finalize(t)
	cleared, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	require.NoError(t, err)

	ti.store.put(ti.orgID, "late", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "late")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put(ti.orgID, "both", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "both")), Org: conv.ToPGText("organization-key copy")})

	stragglers, err := ti.transitions.SweepPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, []orgencryption.StragglerCount{{Columns: fakeColumns, Cleared: orgencryption.ClearedCopies{PlatformOnly: 1, BesideOrgCopy: 1}}}, stragglers)
	require.False(t, ti.store.get("late").Platform.Valid)
	require.False(t, ti.store.get("both").Platform.Valid)
	require.True(t, ti.store.get("both").Org.Valid, "the organization-key copy stays")

	after, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, cleared.Generation, after.Generation)
	require.Equal(t, cleared.PlatformCopiesClearedAt, after.PlatformCopiesClearedAt)

	again, err := ti.transitions.SweepPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Empty(t, again, "nothing is left to report")
}

func TestTransitions_SweepPlatformCopiesRequiresAFinishedClear(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)

	_, err := ti.transitions.SweepPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed, "refused in dual_write")

	ti.finalize(t)

	_, err = ti.transitions.SweepPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed, "refused before the clear finished")
}

func TestMigrator_ListFinalizedOffersStalledClearsAfterTheGraceAndRecentClears(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)
	ti.finalize(t)
	migrator := ti.migrator(t)

	finalized, err := migrator.ListFinalized(t.Context())
	require.NoError(t, err)
	require.Empty(t, finalized, "a clear started moments ago belongs to the finalizing operator")

	ti.backdate(t, 2*time.Hour)
	finalized, err = migrator.ListFinalized(t.Context())
	require.NoError(t, err)
	require.Len(t, finalized, 1)
	require.Nil(t, finalized[0].PlatformCopiesClearedAt, "offered as a stalled clear")

	require.NoError(t, migrator.ResumeClear(t.Context(), ti.orgID, useCase))
	require.False(t, ti.store.get("a").Platform.Valid)

	finalized, err = migrator.ListFinalized(t.Context())
	require.NoError(t, err)
	require.Len(t, finalized, 1)
	require.NotNil(t, finalized[0].PlatformCopiesClearedAt, "offered for the straggler sweep")

	ti.backdate(t, 8*24*time.Hour)
	finalized, err = migrator.ListFinalized(t.Context())
	require.NoError(t, err)
	require.Empty(t, finalized, "past the straggler window")
}

func TestMigrator_SweepStragglersReturnsWhatItCleared(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	migrator := ti.migrator(t)
	_, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	require.NoError(t, err)

	cleared, err := migrator.SweepStragglers(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Zero(t, cleared)

	ti.store.put(ti.orgID, "late", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "late")), Org: orgencryption.ErasedCopies().Org})
	cleared, err = migrator.SweepStragglers(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, int64(1), cleared)
}

func TestRunner_RecordsTheEndOfARunAfterItsVerification(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.NotNil(t, transition.LastBackfill.StartedAt)
	require.NotNil(t, transition.LastVerifiedAt)
	require.NotNil(t, transition.LastBackfill.CompletedAt)
	require.False(t, transition.LastVerifiedAt.Before(*transition.LastBackfill.StartedAt), "verification runs inside the run's claim")
	require.False(t, transition.LastBackfill.CompletedAt.Before(*transition.LastVerifiedAt), "the claim is released only after verification")
}

func TestRunner_RecordsAFailedVerificationAsTheRunsError(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	mismatched, err := ti.cipher.Seal(t.Context(), ti.decide(t), fakeColumns, []byte("organization"))
	require.NoError(t, err)
	mismatched.Platform = conv.ToPGText(ti.platformCopy(t, "platform"))
	ti.store.put(ti.orgID, "m", mismatched)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err, "a failed verification is reported, not returned")
	require.Equal(t, int64(1), result.Mismatched)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.NotNil(t, transition.LastBackfill.Error)
	require.Contains(t, *transition.LastBackfill.Error, "verification failed")
}

func TestMigrator_ListDueBackfillsOffersARunThatNeverEndedOnceItsClaimExpires(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	ti.backdate(t, 2*time.Hour)
	migrator := ti.migrator(t)
	verified := time.Now().Add(-90 * time.Minute)

	ti.setRunTimes(t, ti.orgID, new(time.Now().Add(-time.Minute)), nil, &verified)
	require.Empty(t, dueOrganizations(t, migrator), "a run holds the claim")

	ti.setRunTimes(t, ti.orgID, new(time.Now().Add(-orgencryption.RunLease-time.Minute)), nil, &verified)
	require.Equal(t, []string{ti.orgID}, dueOrganizations(t, migrator), "the run's process died and its claim expired")

	result, err := migrator.Backfill(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.True(t, result.VerificationPassed)
	require.Empty(t, dueOrganizations(t, migrator))
}

// failingCountStore fails Count for one organization.
type failingCountStore struct {
	*fakeCopyStore

	organizationID string
}

func (f failingCountStore) Count(ctx context.Context, organizationID string) (orgencryption.CopyCounts, error) {
	if organizationID == f.organizationID {
		return orgencryption.CopyCounts{}, errors.New("count failed")
	}

	return f.fakeCopyStore.Count(ctx, organizationID)
}

func TestMigrator_ListDueBackfillsLeavesOutAnOrganizationWhoseCountsFail(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	other := createOrganization(t, ti.conn)
	ti.enroll(t)
	_, err := ti.transitions.Enroll(t.Context(), other, useCase, orgencryption.SystemActor())
	require.NoError(t, err)

	recent := time.Now().Add(-time.Minute)
	ti.setRunTimes(t, ti.orgID, &recent, &recent, nil)
	ti.setRunTimes(t, other, &recent, &recent, nil)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put(other, "b", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "beta")), Org: orgencryption.ErasedCopies().Org})

	migrator, err := orgencryption.NewMigrator(testenv.NewLogger(t), ti.conn, ti.runner, ti.transitions, []orgencryption.UseCaseStores{{UseCase: useCase, Stores: []orgencryption.CopyStore{failingCountStore{fakeCopyStore: ti.store, organizationID: ti.orgID}}}}, testenv.NewMeterProvider(t))
	require.NoError(t, err)

	require.Equal(t, []string{other}, dueOrganizations(t, migrator))
}

func TestMigrator_ListDueBackfillsOffersTheLeastRecentlyAttemptedFirst(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	other := createOrganization(t, ti.conn)
	never := createOrganization(t, ti.conn)
	for _, organizationID := range []string{ti.orgID, other, never} {
		_, err := ti.transitions.Enroll(t.Context(), organizationID, useCase, orgencryption.SystemActor())
		require.NoError(t, err)
		ti.store.put(organizationID, "value-"+organizationID, orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, organizationID)), Org: orgencryption.ErasedCopies().Org})
	}

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-time.Hour)
	ti.setRunTimes(t, ti.orgID, &newer, &newer, nil)
	ti.setRunTimes(t, other, &older, &older, nil)

	require.Equal(t, []string{never, other, ti.orgID}, dueOrganizations(t, ti.migrator(t)))
}

// arrivingStore lands a platform-only copy just before its first clear, as a
// writer that bypassed the fence would between a count and the clear.
type arrivingStore struct {
	*fakeCopyStore

	ti      *transitionInstance
	arrived bool
}

func (a *arrivingStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
	if !a.arrived {
		a.arrived = true
		a.put(organizationID, "arrived", orgencryption.Copies{Platform: conv.ToPGText("late platform-key copy"), Org: orgencryption.ErasedCopies().Org})
	}

	return a.fakeCopyStore.ClearPlatformCopies(ctx, organizationID, generation, limit)
}

func TestTransitions_SweepPlatformCopiesReportsWhatTheClearDestroyed(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	_, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	require.NoError(t, err)
	ti.store.put(ti.orgID, "both", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "both")), Org: conv.ToPGText("organization-key copy")})

	stragglers, err := ti.transitions.SweepPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{&arrivingStore{fakeCopyStore: ti.store, ti: ti, arrived: false}})
	require.NoError(t, err)
	require.Equal(t, []orgencryption.StragglerCount{{Columns: fakeColumns, Cleared: orgencryption.ClearedCopies{PlatformOnly: 1, BesideOrgCopy: 1}}}, stragglers, "a platform-only copy that lands just before the clear is reported as platform-only")
}

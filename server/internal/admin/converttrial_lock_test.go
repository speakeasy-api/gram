package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestMarkEnterpriseTrialConverted_LocksLifecycleThenAllKeysBeforeRowReads(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, _ := newProductionRearmService(t)
	const orgID = "org_convert_lock_order"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}

	lifecycleBlocker := testenv.BeginTx(t, ctx, conn)
	_, err := trialsRepo.New(lifecycleBlocker).LockTrialLifecycle(ctx, orgID)
	require.NoError(t, err)
	converted := make(chan error, 1)
	go func() {
		_, callErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- callErr
	}()

	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	requireAdminCondition(t, waitCtx, conn, func(check context.Context) (bool, error) {
		return testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%SELECT tier, ends_at, converted_at, demoted_at%")
	}, "conversion did not block on the lifecycle row")

	probe, err := conn.Acquire(ctx)
	require.NoError(t, err)
	defer probe.Release()
	for _, keyType := range openrouter.AllKeyTypes {
		acquired, lockErr := testrepo.New(probe).TryAcquireOpenRouterKeyBillingLockFixture(ctx, testrepo.TryAcquireOpenRouterKeyBillingLockFixtureParams{OrganizationID: orgID, KeyType: string(keyType)})
		require.NoError(t, lockErr)
		require.Truef(t, acquired, "%s advisory lock was taken before lifecycle lock", keyType)
		unlocked, unlockErr := activitiesrepo.New(probe).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(keyType)})
		require.NoError(t, unlockErr)
		require.True(t, unlocked)
	}

	internalLock, err := conn.Acquire(ctx)
	require.NoError(t, err)
	defer internalLock.Release()
	internalParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal)}
	require.NoError(t, activitiesrepo.New(internalLock).AcquireOpenRouterKeyBillingLock(ctx, internalParams))
	require.NoError(t, lifecycleBlocker.Commit(ctx))

	chatWait, cancelChat := context.WithTimeout(ctx, 2*time.Second)
	defer cancelChat()
	requireAdminCondition(t, chatWait, conn, func(check context.Context) (bool, error) {
		acquired, lockErr := testrepo.New(probe).TryAcquireOpenRouterKeyBillingLockFixture(check, testrepo.TryAcquireOpenRouterKeyBillingLockFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
		if lockErr != nil {
			return false, fmt.Errorf("probe chat advisory lock: %w", lockErr)
		}
		if !acquired {
			return true, nil
		}
		_, unlockErr := activitiesrepo.New(probe).ReleaseOpenRouterKeyBillingLock(check, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
		if unlockErr != nil {
			return false, fmt.Errorf("release chat advisory lock probe: %w", unlockErr)
		}
		return false, nil
	}, "chat advisory lock was not acquired before internal")

	contender := testenv.BeginTx(t, ctx, conn)
	contenderDone := make(chan error, 1)
	go func() {
		_, contenderErr := trialsRepo.New(contender).LockTrialLifecycle(ctx, orgID)
		contenderDone <- contenderErr
	}()
	contenderWait, cancelContender := context.WithTimeout(ctx, 2*time.Second)
	defer cancelContender()
	requireAdminCondition(t, contenderWait, conn, func(check context.Context) (bool, error) {
		return testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%SELECT tier, ends_at, converted_at, demoted_at%")
	}, "concurrent lifecycle contender was not blocked by conversion")

	rowProbe := testenv.BeginTx(t, ctx, conn)
	_, err = testrepo.New(rowProbe).LockOrganizationMetadataForUpdateNowaitFixture(ctx, orgID)
	require.NoError(t, err, "organization row was read before every advisory lock")
	causesByKey, err := testrepo.New(rowProbe).ListOpenRouterAPIKeyDisableCausesForUpdateNowaitFixture(ctx, orgID)
	require.NoError(t, err, "key rows were read before every advisory lock")
	require.Len(t, causesByKey, len(openrouter.AllKeyTypes))
	require.NoError(t, rowProbe.Rollback(ctx))

	keyBlocker := testenv.BeginTx(t, ctx, conn)
	_, err = testrepo.New(keyBlocker).LockOpenRouterAPIKeyForUpdateFixture(ctx, testrepo.LockOpenRouterAPIKeyForUpdateFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, err)

	unlocked, err := activitiesrepo.New(internalLock).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalParams))
	require.NoError(t, err)
	require.True(t, unlocked)

	orgLockWait, cancelOrgLock := context.WithTimeout(ctx, 2*time.Second)
	defer cancelOrgLock()
	requireAdminCondition(t, orgLockWait, conn, func(check context.Context) (bool, error) {
		_, lockErr := testrepo.New(probe).LockOrganizationMetadataForUpdateNowaitFixture(check, orgID)
		if lockErr == nil {
			return false, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(lockErr, &pgErr) && pgErr.Code == pgerrcode.LockNotAvailable {
			return true, nil
		}
		return false, fmt.Errorf("probe conversion organization lock: %w", lockErr)
	}, "conversion did not lock organization row after advisory locks")

	updateDone := make(chan error, 1)
	whitelisted := false
	go func() {
		_, updateErr := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: orgID, Whitelisted: &whitelisted})
		updateDone <- updateErr
	}()
	updateWait, cancelUpdate := context.WithTimeout(ctx, 2*time.Second)
	defer cancelUpdate()
	requireAdminCondition(t, updateWait, conn, func(check context.Context) (bool, error) {
		return testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%UPDATE organization_metadata%")
	}, "concurrent organization update was not blocked by conversion snapshot lock")

	require.NoError(t, keyBlocker.Rollback(ctx))
	select {
	case err = <-converted:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion did not finish")
	}
	select {
	case err = <-contenderDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "lifecycle contender did not resume")
	}
	require.NoError(t, contender.Rollback(ctx))
	select {
	case err = <-updateDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "concurrent organization update did not resume")
	}
	require.False(t, readOrgState(t, ctx, conn, orgID).Whitelisted, "queued update must apply after conversion rather than be lost")
	record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	var after struct {
		Organization struct {
			Whitelisted bool `json:"whitelisted"`
		} `json:"organization"`
	}
	require.NoError(t, json.Unmarshal(record.AfterSnapshot, &after))
	require.True(t, after.Organization.Whitelisted, "audit snapshot must describe conversion state under the organization lock")
}

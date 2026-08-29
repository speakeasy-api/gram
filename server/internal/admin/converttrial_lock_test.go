package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

type cancelOnRedisSetHook struct {
	cancel func()
	once   sync.Once
	called chan struct{}
}

func (h *cancelOnRedisSetHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *cancelOnRedisSetHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" {
			h.once.Do(func() {
				h.cancel()
				close(h.called)
			})
		}
		return next(ctx, cmd)
	}
}

func (h *cancelOnRedisSetHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return next(ctx, cmds)
	}
}

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

func TestMarkEnterpriseTrialConverted_CancellationDuringPostCommitCacheRefreshDoesNotLeaveStaleCache(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, _ := newProductionRearmService(t)
	const orgID = "org_convert_cache_cancel"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		key := productfeatures.FeatureCacheKey(orgID, feature) + ":"
		require.NoError(t, redisClient.Del(ctx, key).Err())
	}
	requestCtx, cancelRequest := context.WithCancel(ctx)
	hook := &cancelOnRedisSetHook{cancel: cancelRequest, called: make(chan struct{})}
	redisClient.AddHook(hook)
	svc.productFeatures = productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, redisClient)

	converted := make(chan error, 1)
	go func() {
		_, callErr := svc.MarkEnterpriseTrialConverted(requestCtx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- callErr
	}()

	select {
	case <-hook.called:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion never attempted its post-commit cache refresh")
	}
	select {
	case <-converted:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion did not return after request cancellation")
	}

	for _, feature := range productfeatures.TrialRuntimeFeatures {
		key := productfeatures.FeatureCacheKey(orgID, feature) + ":"
		exists, existsErr := redisClient.Exists(ctx, key).Result()
		require.NoError(t, existsErr)
		require.Equalf(t, int64(1), exists, "%s cache entry was not safely refreshed after durable commit", feature)
		enabled, cacheErr := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, cacheErr)
		require.Truef(t, enabled, "%s cache entry did not match committed state", feature)
	}
}

func TestMarkEnterpriseTrialConverted_RetryCancellationDuringCacheRefreshDoesNotLeaveStaleCache(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, _ := newProductionRearmService(t)
	const orgID = "org_convert_retry_cache_cancel"
	convertedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: convertedAt, convertedAt: &convertedAt})
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		_, err := featurerepo.New(conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{OrganizationID: orgID, FeatureName: string(feature)})
		require.NoError(t, err)
	}

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		key := productfeatures.FeatureCacheKey(orgID, feature) + ":"
		require.NoError(t, redisClient.Del(ctx, key).Err())
	}
	requestCtx, cancelRequest := context.WithCancel(ctx)
	hook := &cancelOnRedisSetHook{cancel: cancelRequest, called: make(chan struct{})}
	redisClient.AddHook(hook)
	svc.productFeatures = productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, redisClient)

	retried := make(chan error, 1)
	go func() {
		_, callErr := svc.MarkEnterpriseTrialConverted(requestCtx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		retried <- callErr
	}()

	select {
	case <-hook.called:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion retry never attempted its cache refresh")
	}
	select {
	case <-retried:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion retry did not return after request cancellation")
	}

	for _, feature := range productfeatures.TrialRuntimeFeatures {
		key := productfeatures.FeatureCacheKey(orgID, feature) + ":"
		exists, existsErr := redisClient.Exists(ctx, key).Result()
		require.NoError(t, existsErr)
		require.Equalf(t, int64(1), exists, "%s cache entry was not safely refreshed on retry", feature)
		enabled, cacheErr := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, cacheErr)
		require.Truef(t, enabled, "%s retry cache entry did not match durable state", feature)
	}
}

func TestMarkEnterpriseTrialConverted_MissingKeyWaitsForProvisioningAndReReads(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, _ := newProductionRearmService(t)
	const orgID = "org_convert_missing_key_race"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 7, disabled: true})

	provisioning := testenv.BeginTx(t, ctx, conn)
	keys := orrepo.New(provisioning)
	require.NoError(t, keys.LockOpenRouterKeyProvisioning(ctx, orrepo.LockOpenRouterKeyProvisioningParams{
		OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat),
	}))

	converted := make(chan error, 1)
	go func() {
		_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- err
	}()

	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	requireAdminCondition(t, waitCtx, conn, func(check context.Context) (bool, error) {
		return testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%LockOpenRouterKeyProvisioning%")
	}, "conversion did not wait for in-flight first-time key provisioning")

	ciphertext, err := testenv.NewEncryptionClient(t).Encrypt([]byte("sk-test-racing-provisioner"))
	require.NoError(t, err)
	_, err = keys.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
		KeyEncrypted:   conv.ToPGText(ciphertext),
		KeyHash:        "hash-racing-provisioner",
		MonthlyCredits: 7,
	})
	require.NoError(t, err)
	require.NoError(t, provisioning.Commit(ctx))

	select {
	case err = <-converted:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion did not resume after provisioning committed")
	}

	key := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	floor, ok := openrouter.DefaultCreditLimit(orgID, "enterprise", false)
	require.True(t, ok)
	require.GreaterOrEqual(t, key.MonthlyCredits, int64(floor), "conversion must re-read and promote the newly provisioned key")
}

func TestMarkEnterpriseTrialConverted_SerializesRuntimeFeatureWritesThroughCacheRefresh(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, _ := newProductionRearmService(t)
	const orgID = "org_convert_feature_race"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}

	keyBlocker, err := conn.Acquire(ctx)
	require.NoError(t, err)
	defer keyBlocker.Release()
	internalLock := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal)}
	require.NoError(t, activitiesrepo.New(keyBlocker).AcquireOpenRouterKeyBillingLock(ctx, internalLock))
	defer func() {
		_, _ = activitiesrepo.New(keyBlocker).ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalLock))
	}()

	converted := make(chan error, 1)
	go func() {
		_, callErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- callErr
	}()
	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	requireAdminCondition(t, waitCtx, conn, func(check context.Context) (bool, error) {
		return testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%AcquireOpenRouterBillingLock%")
	}, "conversion did not reach the key lock after acquiring feature locks")

	featureWriter, err := conn.Acquire(ctx)
	require.NoError(t, err)
	defer featureWriter.Release()
	feature := productfeatures.FeatureLogs
	written := make(chan error, 1)
	go func() {
		q := featurerepo.New(featureWriter)
		params := featurerepo.AcquireFeatureCacheLockParams{OrganizationID: orgID, FeatureName: string(feature)}
		if lockErr := q.AcquireFeatureCacheLock(ctx, params); lockErr != nil {
			written <- lockErr
			return
		}
		defer q.ReleaseFeatureCacheLock(context.WithoutCancel(ctx), featurerepo.ReleaseFeatureCacheLockParams(params)) //nolint:errcheck
		tx, txErr := featureWriter.Begin(ctx)
		if txErr == nil {
			_, txErr = featurerepo.New(tx).DeleteFeature(ctx, featurerepo.DeleteFeatureParams{OrganizationID: orgID, FeatureName: string(feature)})
		}
		if txErr == nil {
			txErr = tx.Commit(ctx)
		} else if tx != nil {
			_ = tx.Rollback(ctx)
		}
		if txErr == nil {
			txErr = svc.productFeatures.UpdateFeatureCacheUnderLock(ctx, featureWriter, orgID, feature)
		}
		written <- txErr
	}()

	featureWait, cancelFeatureWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelFeatureWait()
	requireAdminCondition(t, featureWait, conn, func(check context.Context) (bool, error) {
		return testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%AcquireFeatureCacheLock%")
	}, "overlapping feature disable was not serialized behind conversion")

	unlocked, err := activitiesrepo.New(keyBlocker).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalLock))
	require.NoError(t, err)
	require.True(t, unlocked)
	select {
	case err = <-converted:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "conversion did not finish")
	}
	select {
	case err = <-written:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "queued feature disable did not finish")
	}
	enabled, err := featurerepo.New(conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: orgID, FeatureName: string(feature)})
	require.NoError(t, err)
	require.False(t, enabled, "later-completed disable must not be overwritten by conversion")
	cached, err := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
	require.NoError(t, err)
	require.False(t, cached, "later-completed disable must own the final cache value")
}

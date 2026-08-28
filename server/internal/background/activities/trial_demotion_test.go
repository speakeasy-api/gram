package activities_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// trialProvisioner uses the production local cause mutation against real
// PostgreSQL while faking only the post-commit OpenRouter reconciliation.
type trialProvisioner struct {
	*openrouter.Development
	local *openrouter.OpenRouter

	reconciled        []string
	reconcileFailures int
}

type recordingTrialNotifier struct {
	inactive []string
}

func (n *recordingTrialNotifier) TrialStarted(context.Context, string) error {
	return nil
}

func (n *recordingTrialNotifier) AdminAdded(context.Context, string, string) error {
	return nil
}

func (n *recordingTrialNotifier) TrialInactive(_ context.Context, organizationID string) error {
	n.inactive = append(n.inactive, organizationID)
	return nil
}

var _ openrouter.Provisioner = (*trialProvisioner)(nil)

func (p *trialProvisioner) AddAPIKeyDisableCauseWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	change, err := p.local.AddAPIKeyDisableCauseWithDB(ctx, db, orgID, keyType, cause)
	if err != nil {
		return change, fmt.Errorf("add OpenRouter API key disable cause: %w", err)
	}
	return change, nil
}

func (p *trialProvisioner) ReconcileAPIKeyDisabled(_ context.Context, orgID string, keyType openrouter.KeyType) error {
	p.reconciled = append(p.reconciled, orgID+":"+string(keyType))
	if p.reconcileFailures > 0 {
		p.reconcileFailures--
		return errors.New("openrouter unavailable")
	}
	return nil
}

type trialTestInstance struct {
	conn            *pgxpool.Pool
	trials          *trialsrepo.Queries
	orgs            *orgrepo.Queries
	productFeatures *productfeatures.Client
	provisioner     *trialProvisioner
	notifier        *recordingTrialNotifier
	activity        *activities.DemoteExpiredTrials
}

func newTrialTestInstance(t *testing.T) (context.Context, *trialTestInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "trialdemotion")
	require.NoError(t, err)

	provisioner := &trialProvisioner{Development: openrouter.NewDevelopment(""), local: new(openrouter.OpenRouter)}
	notifier := &recordingTrialNotifier{inactive: nil}
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	productFeatures := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, redisClient)

	return ctx, &trialTestInstance{
		conn:            conn,
		trials:          trialsrepo.New(conn),
		orgs:            orgrepo.New(conn),
		productFeatures: productFeatures,
		provisioner:     provisioner,
		notifier:        notifier,
		activity: activities.NewDemoteExpiredTrials(
			testenv.NewLogger(t),
			conn,
			provisioner,
			audit.NewLogger(),
			notifier,
			productFeatures,
		),
	}
}

// newTrialOrg creates an enterprise organization that is whitelisted for the
// duration of its trial, which is the state signup leaves behind.
func newTrialOrg(t *testing.T, ctx context.Context, ti *trialTestInstance, endsAt time.Time) string {
	t.Helper()

	orgID := "org-" + uuid.NewString()[:8]

	_, err := ti.orgs.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Trial Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	require.NoError(t, ti.orgs.SetAccountType(ctx, orgrepo.SetAccountTypeParams{
		ID:              orgID,
		GramAccountType: "enterprise",
	}))

	err = ti.trials.CreateTrial(ctx, trialsrepo.CreateTrialParams{
		OrganizationID: orgID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: endsAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	return orgID
}

func materializeTrialKey(t *testing.T, ctx context.Context, ti *trialTestInstance, orgID string, keyType openrouter.KeyType, causes []string) {
	t.Helper()

	_, err := openrouterrepo.New(ti.conn).CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash-" + string(keyType),
		MonthlyCredits: 100,
	})
	require.NoError(t, err)
	err = testrepo.New(ti.conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		DisableCauses:  causes,
		Disabled:       causes == nil || len(causes) > 0,
	})
	require.NoError(t, err)
}

func trialKey(t *testing.T, ctx context.Context, ti *trialTestInstance, orgID string, keyType openrouter.KeyType) openrouterrepo.OpenrouterApiKey {
	t.Helper()
	key, err := openrouterrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	require.NoError(t, err)
	return key
}

func TestDemoteExpiredTrials_LocksOutExpiredTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	endsAt := time.Now().Add(-time.Hour).UTC()
	orgID := newTrialOrg(t, ctx, ti, endsAt)
	for _, keyType := range openrouter.AllKeyTypes {
		materializeTrialKey(t, ctx, ti, orgID, keyType, []string{})
	}
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, productfeatures.SeedOrganizationDefaultsTx(ctx, tx, orgID))
	q := featurerepo.New(tx)
	features := slices.Concat(productfeatures.TrialRuntimeFeatures, []productfeatures.Feature{
		productfeatures.FeatureSSO,
		productfeatures.FeatureAuthzChallengeLogging,
	})
	for _, feature := range features {
		_, err := q.EnableFeature(ctx, featurerepo.EnableFeatureParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(ctx))
	for _, feature := range features {
		_, err := ti.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, err)
	}

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)

	expired, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, expired)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.False(t, org.Whitelisted)

	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.True(t, trial.DemotedAt.Valid)

	require.Equal(t, []string{orgID + ":chat", orgID + ":internal"}, ti.provisioner.reconciled)
	for _, keyType := range openrouter.AllKeyTypes {
		key := trialKey(t, ctx, ti, orgID, keyType)
		require.Equal(t, []string{"trial_demotion"}, key.DisableCauses)
		require.True(t, key.Disabled)
	}
	require.Equal(t, []string{orgID}, ti.notifier.inactive)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, err := ti.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, err)
		require.Falsef(t, enabled, "demotion should disable %s", feature)
	}
	for _, feature := range []productfeatures.Feature{
		productfeatures.FeatureSSO,
		productfeatures.FeatureAuthzChallengeLogging,
	} {
		enabled, err := ti.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, err)
		require.Truef(t, enabled, "demotion should preserve %s", feature)
	}

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)
	require.Equal(t, "organization", entry.SubjectType)
	require.Equal(t, orgID, entry.SubjectSlug)

	metadata, err := audittest.DecodeAuditData(entry.Metadata)
	require.NoError(t, err)
	require.Equal(t, "enterprise", metadata["previous_account_type"])
	require.NotEmpty(t, metadata["trial_ends_at"])
	require.Equal(t, true, metadata["key_access_changed"])
}

// The sweep predicate is the only thing standing between a paying customer and
// a lockout, so each of its three clauses is pinned.
func TestDemoteExpiredTrials_ListSkipsTrialsThatAreNotDue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)

	active := newTrialOrg(t, ctx, ti, time.Now().Add(24*time.Hour).UTC())
	converted := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	demoted := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	expired := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	_, err := ti.trials.MarkTrialConverted(ctx, converted)
	require.NoError(t, err)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: demoted}))

	due, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{expired}, due)
	require.NotContains(t, due, active)
}

// A conversion that lands between the list and the write must win, and the
// organization must be left exactly as the conversion left it.
func TestDemoteExpiredTrials_DemoteSkipsTrialConvertedAfterListing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	expired, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, expired)

	_, err = ti.trials.MarkTrialConverted(ctx, orgID)
	require.NoError(t, err)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	require.True(t, org.Whitelisted)

	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.False(t, trial.DemotedAt.Valid)

	require.Empty(t, ti.provisioner.reconciled, "a trial that converted keeps its keys")
	require.Empty(t, ti.notifier.inactive, "a no-op demotion must not publish trial inactivity")
}

// Temporal retries a failed activity, so a second demotion of the same trial
// must not write a second audit entry.
func TestDemoteExpiredTrials_DemoteIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	firstDemotedAt := trial.DemotedAt.Time

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	trial, err = ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, firstDemotedAt, trial.DemotedAt.Time)

	again, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)
	require.Equal(t, after, again)
	require.Equal(t, []string{orgID}, ti.notifier.inactive, "a retried demotion notifies exactly once")
}

func TestDemoteExpiredTrials_PostCommitReconcileFailureRetriesCommittedIntent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	for _, keyType := range openrouter.AllKeyTypes {
		materializeTrialKey(t, ctx, ti, orgID, keyType, []string{})
	}
	ti.provisioner.reconcileFailures = 1

	require.ErrorContains(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}), "openrouter unavailable")

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.False(t, org.Whitelisted)
	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.True(t, trial.DemotedAt.Valid)
	for _, keyType := range openrouter.AllKeyTypes {
		require.Equal(t, []string{"trial_demotion"}, trialKey(t, ctx, ti, orgID, keyType).DisableCauses)
	}

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))
	require.Equal(t, []string{orgID + ":chat", orgID + ":internal", orgID + ":chat", orgID + ":internal"}, ti.provisioner.reconciled)
	require.Equal(t, []string{orgID}, ti.notifier.inactive)
}

func TestDemoteExpiredTrials_PreservesLayeredDisableCauses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	materializeTrialKey(t, ctx, ti, orgID, openrouter.KeyTypeChat, []string{"admin_lock"})
	materializeTrialKey(t, ctx, ti, orgID, openrouter.KeyTypeInternal, []string{"billing_inactive"})

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	chatKey := trialKey(t, ctx, ti, orgID, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock", "trial_demotion"}, chatKey.DisableCauses)
	require.True(t, chatKey.Disabled, "adding trial_demotion over admin_lock remains disabled")
	internalKey := trialKey(t, ctx, ti, orgID, openrouter.KeyTypeInternal)
	require.Equal(t, []string{"trial_demotion", "billing_inactive"}, internalKey.DisableCauses)
	require.True(t, internalKey.Disabled)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(entry.Metadata)
	require.NoError(t, err)
	require.Equal(t, false, metadata["key_access_changed"], "layering a cause onto disabled keys does not change access")
}

func TestDemoteExpiredTrials_MissingKeysAreSafe(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))
	require.Equal(t, []string{orgID + ":chat", orgID + ":internal"}, ti.provisioner.reconciled)
}

func TestDemoteExpiredTrials_NullDisableCausesFailClosedAndRollBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	materializeTrialKey(t, ctx, ti, orgID, openrouter.KeyTypeChat, nil)

	err := ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID})
	require.ErrorContains(t, err, "unclassified key")

	trial, getErr := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, getErr)
	require.False(t, trial.DemotedAt.Valid)
	org, getErr := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, getErr)
	require.Equal(t, "enterprise", org.GramAccountType)
	key := trialKey(t, ctx, ti, orgID, openrouter.KeyTypeChat)
	require.Nil(t, key.DisableCauses)
	require.True(t, key.Disabled, "NULL cause state remains fail-closed")
	require.Empty(t, ti.provisioner.reconciled)
}

func TestDemoteExpiredTrials_AuditFailureRollsBackLifecycleCausesAndOutbox(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	for _, keyType := range openrouter.AllKeyTypes {
		materializeTrialKey(t, ctx, ti, orgID, keyType, []string{})
	}
	beforeOutbox, err := testrepo.New(ti.conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted))

	require.Error(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.False(t, trial.DemotedAt.Valid)
	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	for _, keyType := range openrouter.AllKeyTypes {
		key := trialKey(t, ctx, ti, orgID, keyType)
		require.Empty(t, key.DisableCauses)
		require.False(t, key.Disabled)
	}
	afterOutbox, err := testrepo.New(ti.conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeOutbox, afterOutbox)
	require.Empty(t, ti.provisioner.reconciled)
	require.Empty(t, ti.notifier.inactive)
}

func TestDemoteExpiredTrials_LocksLifecycleBeforeAllKeysAndRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	for _, keyType := range openrouter.AllKeyTypes {
		materializeTrialKey(t, ctx, ti, orgID, keyType, []string{})
	}

	rowLock := testenv.BeginTx(t, ctx, ti.conn)
	_, err := trialsrepo.New(rowLock).LockTrialLifecycleForRearm(ctx, orgID)
	require.NoError(t, err)

	demoted := make(chan error, 1)
	go func() { demoted <- ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}) }()

	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	requireCondition(t, waitCtx, func() (bool, error) {
		blocked, err := testrepo.New(ti.conn).IsQueryBlockedOnLockFixture(waitCtx, "%UPDATE trials%")
		if err != nil {
			return false, fmt.Errorf("check blocked trial demotion query: %w", err)
		}
		return blocked, nil
	}, "demotion did not block on the trial row")

	probe, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer probe.Release()
	for _, keyType := range openrouter.AllKeyTypes {
		acquired, err := testrepo.New(probe).TryAcquireOpenRouterKeyBillingLockFixture(ctx, testrepo.TryAcquireOpenRouterKeyBillingLockFixtureParams{
			KeyType: string(keyType), OrganizationID: orgID,
		})
		require.NoError(t, err)
		require.Truef(t, acquired, "%s lock must remain acquirable while the trial row is blocked", keyType)
		unlocked, unlockErr := activitiesrepo.New(probe).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(keyType)})
		require.NoError(t, unlockErr)
		require.True(t, unlocked)
	}

	internalLockConn, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer internalLockConn.Release()
	internalParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal)}
	require.NoError(t, activitiesrepo.New(internalLockConn).AcquireOpenRouterKeyBillingLock(ctx, internalParams))
	internalLocked := true
	defer func() {
		if !internalLocked {
			return
		}
		_, _ = activitiesrepo.New(internalLockConn).ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalParams))
	}()

	require.NoError(t, rowLock.Commit(ctx))

	chatHeldCtx, cancelChatHeld := context.WithTimeout(ctx, 2*time.Second)
	defer cancelChatHeld()
	requireCondition(t, chatHeldCtx, func() (bool, error) {
		acquired, err := testrepo.New(probe).TryAcquireOpenRouterKeyBillingLockFixture(chatHeldCtx, testrepo.TryAcquireOpenRouterKeyBillingLockFixtureParams{
			KeyType: string(openrouter.KeyTypeChat), OrganizationID: orgID,
		})
		if err != nil {
			return false, fmt.Errorf("probe chat billing lock: %w", err)
		}
		if !acquired {
			return true, nil
		}
		unlocked, unlockErr := activitiesrepo.New(probe).ReleaseOpenRouterKeyBillingLock(chatHeldCtx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
		if unlockErr != nil {
			return false, fmt.Errorf("release chat billing lock probe: %w", unlockErr)
		}
		if !unlocked {
			return false, errors.New("probe chat lock was not released")
		}
		return false, nil
	}, "chat lock was not acquired before the blocked internal lock")

	keyProbe := testenv.BeginTx(t, ctx, ti.conn)
	causesByKey, err := testrepo.New(keyProbe).ListOpenRouterAPIKeyDisableCausesForUpdateNowaitFixture(ctx, orgID)
	require.NoError(t, err, "key rows must remain unlocked until every advisory lock is held")
	for _, causes := range causesByKey {
		require.Empty(t, causes, "trial_demotion must not be written before the internal lock is acquired")
	}
	require.Len(t, causesByKey, len(openrouter.AllKeyTypes))
	require.NoError(t, keyProbe.Rollback(ctx))

	unlocked, err := activitiesrepo.New(internalLockConn).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalParams))
	require.NoError(t, err)
	require.True(t, unlocked)
	internalLocked = false

	resultCtx, cancelResult := context.WithTimeout(ctx, 2*time.Second)
	defer cancelResult()
	select {
	case err := <-demoted:
		require.NoError(t, err)
	case <-resultCtx.Done():
		require.FailNow(t, "demotion did not finish after releasing the internal lock", resultCtx.Err().Error())
	}
	for _, keyType := range openrouter.AllKeyTypes {
		require.Equal(t, []string{"trial_demotion"}, trialKey(t, ctx, ti, orgID, keyType).DisableCauses)
	}
	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.True(t, trial.DemotedAt.Valid)
	organization, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "free", organization.GramAccountType)
}

func requireCondition(t *testing.T, ctx context.Context, condition func() (bool, error), message string) {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		met, err := condition()
		require.NoError(t, err)
		if met {
			return
		}
		select {
		case <-ctx.Done():
			require.FailNow(t, message, ctx.Err().Error())
		case <-ticker.C:
		}
	}
}

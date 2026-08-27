package activities_test

import (
	"context"
	"errors"
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
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// trialProvisioner records which of an organization's keys were locked down and
// can be made to fail, which is the only openrouter.Provisioner behaviour the
// sweeper depends on.
type trialProvisioner struct {
	*openrouter.Development

	causeAdds            []trialCauseAdd
	adminLocked          bool
	upstreamStatePatches int
	failWith             error
}

type trialCauseAdd struct {
	orgID   string
	keyType openrouter.KeyType
	cause   openrouter.DisableCause
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

func (p *trialProvisioner) AddAPIKeyDisableCauseWithDB(_ context.Context, _ openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	if p.failWith != nil {
		return openrouter.DisableCauseChange{}, p.failWith
	}
	p.causeAdds = append(p.causeAdds, trialCauseAdd{orgID: orgID, keyType: keyType, cause: cause})
	if p.adminLocked {
		return openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: false}, nil
	}
	p.upstreamStatePatches++
	return openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, nil
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

	provisioner := &trialProvisioner{Development: openrouter.NewDevelopment(""), causeAdds: nil, failWith: nil}
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

func TestDemoteExpiredTrials_AddsTrialDemotionToAdminLockedKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	ti.provisioner.adminLocked = true

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))
	require.ElementsMatch(t, []trialCauseAdd{
		{orgID: orgID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseTrialDemotion},
		{orgID: orgID, keyType: openrouter.KeyTypeInternal, cause: openrouter.DisableCauseTrialDemotion},
	}, ti.provisioner.causeAdds)
	require.Zero(t, ti.provisioner.upstreamStatePatches, "an admin-locked key must not receive another disabled-state patch")
}

func TestDemoteExpiredTrials_LocksOutExpiredTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	endsAt := time.Now().Add(-time.Hour).UTC()
	orgID := newTrialOrg(t, ctx, ti, endsAt)
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

	require.ElementsMatch(t, []trialCauseAdd{
		{orgID: orgID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseTrialDemotion},
		{orgID: orgID, keyType: openrouter.KeyTypeInternal, cause: openrouter.DisableCauseTrialDemotion},
	}, ti.provisioner.causeAdds)
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

	require.Empty(t, ti.provisioner.causeAdds, "a trial that converted keeps its keys")
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

// The lockdown runs inside the demotion transaction on purpose: a stamped
// demoted_at drops the row out of the next sweep, so a lockdown that failed
// after the commit would never be retried.
func TestDemoteExpiredTrials_KeyLockdownFailureLeavesTrialArmed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	ti.provisioner.failWith = errors.New("openrouter unavailable")

	require.Error(t, ti.activity.Demote(ctx, activities.DemoteExpiredTrialArgs{OrganizationID: orgID}))

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	require.True(t, org.Whitelisted)

	trial, err := ti.trials.GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.False(t, trial.DemotedAt.Valid)

	due, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, due)
	require.Empty(t, ti.notifier.inactive, "a rolled-back demotion must not publish trial inactivity")
}

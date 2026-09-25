//go:build localaccounts_integration

package localaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/localaccounts/fixtures/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/localaccounts/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Opt in with: mise run test:server -tags=localaccounts_integration ./internal/localaccounts/
// This uses a disposable database container, never the development database.
func TestProfilesElapsedFixtureReconcilesWithoutResurrection(t *testing.T) {
	clone := newProfilePostgres(t)
	db, features := newProfileFixture(t, clone)
	applyProfile(t, db, features, ActiveTrial, time.Now())
	deadline := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, fixturerepo.New(db).SetTrialDeadline(t.Context(), fixturerepo.SetTrialDeadlineParams{OrganizationID: profileOrg, EndsAt: conv.ToPGTimestamptz(deadline)}))
	preserved := snapshotProfileTables(t, db, preservationTables)
	logger, tracer := testenv.NewLogger(t), testenv.NewTracerProvider(t)
	stub := billing.NewStubClientWithLocalProfiles(logger, tracer, db)
	tier, active, err := stub.GetCustomerTier(t.Context(), profileOrg)
	require.NoError(t, err)
	require.Equal(t, billing.TierBase, *tier)
	require.False(t, active)
	handled, err := HandleTrialFixture(t.Context(), db, features, profileOrg)
	require.NoError(t, err)
	require.True(t, handled)
	for range 2 {
		org, err := mv.DescribeOrganization(t.Context(), logger, orgrepo.New(db), stub, profileOrg)
		require.NoError(t, err)
		require.Equal(t, "free", org.GramAccountType)
		require.False(t, org.HasActiveSubscription)
		require.False(t, org.Whitelisted)
	}
	state, err := Status(t.Context(), db, profileOrg)
	require.NoError(t, err)
	require.Equal(t, ActiveTrial, state.Profile)
	dates, err := fixturerepo.New(db).GetTrialDates(t.Context(), profileOrg)
	require.NoError(t, err)
	require.WithinDuration(t, deadline, dates.EndsAt.Time, time.Microsecond)
	require.False(t, profileFeatureEnabled(t, db, string(productfeatures.FeaturePlatformMCP)))
	require.Equal(t, preserved, snapshotProfileTables(t, db, preservationTables))
	beforeRepeat := snapshotProfileTables(t, db, mutationTables)
	handled, err = HandleTrialFixture(t.Context(), db, features, profileOrg)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, beforeRepeat, snapshotProfileTables(t, db, mutationTables))
}

func TestProfilesMarkerDetachesOnOwnerDelete(t *testing.T) {
	clone := newProfilePostgres(t)
	db, features := newProfileFixture(t, clone)
	const org = "org_detached_fixture"
	require.NoError(t, fixturerepo.New(db).CreateDetachedOrganization(t.Context(), org))
	result, err := Apply(t.Context(), db, features, org, Enterprise, false, time.Now(), profileRecheck)
	require.NoError(t, err)
	require.True(t, result.Committed)
	require.NoError(t, fixturerepo.New(db).DeleteOrganization(t.Context(), org))
	detached, err := fixturerepo.New(db).CountDetachedProfiles(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, detached)
	require.NoError(t, fixturerepo.New(db).CreateReplacementOrganization(t.Context(), org))
	stub := billing.NewStubClientWithLocalProfiles(testenv.NewLogger(t), testenv.NewTracerProvider(t), db)
	tier, active, err := stub.GetCustomerTier(t.Context(), org)
	require.NoError(t, err)
	require.Equal(t, billing.TierPro, *tier)
	require.True(t, active)
}

func TestProfilesDryRunWithoutMarkerAndGuardRollback(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	db, client := newProfileFixture(t, clone)
	before := snapshotProfileTables(t, db, append(append([]string{}, preservationTables...), "organization_metadata", "trials", "organization_features"))
	checked := false
	result, err := Apply(t.Context(), db, client, profileOrg, ExpiredTrial, true, now, func(context.Context, pgx.Tx) error { checked = true; return nil })
	require.NoError(t, err)
	require.True(t, checked)
	require.True(t, result.DryRun)
	require.False(t, result.Committed)
	require.True(t, anchor.Equal(result.Anchor))
	exists, err := fixturerepo.New(db).LocalSchemaExists(t.Context())
	require.NoError(t, err)
	require.False(t, exists, "dry-run must not create even the local schema")
	require.Equal(t, before, snapshotProfileTables(t, db, append(append([]string{}, preservationTables...), "organization_metadata", "trials", "organization_features")))

	sentinel := errors.New("membership changed")
	result, err = Apply(t.Context(), db, client, profileOrg, Enterprise, false, now, func(ctx context.Context, tx pgx.Tx) error {
		err := fixturerepo.New(tx).RenameOrganization(ctx, profileOrg)
		require.NoError(t, err)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.False(t, result.Committed)
	require.Equal(t, before, snapshotProfileTables(t, db, append(append([]string{}, preservationTables...), "organization_metadata", "trials", "organization_features")))
	exists, err = fixturerepo.New(db).LocalSchemaExists(t.Context())
	require.NoError(t, err)
	require.False(t, exists)
}

func TestProfilesDryRunExistingProfile(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	db, client := newProfileFixture(t, clone)
	applyProfile(t, db, client, ActiveTrial, now)
	before := snapshotProfileTables(t, db, mutationTables)
	for _, target := range []Profile{ActiveTrial, ExpiredTrial} {
		result, err := Apply(t.Context(), db, client, profileOrg, target, true, now.Add(48*time.Hour), profileRecheck)
		require.NoError(t, err)
		require.False(t, result.Committed)
		if target == ActiveTrial {
			require.True(t, anchor.Equal(result.Anchor))
		} else {
			require.True(t, anchor.Add(48*time.Hour).Equal(result.Anchor))
		}
		require.Equal(t, before, snapshotProfileTables(t, db, mutationTables))
	}
}

func TestProfilesEnterpriseRepairsSameProfile(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	db, client := newProfileFixture(t, clone)
	applyProfile(t, db, client, Enterprise, now)
	require.NoError(t, fixturerepo.New(db).ClearAccountTier(t.Context(), profileOrg))
	require.NoError(t, fixturerepo.New(db).DisableFeature(t.Context(), fixturerepo.DisableFeatureParams{OrganizationID: profileOrg, FeatureName: string(productfeatures.EnterpriseAccessBundle[0])}))
	require.NoError(t, fixturerepo.New(db).CreateTrial(t.Context(), fixturerepo.CreateTrialParams{OrganizationID: profileOrg, EndsAt: conv.ToPGTimestamptz(anchor.Add(-24 * time.Hour))}))
	result := applyProfile(t, db, client, Enterprise, now.Add(96*time.Hour))
	require.True(t, anchor.Equal(result.Anchor))
	assertProfileState(t, db, Enterprise, anchor)

	// A feature-only repair must restore access, even when the
	// marker, account metadata and trial row already match.
	require.NoError(t, fixturerepo.New(db).DisableFeature(t.Context(), fixturerepo.DisableFeatureParams{OrganizationID: profileOrg, FeatureName: string(productfeatures.EnterpriseAccessBundle[1])}))
	applyProfile(t, db, client, Enterprise, now.Add(108*time.Hour))
	assertProfileState(t, db, Enterprise, anchor)
	snapshot := snapshotProfileTables(t, db, mutationTables)
	applyProfile(t, db, client, Enterprise, now.Add(120*time.Hour))
	require.Equal(t, snapshot, snapshotProfileTables(t, db, mutationTables))
}

func TestProfilesStaleActiveTrialRequiresExplicitRestart(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	db, client := newProfileFixture(t, clone)
	applyProfile(t, db, client, ActiveTrial, now)
	before := snapshotProfileTables(t, db, mutationTables)
	deadline := anchor.AddDate(0, 0, 14)
	// Just before the boundary is still active and must remain a no-op.
	applyProfile(t, db, client, ActiveTrial, deadline.Add(-time.Nanosecond))
	require.Equal(t, before, snapshotProfileTables(t, db, mutationTables))
	for _, attempt := range []time.Time{deadline, deadline.Add(24 * time.Hour)} {
		for _, dryRun := range []bool{false, true} {
			result, err := Apply(t.Context(), db, client, profileOrg, ActiveTrial, dryRun, attempt, profileRecheck)
			require.ErrorContains(t, err, "apply enterprise then active-trial")
			require.False(t, result.Committed)
			require.Equal(t, before, snapshotProfileTables(t, db, mutationTables))
		}
	}
	// Stored deadlines and demotions take precedence over a future marker deadline.
	for _, demoted := range []bool{false, true} {
		ends := now.Add(-time.Hour)
		var demotedAt *time.Time
		if demoted {
			ends = now.Add(time.Hour)
			demotedAt = &now
		}
		require.NoError(t, fixturerepo.New(db).SetTrialExpiration(t.Context(), fixturerepo.SetTrialExpirationParams{OrganizationID: profileOrg, EndsAt: conv.ToPGTimestamptz(ends), DemotedAt: conv.PtrToPGTimestamptz(demotedAt)}))
		before = snapshotProfileTables(t, db, mutationTables)
		for _, dryRun := range []bool{false, true} {
			result, err := Apply(t.Context(), db, client, profileOrg, ActiveTrial, dryRun, now, profileRecheck)
			require.ErrorContains(t, err, "apply enterprise then active-trial")
			require.False(t, result.Committed)
			require.Equal(t, before, snapshotProfileTables(t, db, mutationTables))
		}
	}
	applyProfile(t, db, client, Enterprise, deadline)
	restarted := applyProfile(t, db, client, ActiveTrial, deadline)
	require.True(t, deadline.Equal(restarted.Anchor))
	assertProfileState(t, db, ActiveTrial, deadline)
}

func TestProfilesTrialDatesIdempotentAcrossDst(t *testing.T) {
	clone := newProfilePostgres(t)
	location, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	db, client := newProfileFixture(t, clone, location)
	beforeDST := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	applyProfile(t, db, client, ActiveTrial, beforeDST)
	before := snapshotProfileTables(t, db, mutationTables)
	result := applyProfile(t, db, client, ActiveTrial, beforeDST.Add(24*time.Hour))
	require.True(t, beforeDST.Equal(result.Anchor))
	after := snapshotProfileTables(t, db, mutationTables)
	require.Equal(t, before["trials"], after["trials"], "reloaded anchors must retain UTC date arithmetic across DST")
	require.Equal(t, before, after)
}

func TestProfilesPaidConversionKeepsOriginalConversionDate(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	db, client := newProfileFixture(t, clone)
	applyProfile(t, db, client, ActiveTrial, now)
	applyProfile(t, db, client, Enterprise, now.Add(48*time.Hour))
	dates, err := fixturerepo.New(db).GetTrialDates(t.Context(), profileOrg)
	require.NoError(t, err)
	require.True(t, dates.ConvertedAt.Time.Equal(anchor.Add(48*time.Hour)))
	require.True(t, dates.EndsAt.Time.Equal(anchor.AddDate(0, 0, 14)))
	before := snapshotProfileTables(t, db, []string{"trials"})
	applyProfile(t, db, client, PAYG, now.Add(96*time.Hour))
	require.Equal(t, before, snapshotProfileTables(t, db, []string{"trials"}))
}

func TestProfilesTransitions(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	profiles := []Profile{Enterprise, PAYG, ActiveTrial, ExpiredTrial}
	for _, from := range profiles {
		for _, to := range profiles {

			db, client := newProfileFixture(t, clone)
			preserved := snapshotProfileTables(t, db, preservationTables)
			first := applyProfile(t, db, client, from, now)
			require.True(t, anchor.Equal(first.Anchor))
			next := applyProfile(t, db, client, to, now.Add(48*time.Hour))
			wantAnchor := anchor.Add(48 * time.Hour)
			if from == to {
				wantAnchor = anchor
			}
			require.True(t, wantAnchor.Equal(next.Anchor))
			assertProfileState(t, db, to, wantAnchor)

			// Reapplying on a later date must not churn trial timestamps,
			// feature rows, metadata, or marker.
			beforeRepeat := snapshotProfileTables(t, db, mutationTables)
			repeated := applyProfile(t, db, client, to, now.Add(96*time.Hour))
			require.True(t, wantAnchor.Equal(repeated.Anchor))
			require.Equal(t, beforeRepeat, snapshotProfileTables(t, db, mutationTables))
			require.Equal(t, preserved, snapshotProfileTables(t, db, preservationTables))

		}
	}
}

func TestProfilesPAYGRuntimeAdminDisable(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	for _, trialExists := range []bool{false, true} {

		db, client := newProfileFixture(t, clone)
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			require.NoError(t, fixturerepo.New(db).CreateDisabledFeature(t.Context(), fixturerepo.CreateDisabledFeatureParams{OrganizationID: profileOrg, FeatureName: string(feature)}))
		}
		if trialExists {
			require.NoError(t, fixturerepo.New(db).CreateTrial(t.Context(), fixturerepo.CreateTrialParams{OrganizationID: profileOrg, EndsAt: conv.ToPGTimestamptz(anchor.Add(-24 * time.Hour))}))
		}
		applyProfile(t, db, client, PAYG, now)
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			require.Equal(t, trialExists, profileFeatureEnabled(t, db, string(feature)))
		}
		before := snapshotProfileTables(t, db, mutationTables)
		applyProfile(t, db, client, PAYG, now.Add(48*time.Hour))
		require.Equal(t, before, snapshotProfileTables(t, db, mutationTables))

	}
}

func TestProfilesPAYGPreservesAdminChoices(t *testing.T) {
	clone := newProfilePostgres(t)
	now := time.Date(2026, 3, 10, 23, 30, 15, 123, time.FixedZone("west", -7*60*60))
	anchor := now.UTC().Truncate(24 * time.Hour)
	for _, trialExists := range []bool{false, true} {

		db, client := newProfileFixture(t, clone)
		disabled := string(productfeatures.EnterpriseAccessBundle[0])
		missing := string(productfeatures.EnterpriseAccessBundle[1])
		enabled := string(productfeatures.EnterpriseAccessBundle[2])
		require.NoError(t, fixturerepo.New(db).CreateAdminFeatures(t.Context(), fixturerepo.CreateAdminFeaturesParams{OrganizationID: profileOrg, DisabledFeature: disabled, EnabledFeature: enabled}))
		if trialExists {
			require.NoError(t, fixturerepo.New(db).CreateTrial(t.Context(), fixturerepo.CreateTrialParams{OrganizationID: profileOrg, EndsAt: conv.ToPGTimestamptz(anchor.Add(-24 * time.Hour))}))
		}
		applyProfile(t, db, client, PAYG, now)
		require.False(t, profileFeatureEnabled(t, db, disabled))
		require.True(t, profileFeatureEnabled(t, db, enabled))
		require.Equal(t, !trialExists, profileFeatureEnabled(t, db, missing))
		assertProfileState(t, db, PAYG, anchor)

	}
}

const profileOrg = "org_profile_fixture"

var preservationTables = []string{"projects", "api_keys", "users", "organization_user_relationships", "organization_roles", "organization_role_assignments", "principal_grants"}
var mutationTables = []string{"organization_metadata", "trials", "organization_features", "gram_local.account_profiles"}

func profileRecheck(context.Context, pgx.Tx) error { return nil }

func applyProfile(t *testing.T, db *pgxpool.Pool, client *productfeatures.Client, profile Profile, now time.Time) Result {
	t.Helper()
	result, err := Apply(t.Context(), db, client, profileOrg, profile, false, now, profileRecheck)
	require.NoError(t, err)
	require.True(t, result.Committed)
	require.Empty(t, result.CacheRefreshError)
	return result
}

func snapshotProfileTables(t *testing.T, db *pgxpool.Pool, tables []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(tables))
	for _, table := range tables {
		var data string
		var err error
		switch table {
		case "projects":
			data, err = fixturerepo.New(db).SnapshotProjects(t.Context())
		case "api_keys":
			data, err = fixturerepo.New(db).SnapshotApiKeys(t.Context())
		case "users":
			data, err = fixturerepo.New(db).SnapshotUsers(t.Context())
		case "organization_user_relationships":
			data, err = fixturerepo.New(db).SnapshotOrganizationUserRelationships(t.Context())
		case "organization_roles":
			data, err = fixturerepo.New(db).SnapshotOrganizationRoles(t.Context())
		case "organization_role_assignments":
			data, err = fixturerepo.New(db).SnapshotOrganizationRoleAssignments(t.Context())
		case "principal_grants":
			data, err = fixturerepo.New(db).SnapshotPrincipalGrants(t.Context())
		case "organization_metadata":
			data, err = fixturerepo.New(db).SnapshotOrganizationMetadata(t.Context())
		case "trials":
			data, err = fixturerepo.New(db).SnapshotTrials(t.Context())
		case "organization_features":
			data, err = fixturerepo.New(db).SnapshotOrganizationFeatures(t.Context())
		case "gram_local.account_profiles":
			data, err = fixturerepo.New(db).SnapshotAccountProfiles(t.Context())
		default:
			t.Fatalf("unsupported snapshot table: %s", table)
		}
		require.NoError(t, err)
		out[table] = data
	}
	return out
}

func profileFeatureEnabled(t *testing.T, db *pgxpool.Pool, name string) bool {
	t.Helper()
	enabled, err := fixturerepo.New(db).FeatureEnabled(t.Context(), fixturerepo.FeatureEnabledParams{OrganizationID: profileOrg, FeatureName: name})
	require.NoError(t, err)
	return enabled
}

func assertProfileState(t *testing.T, db *pgxpool.Pool, profile Profile, anchor time.Time) {
	t.Helper()
	state, err := Status(t.Context(), db, profileOrg)
	require.NoError(t, err)
	require.Equal(t, profile, state.Profile)
	require.True(t, state.Anchor.Equal(anchor))
	wantTier := string(profile)
	if profile == ActiveTrial {
		wantTier = "enterprise"
	}
	if profile == ExpiredTrial {
		wantTier = "free"
	}
	require.Equal(t, wantTier, state.AccountType)
	require.Equal(t, profile != ExpiredTrial, state.Whitelisted)
	require.True(t, profileFeatureEnabled(t, db, string(productfeatures.FeatureHooksFailOpen)), "unmanaged feature must survive")
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		require.Equal(t, profile != ExpiredTrial, profileFeatureEnabled(t, db, string(feature)))
	}
	if profile == Enterprise || profile == ActiveTrial {
		for _, feature := range productfeatures.EnterpriseAccessBundle {
			require.True(t, profileFeatureEnabled(t, db, string(feature)), "enterprise bundle feature %s", feature)
		}
	}
	var trial *struct {
		Tier        string     `json:"tier"`
		EndsAt      time.Time  `json:"ends_at"`
		ConvertedAt *time.Time `json:"converted_at"`
		DemotedAt   *time.Time `json:"demoted_at"`
	}
	require.NoError(t, json.Unmarshal(state.Trial, &trial))
	if profile == ActiveTrial || profile == ExpiredTrial {
		require.NotNil(t, trial)
		require.Equal(t, "enterprise", trial.Tier)
		require.Nil(t, trial.ConvertedAt)
		if profile == ActiveTrial {
			require.True(t, trial.EndsAt.Equal(anchor.AddDate(0, 0, 14)))
			require.Nil(t, trial.DemotedAt)
		} else {
			require.True(t, trial.EndsAt.Equal(anchor.AddDate(0, 0, -1)))
			require.NotNil(t, trial.DemotedAt)
			require.True(t, trial.DemotedAt.Equal(anchor))
		}
	} else if trial != nil {
		require.NotNil(t, trial.ConvertedAt, "paid profiles must neutralize pending trial demotion")
	}
	unrelated, err := Status(t.Context(), db, "org_unrelated")
	require.NoError(t, err)
	require.Equal(t, "free", unrelated.AccountType)
}

// A deadline passing after BEGIN must be evaluated using the current clock,
// not the transaction timestamp, once expiry has acquired its row locks.
func TestProfilesTrialDueAfterTransactionStart(t *testing.T) {
	clone := newProfilePostgres(t)
	db, client := newProfileFixture(t, clone)
	applyProfile(t, db, client, ActiveTrial, time.Now())
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(t.Context())) })
	q := repo.New(tx)
	require.NoError(t, q.LockAccountMetadata(t.Context(), profileOrg))
	require.NoError(t, fixturerepo.New(db).SetTrialExpiration(t.Context(), fixturerepo.SetTrialExpirationParams{
		OrganizationID: profileOrg,
		EndsAt:         conv.ToPGTimestamptz(time.Now()),
		DemotedAt:      pgtype.Timestamptz{},
	}))
	require.NoError(t, q.LockAccountTrial(t.Context(), profileOrg))
	due, err := q.AccountTrialDue(t.Context(), profileOrg)
	require.NoError(t, err)
	require.True(t, due.Valid)
	require.True(t, due.Bool)
}

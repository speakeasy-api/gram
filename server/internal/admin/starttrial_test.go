package admin

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func seedOrgReadyForTrial(t *testing.T, ctx context.Context, conn *pgxpool.Pool, f orgFixture) {
	t.Helper()

	seedOrg(t, ctx, conn, f)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, authz.SeedSystemRoleGrantsTx(ctx, tx, f.id))
	require.NoError(t, tx.Commit(ctx))
}

func TestStartTrial_GrantsATrialToAnOrganizationThatNeverTrialled(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{
		id:          "org_start_none",
		name:        "org_start_none Name",
		slug:        "org_start_none-slug",
		accountType: "free",
		whitelisted: false,
	})

	res, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: "org_start_none", Days: 14})
	require.NoError(t, err)
	require.Equal(t, "org_start_none", res.ID)
	require.Equal(t, "enterprise", res.AccountType)
	require.True(t, res.Whitelisted)
	require.NotNil(t, res.TrialState)
	require.Equal(t, "running", *res.TrialState)
	require.NotNil(t, res.TrialEndsAt)

	trial, err := trialsRepo.New(conn).GetTrial(ctx, "org_start_none")
	require.NoError(t, err)
	require.Equal(t, "enterprise", trial.Tier)
	require.False(t, trial.DemotedAt.Valid)
	require.False(t, trial.ConvertedAt.Valid)
	require.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), trial.EndsAt.Time, time.Minute)

	state := readOrgState(t, ctx, conn, "org_start_none")
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)

	enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, "org_start_none", productfeatures.FeatureSSO)
	require.NoError(t, err)
	require.True(t, enabled, "a started trial must receive the enterprise trial bundle")
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		runtimeEnabled, featureErr := svc.productFeatures.IsFeatureEnabled(ctx, "org_start_none", feature)
		require.NoError(t, featureErr)
		require.Truef(t, runtimeEnabled, "a started trial must enable %s", feature)
	}
}

func TestStartTrial_RestartsAnExpiredTrialFromNow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_start_expired"
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: "enterprise", whitelisted: true})
	// Far enough in the past that adding fourteen days to the old end would
	// still leave a trial the next sweep demotes.
	oldEnd := time.Now().UTC().Add(-100 * 24 * time.Hour)
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: oldEnd})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	after := readTrial(t, ctx, conn, orgID)
	require.False(t, after.DemotedAt.Valid)
	require.False(t, after.ConvertedAt.Valid)
	require.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), after.EndsAt.Time, time.Minute)
	require.True(t, after.EndsAt.Time.After(time.Now().UTC()), "a restarted trial must end in the future")

	expired, err := trialsRepo.New(conn).ListExpiredTrials(ctx)
	require.NoError(t, err)
	require.NotContains(t, expired, orgID)

	detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: orgID})
	require.NoError(t, err)
	require.Equal(t, "running", *detail.TrialState)
}

func TestStartTrial_RevivesDisabledKeysOnAnExpiredTrial(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_start_expired_keys"
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise"})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(-24 * time.Hour)})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	require.Len(t, provisioner.revivals, len(openrouter.AllKeyTypes))
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).Disabled)
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal).Disabled)
}

func TestStartTrial_DoesNotRestartLoopsSequence(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: "org_start_loops", name: "org_start_loops", slug: "org_start_loops"})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: "org_start_loops", Days: 14})
	require.NoError(t, err)
	require.Empty(t, notifier.started)
	require.Empty(t, notifier.inactive)
}

func TestStartTrial_RejectsTrialsThatAreNotStartable(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	now := time.Now().UTC()
	convertedAt := now.Add(-10 * 24 * time.Hour)
	demotedAt := now.Add(-9 * 24 * time.Hour)

	cases := []struct {
		name   string
		orgID  string
		trial  *trialFixture
		want   oops.Code
		noRow  bool
	}{
		{name: "running", orgID: "org_start_reject_running", trial: &trialFixture{endsAt: now.Add(10 * 24 * time.Hour)}},
		{name: "demoted", orgID: "org_start_reject_demoted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), demotedAt: &demotedAt}},
		{name: "converted", orgID: "org_start_reject_converted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), convertedAt: &convertedAt}},
		{name: "missing organization", orgID: "org_start_reject_missing", noRow: true, want: oops.CodeNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !tc.noRow {
				seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: tc.orgID, name: tc.orgID, slug: tc.orgID})
				if tc.trial != nil {
					f := *tc.trial
					f.orgID = tc.orgID
					seedTrial(t, ctx, conn, f)
				}
			}

			var before *trialsRepo.Trial
			if tc.trial != nil {
				row := readTrial(t, ctx, conn, tc.orgID)
				before = &row
			}

			_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: tc.orgID, Days: 14})
			want := tc.want
			if want == "" {
				want = oops.CodeConflict
			}
			requireOopsCode(t, err, want)

			if before != nil {
				after := readTrial(t, ctx, conn, tc.orgID)
				require.Equal(t, before.EndsAt.Time, after.EndsAt.Time, "a rejected start must leave ends_at where it was")
				require.Equal(t, before.DemotedAt.Valid, after.DemotedAt.Valid)
				require.Equal(t, before.ConvertedAt.Valid, after.ConvertedAt.Valid)
			}
		})
	}
}

func TestStartTrial_WritesAnAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{
		id:   "org_start_audit",
		name: "org_start_audit Name",
		slug: "org_start_audit-slug",
	})

	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-start-audit",
		Email:       "operator@example.test",
		OIDCSubject: "oidc-subject-start-audit",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialStarted)
	require.NoError(t, err)

	_, err = svc.StartTrial(ctx, &gen.StartTrialPayload{ID: "org_start_audit", Days: 14})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialStarted)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialStarted)
	require.NoError(t, err)
	require.Equal(t, "organization", entry.SubjectType)
	require.Equal(t, "org_start_audit Name", entry.SubjectDisplay)
	require.Equal(t, "org_start_audit-slug", entry.SubjectSlug)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdmin), *entry.ActingSurface)

	var metadata struct {
		AccountType string    `json:"account_type"`
		TrialEndsAt time.Time `json:"trial_ends_at"`
	}
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, "enterprise", metadata.AccountType)
	require.WithinDuration(t, readTrial(t, ctx, conn, "org_start_audit").EndsAt.Time, metadata.TrialEndsAt, 0)

	_, err = audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: "org_start_audit",
		EventType:      string(events.OrganizationEnterpriseTrialV1.EventType()),
	})
	require.NoError(t, err, "a start must enqueue an outbox entry on the enterprise trial event")
}

func TestStartTrial_DayCountBounds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	cases := []struct {
		name    string
		days    int
		wantErr bool
	}{
		{name: "large negative", days: -365, wantErr: true},
		{name: "minus one", days: -1, wantErr: true},
		{name: "zero", days: 0, wantErr: true},
		{name: "minimum", days: constants.MinTrialStartDays},
		{name: "maximum", days: constants.MaxTrialStartDays},
		{name: "one past the maximum", days: constants.MaxTrialStartDays + 1, wantErr: true},
		{name: "far past the maximum", days: 100000, wantErr: true},
		{name: "int32 overflow to a negative", days: math.MaxInt32 + 1, wantErr: true},
		{name: "int32 overflow to a valid day count", days: math.MaxUint32 + 2, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org_start_bound_" + tc.name
			seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID})

			_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: orgID, Days: tc.days})
			if tc.wantErr {
				requireOopsCode(t, err, oops.CodeInvalid)
				_, lookupErr := trialsRepo.New(conn).GetTrial(ctx, orgID)
				require.Error(t, lookupErr, "a rejected day count must not create a trial row")
				require.Equal(t, "free", readOrgState(t, ctx, conn, orgID).GramAccountType)
				return
			}

			require.NoError(t, err)
			after := readTrial(t, ctx, conn, orgID)
			require.WithinDuration(t, time.Now().UTC().Add(time.Duration(tc.days)*24*time.Hour), after.EndsAt.Time, time.Minute)
		})
	}
}

func TestStartTrialRequestBody_DesignBoundsAreEnforced(t *testing.T) {
	t.Parallel()

	id := "org_start_validate"
	minDays := constants.MinTrialStartDays
	maxDays := constants.MaxTrialStartDays

	cases := []struct {
		name    string
		id      *string
		days    *int
		wantErr bool
	}{
		{name: "at the minimum", id: &id, days: &minDays},
		{name: "at the maximum", id: &id, days: &maxDays},
		{name: "below the minimum", id: &id, days: new(minDays - 1), wantErr: true},
		{name: "above the maximum", id: &id, days: new(maxDays + 1), wantErr: true},
		{name: "negative", id: &id, days: new(-1), wantErr: true},
		{name: "empty id", id: new(""), days: &minDays, wantErr: true},
		{name: "missing id", id: nil, days: &minDays, wantErr: true},
		{name: "missing days", id: &id, days: nil, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := srv.ValidateStartTrialRequestBody(&srv.StartTrialRequestBody{ID: tc.id, Days: tc.days})
			if tc.wantErr {
				require.Error(t, err, "the request decoder must reject this before the handler runs")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestStartTrial_IsIdempotentAcrossARetry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: "org_start_twice", name: "org_start_twice", slug: "org_start_twice"})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: "org_start_twice", Days: 14})
	require.NoError(t, err)
	first := readTrial(t, ctx, conn, "org_start_twice")

	_, err = svc.StartTrial(ctx, &gen.StartTrialPayload{ID: "org_start_twice", Days: 365})
	requireOopsCode(t, err, oops.CodeConflict)

	second := readTrial(t, ctx, conn, "org_start_twice")
	require.Equal(t, first.EndsAt.Time, second.EndsAt.Time, "a second start must not move a trial that is already running")
	require.Equal(t, first.UpdatedAt.Time, second.UpdatedAt.Time)
}

func TestStartTrial_StartsADisabledOrganizationsTrialWithoutEnablingIt(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_start_disabled"
	disabledAt := time.Now().UTC().Add(-time.Hour)
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, disabledAt: &disabledAt})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	state := readOrgState(t, ctx, conn, orgID)
	require.True(t, state.DisabledAt.Valid, "starting a trial must not enable a disabled organization")
	require.WithinDuration(t, disabledAt, state.DisabledAt.Time, time.Second)
}

func TestStartTrial_TouchesOnlyTheTargetRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: "org_start_target", name: "org_start_target", slug: "org_start_target"})
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: "org_start_neighbour", name: "org_start_neighbour", slug: "org_start_neighbour", accountType: "free", whitelisted: false})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: "org_start_target", Days: 14})
	require.NoError(t, err)

	_, lookupErr := trialsRepo.New(conn).GetTrial(ctx, "org_start_neighbour")
	require.Error(t, lookupErr, "starting must not create a trial on another organization")
	neighbour := readOrgState(t, ctx, conn, "org_start_neighbour")
	require.Equal(t, "free", neighbour.GramAccountType)
	require.False(t, neighbour.Whitelisted)
}

func TestStartTrial_CachesEnabledTrialFeatures(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_start_cache"
	seedOrgReadyForTrial(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID})

	_, err := svc.StartTrial(ctx, &gen.StartTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	q := featurerepo.New(conn)
	for _, feature := range productfeatures.EnterpriseTrialBundle {
		enabled, featureErr := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		require.NoError(t, featureErr)
		require.Truef(t, enabled, "feature %s", feature)
	}
}

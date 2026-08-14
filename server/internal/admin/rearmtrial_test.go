package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// keyRevival is one call the handler made, with what the database looked like
// from outside its transaction then. The "seen" fields read through the pool.
type keyRevival struct {
	orgID   string
	keyType openrouter.KeyType
	limit   *int

	accountTypeSeen string
	demotedSeen     bool
}

// rearmProvisioner stands in for OpenRouter and can be made to fail on one key.
type rearmProvisioner struct {
	conn *pgxpool.Pool

	mu       sync.Mutex
	revivals []keyRevival

	failOn openrouter.KeyType
	// failAfter is how a test reaches the post-commit recap.
	failAfter int
	failWith  error
}

var _ TrialKeyReviver = (*rearmProvisioner)(nil)

func (p *rearmProvisioner) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	accountType, demoted := "", false
	if p.conn != nil {
		accountType, demoted = readRearmState(ctx, p.conn, orgID)
	}

	p.mu.Lock()
	p.revivals = append(p.revivals, keyRevival{
		orgID:           orgID,
		keyType:         keyType,
		limit:           limit,
		accountTypeSeen: accountType,
		demotedSeen:     demoted,
	})
	calls := len(p.revivals)
	p.mu.Unlock()

	if p.failWith != nil && calls > p.failAfter && (p.failOn == "" || p.failOn == keyType) {
		return 0, p.failWith
	}

	// What the real RefreshAPIKeyLimit does locally.
	if p.conn != nil {
		if _, err := orrepo.New(p.conn).UpdateOpenRouterKey(ctx, orrepo.UpdateOpenRouterKeyParams{
			OrganizationID: orgID,
			KeyType:        string(keyType),
			MonthlyCredits: int64(conv.PtrValOr(limit, 0)),
			KeyHash:        "hash-" + orgID + "-" + string(keyType),
			Reinstate:      true,
		}); err != nil {
			return 0, fmt.Errorf("reinstate %s key: %w", keyType, err)
		}
	}

	return conv.PtrValOr(limit, 0), nil
}

// A nil ceiling is not a zero one: nil asks for the policy default.
func (p *rearmProvisioner) revivedLimits() map[openrouter.KeyType]*int {
	p.mu.Lock()
	defer p.mu.Unlock()

	limits := make(map[openrouter.KeyType]*int, len(p.revivals))
	for _, r := range p.revivals {
		limits[r.keyType] = r.limit
	}

	return limits
}

// readRearmState runs inside the handler's own goroutine, so it cannot use the
// require helpers: it answers zero values and lets the caller's assertions fail.
func readRearmState(ctx context.Context, conn *pgxpool.Pool, orgID string) (accountType string, demoted bool) {
	org, err := testrepo.New(conn).GetOrganizationMetadataStateFixture(ctx, orgID)
	if err != nil {
		return "", false
	}

	trial, err := trialsRepo.New(conn).GetTrial(ctx, orgID)
	if err != nil {
		return org.GramAccountType, false
	}

	return org.GramAccountType, trial.DemotedAt.Valid
}

func newTestAdminServiceWithOpenRouter(t *testing.T, provisioner TrialKeyReviver) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	svc.openRouter = provisioner

	return ctx, svc, conn
}

func newRearmService(t *testing.T) (context.Context, *Service, *pgxpool.Pool, *rearmProvisioner) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	provisioner := &rearmProvisioner{conn: conn, mu: sync.Mutex{}, revivals: nil, failOn: "", failAfter: 0, failWith: nil}
	svc.openRouter = provisioner

	return ctx, svc, conn, provisioner
}

type keyFixture struct {
	keyType        openrouter.KeyType
	monthlyCredits int64
	disabled       bool
}

func seedOpenRouterKey(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, f keyFixture) {
	t.Helper()

	keys := orrepo.New(conn)
	_, err := keys.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(f.keyType),
		Key:            conv.ToPGText("sk-test-" + orgID + "-" + string(f.keyType)),
		KeyEncrypted:   conv.ToPGText(""),
		KeyHash:        "hash-" + orgID + "-" + string(f.keyType),
		MonthlyCredits: f.monthlyCredits,
	})
	require.NoError(t, err)

	if f.disabled {
		require.NoError(t, keys.DisableOpenRouterAPIKey(ctx, orrepo.DisableOpenRouterAPIKeyParams{
			OrganizationID: orgID,
			KeyType:        string(f.keyType),
		}))
	}
}

func readOpenRouterKey(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, keyType openrouter.KeyType) orrepo.OpenrouterApiKey {
	t.Helper()

	row, err := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	require.NoError(t, err)

	return row
}

// seedDemotedTrial leaves the organization where the demotion sweeper leaves it.
// The two key ceilings differ so a revival that passed the wrong one is visible.
func seedDemotedTrial(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, tier string) time.Time {
	t.Helper()

	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)

	// id, name and slug all differ, so an assertion on one cannot pass on another.
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: tier, endsAt: endsAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	return endsAt
}

func TestRearmTrial_RestoresTheOrganizationAndRevivesEveryKey(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm", "enterprise")
	beforeTrial := readTrial(t, ctx, conn, "org_rearm")
	beforeOrg := readOrgState(t, ctx, conn, "org_rearm")

	res, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm", Days: 14})
	require.NoError(t, err)
	require.Equal(t, "org_rearm", res.ID)

	require.Len(t, provisioner.revivals, len(openrouter.AllKeyTypes))
	require.Equal(t, map[openrouter.KeyType]*int{
		openrouter.KeyTypeChat:     conv.PtrEmpty(50),
		openrouter.KeyTypeInternal: conv.PtrEmpty(37),
	}, provisioner.revivedLimits(), "every key type the demotion disables must come back up on its own ceiling")
	require.False(t, readOpenRouterKey(t, ctx, conn, "org_rearm", openrouter.KeyTypeChat).Disabled)
	require.False(t, readOpenRouterKey(t, ctx, conn, "org_rearm", openrouter.KeyTypeInternal).Disabled)

	// Demotion cleared both; the signup arming path only ever writes the first.
	state := readOrgState(t, ctx, conn, "org_rearm")
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted, "a re-armed organization must be whitelisted again")

	after := readTrial(t, ctx, conn, "org_rearm")
	require.False(t, after.DemotedAt.Valid, "re-arming must clear demoted_at")
	require.False(t, after.ConvertedAt.Valid)
	require.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), after.EndsAt.Time, time.Minute)

	// The rejection tests assert updated_at holds still; this is the other half.
	require.True(t, after.UpdatedAt.Time.After(beforeTrial.UpdatedAt.Time),
		"a re-arm must stamp the trial's updated_at: was %s, now %s", beforeTrial.UpdatedAt.Time, after.UpdatedAt.Time)
	require.True(t, state.UpdatedAt.Time.After(beforeOrg.UpdatedAt.Time),
		"restoring the organization must stamp its updated_at: was %s, now %s", beforeOrg.UpdatedAt.Time, state.UpdatedAt.Time)

	require.NotNil(t, res.TrialEndsAt)
	detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_rearm"})
	require.NoError(t, err)
	require.Equal(t, "running", *detail.TrialState)
	require.Equal(t, *res.TrialEndsAt, *detail.TrialEndsAt)
	require.Equal(t, "enterprise", detail.AccountType)
}

func TestRearmTrial_MarksTrialStartedInLoops(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier
	seedDemotedTrial(t, ctx, conn, "org_rearm_loops", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_loops", Days: 14})
	require.NoError(t, err)
	require.Equal(t, []string{"org_rearm_loops"}, notifier.started)
}

func TestRearmTrial_TrialStartedFailureDoesNotFailRearm(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	notifier := &fakeTrialNotifier{startedErr: errors.New("loops unavailable")}
	svc.trial = notifier
	seedDemotedTrial(t, ctx, conn, "org_rearm_loops_fail", "enterprise")

	res, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_loops_fail", Days: 14})
	require.NoError(t, err)
	require.Equal(t, "org_rearm_loops_fail", res.ID)
	require.Equal(t, []string{"org_rearm_loops_fail"}, notifier.started)
	require.False(t, readTrial(t, ctx, conn, "org_rearm_loops_fail").DemotedAt.Valid)
}

// MarkTrialDemoted only demotes an already-past ends_at, so a re-arm that
// cleared the stamp and left the date would be re-demoted on the next sweep.
// The trial here ended 100 days ago, so a date computed from it is still past.
func TestRearmTrial_MovesEndsAtIntoTheFuture(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	orgID := "org_rearm_stale"
	longExpired := time.Now().UTC().Add(-100 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-99 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: longExpired, demotedAt: &demotedAt})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 3})
	require.NoError(t, err)

	after := readTrial(t, ctx, conn, orgID)
	require.True(t, after.EndsAt.Time.After(time.Now().UTC()),
		"a re-armed trial must end in the future or the next sweep demotes it again: ends %s", after.EndsAt.Time)
	require.WithinDuration(t, time.Now().UTC().Add(3*24*time.Hour), after.EndsAt.Time, time.Minute,
		"the new end date is counted from now, not added to the old one")

	expired, err := trialsRepo.New(conn).ListExpiredTrials(ctx)
	require.NoError(t, err)
	require.NotContains(t, expired, orgID, "a re-armed trial must not be due for demotion again")

	_, err = svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: orgID, Days: 5})
	require.NoError(t, err, "a re-armed trial must be extendable")
}

// The fake reads through the pool, so while the handler's transaction is open
// the organization must still read free and the trial must still read demoted.
func TestRearmTrial_RevivesKeysBeforeTheRestoreCommits(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_order", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_order", Days: 14})
	require.NoError(t, err)

	require.Len(t, provisioner.revivals, len(openrouter.AllKeyTypes))
	for _, revival := range provisioner.revivals {
		require.Equal(t, "free", revival.accountTypeSeen,
			"the %s key must come back up before the restore commits, or a failure leaves a running trial with dead keys", revival.keyType)
		require.True(t, revival.demotedSeen,
			"the %s key must come back up while the trial still reads demoted", revival.keyType)
	}
}

// The failure lands on the second key type, so the first is already back up.
func TestRearmTrial_KeyRevivalFailureLeavesTheOrganizationDemoted(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	provisioner := &rearmProvisioner{
		conn:      conn,
		mu:        sync.Mutex{},
		revivals:  nil,
		failOn:    openrouter.KeyTypeInternal,
		failAfter: 0,
		failWith:  errors.New("openrouter is down"),
	}
	svc.openRouter = provisioner

	seededEndsAt := seedDemotedTrial(t, ctx, conn, "org_rearm_fail", "enterprise")
	auditsBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)

	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_fail", Days: 14})
	requireOopsCode(t, err, oops.CodeGatewayError)

	after := readTrial(t, ctx, conn, "org_rearm_fail")
	require.True(t, after.DemotedAt.Valid, "a failed re-arm must leave the trial demoted")
	require.WithinDuration(t, seededEndsAt, after.EndsAt.Time, time.Second, "a failed re-arm must not move ends_at")

	state := readOrgState(t, ctx, conn, "org_rearm_fail")
	require.Equal(t, "free", state.GramAccountType, "a failed re-arm must not restore the account type")
	require.False(t, state.Whitelisted)

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter, "a failed re-arm must not claim one happened")

	require.False(t, readOpenRouterKey(t, ctx, conn, "org_rearm_fail", openrouter.KeyTypeChat).Disabled,
		"a revived key must survive the rollback, which is what makes a retry cheap")
	require.True(t, readOpenRouterKey(t, ctx, conn, "org_rearm_fail", openrouter.KeyTypeInternal).Disabled)
}

// Walks the guard's whole eight-row state space. The demoted-and-not-expired
// row is unreachable today, and is here because the query does not test ends_at.
func TestRearmTrial_OnlyADemotedTrialCanBeRearmed(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-96 * time.Hour)
	demotedAt := now.Add(-72 * time.Hour)

	cases := []struct {
		name      string
		orgID     string
		expired   bool
		converted bool
		demoted   bool
		wantRearm bool
	}{
		{name: "demoted and expired", orgID: "org_re_demoted_expired", demoted: true, expired: true, wantRearm: true},
		{name: "demoted", orgID: "org_re_demoted", demoted: true, wantRearm: true},

		// A conversion can land after a demotion, so this is not implied.
		{name: "converted after demotion", orgID: "org_re_both", converted: true, demoted: true},
		{name: "converted after demotion and expired", orgID: "org_re_both_expired", converted: true, demoted: true, expired: true},

		{name: "running", orgID: "org_re_running"},
		{name: "expired not yet demoted", orgID: "org_re_expired", expired: true},
		{name: "converted", orgID: "org_re_converted", converted: true},
		{name: "converted and expired", orgID: "org_re_converted_expired", converted: true, expired: true},
	}

	for _, tc := range cases {
		endsAt := now.Add(10 * 24 * time.Hour)
		if tc.expired {
			endsAt = now.Add(-10 * 24 * time.Hour)
		}
		f := trialFixture{orgID: tc.orgID, endsAt: endsAt}
		if tc.converted {
			f.convertedAt = &convertedAt
		}
		if tc.demoted {
			f.demotedAt = &demotedAt
		}

		seedOrg(t, ctx, conn, orgFixture{id: tc.orgID, name: tc.orgID, slug: tc.orgID, accountType: "free", whitelisted: false})
		seedTrial(t, ctx, conn, f)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := readTrial(t, ctx, conn, tc.orgID)

			_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: tc.orgID, Days: 14})

			after := readTrial(t, ctx, conn, tc.orgID)
			if tc.wantRearm {
				require.NoError(t, err)
				require.False(t, after.DemotedAt.Valid)
				require.True(t, after.EndsAt.Time.After(now))
				return
			}

			requireOopsCode(t, err, oops.CodeConflict)
			require.Equal(t, before.EndsAt.Time, after.EndsAt.Time,
				"a trial that is not demoted must not be re-armed: ends_at was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)
			require.Equal(t, before.DemotedAt.Time, after.DemotedAt.Time)
			require.Equal(t, before.ConvertedAt.Time, after.ConvertedAt.Time)
			require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time, "a rejected re-arm must not touch updated_at")

			// A rejection must not whitelist a converted customer.
			state := readOrgState(t, ctx, conn, tc.orgID)
			require.Equal(t, "free", state.GramAccountType)
			require.False(t, state.Whitelisted)
		})
	}
}

// The state-space test above has no key rows. Hoisting the revival above the
// UPDATE would switch a converted customer's keys on before answering 409.
func TestRearmTrial_ARejectedRearmRevivesNoKeys(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-96 * time.Hour)
	demotedAt := now.Add(-72 * time.Hour)

	// Including the running trial, which is not a state the application reaches.
	seedRejectedRearm := func(orgID string, f trialFixture) {
		t.Helper()

		f.orgID = orgID
		seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: "free", whitelisted: false})
		seedTrial(t, ctx, conn, f)
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})
	}

	seedRejectedRearm("org_rearm_reject_converted", trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), convertedAt: &convertedAt, demotedAt: &demotedAt})
	seedRejectedRearm("org_rearm_reject_running", trialFixture{endsAt: now.Add(10 * 24 * time.Hour)})

	cases := []struct {
		name  string
		orgID string
		want  oops.Code
	}{
		{name: "converted after demotion", orgID: "org_rearm_reject_converted", want: oops.CodeConflict},
		{name: "running trial", orgID: "org_rearm_reject_running", want: oops.CodeConflict},
		{name: "unknown organization", orgID: "org_rearm_reject_missing", want: oops.CodeNotFound},
	}

	// Not subtests: a parallel sibling would still be writing the fake's history.
	for _, tc := range cases {
		_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: tc.orgID, Days: 14})
		requireOopsCode(t, err, tc.want)
		require.Empty(t, provisioner.revivedLimits(),
			"re-arming %s was rejected, so it must not have touched any model provider key", tc.name)
	}

	for _, orgID := range []string{"org_rearm_reject_converted", "org_rearm_reject_running"} {
		for _, keyType := range openrouter.AllKeyTypes {
			require.True(t, readOpenRouterKey(t, ctx, conn, orgID, keyType).Disabled,
				"%s must still have its %s key switched off after a rejected re-arm", orgID, keyType)
		}
	}
}

// A hardcoded 'enterprise' account type passes every other test in this file.
func TestRearmTrial_RestoresTheTierTheTrialGrants(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_tier", "pro")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_tier", Days: 14})
	require.NoError(t, err)

	state := readOrgState(t, ctx, conn, "org_rearm_tier")
	require.Equal(t, "pro", state.GramAccountType,
		"the restored account type must be the trial's tier, not a hardcoded enterprise")
	require.True(t, state.Whitelisted)
}

// This and TestRearmTrial_OrganizationWithNoTrialRow keep the two causes of a
// zero-row update apart: a wrong id reports a missing organization, not a trial.
func TestRearmTrial_UnknownAndMalformedOrganizationIDs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	// Its slug is one of the ids tried below: the lookup must resolve ids only.
	seedDemotedTrial(t, ctx, conn, "org_rearm_bystander", "enterprise")
	before := readTrial(t, ctx, conn, "org_rearm_bystander")

	for _, id := range []string{"org_rearm_missing", "", "org_rearm_bystander-slug", "not a valid id"} {
		_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: id, Days: 14})
		requireOopsCode(t, err, oops.CodeNotFound)
	}

	after := readTrial(t, ctx, conn, "org_rearm_bystander")
	require.True(t, after.DemotedAt.Valid, "an unmatched id must not re-arm anything")
	require.Equal(t, before.EndsAt.Time, after.EndsAt.Time)
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
}

func TestRearmTrial_OrganizationWithNoTrialRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_rearm_no_trial", name: "No Trial", slug: "no-trial-rearm", accountType: "free"})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_no_trial", Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)

	// Re-arm must never be a way to grant a trial; that is the auth flow's job.
	_, err = trialsRepo.New(conn).GetTrial(ctx, "org_rearm_no_trial")
	require.Error(t, err, "a rejected re-arm must not create a trial row")

	state := readOrgState(t, ctx, conn, "org_rearm_no_trial")
	require.Equal(t, "free", state.GramAccountType)
	require.False(t, state.Whitelisted)
}

// DisableAPIKey no-ops on a missing key row but RefreshAPIKeyLimit errors on
// one, so the demotion's unconditional loop would fail this re-arm outright.
func TestRearmTrial_OrganizationWithNoKeysSucceeds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	orgID := "org_rearm_nokeys"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})

	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err, "an organization with no key of a type must still be re-armable")

	//nolint:exhaustive // the absent key type is the assertion: it must not appear
	require.Equal(t, map[openrouter.KeyType]*int{openrouter.KeyTypeChat: conv.PtrEmpty(50)}, provisioner.revivedLimits(),
		"a key type the organization has no row for must not be refreshed")

	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
}

// The sibling test omits the last of AllKeyTypes, so an implementation that
// stopped at the first missing row would still pass it. Here it is the first.
func TestRearmTrial_OrganizationWithOnlyAnInternalKeySucceeds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	orgID := "org_rearm_internal_only"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	//nolint:exhaustive // the absent key type is the assertion: it must not appear
	require.Equal(t, map[openrouter.KeyType]*int{openrouter.KeyTypeInternal: conv.PtrEmpty(37)}, provisioner.revivedLimits(),
		"a key that follows an absent one must still come back up")
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal).Disabled)
}

// A live key needs no round trip, which is what makes retrying a re-arm cheap.
func TestRearmTrial_AlreadyEnabledKeyIsLeftAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	orgID := "org_rearm_partial"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: false})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	//nolint:exhaustive // the omitted key type is the assertion: it was already live
	require.Equal(t, map[openrouter.KeyType]*int{openrouter.KeyTypeInternal: conv.PtrEmpty(37)}, provisioner.revivedLimits(),
		"an already-enabled key needs no upstream round trip")
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).Disabled)
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal).Disabled)
}

// A key minted before the ceiling column existed records none, so its revival
// resolves one from the pool: pre-commit that reads a free organization and
// answers the free-tier allowance. Nothing else corrects it on a schedule.
func TestRearmTrial_ZeroCeilingKeyIsRecappedAfterTheRestoreCommits(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	orgID := "org_rearm_zero_ceiling"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 0, disabled: true})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	var chat, internal []keyRevival
	for _, r := range provisioner.revivals {
		if r.keyType == openrouter.KeyTypeChat {
			chat = append(chat, r)
			continue
		}
		internal = append(internal, r)
	}

	// A second ask for this row would overwrite a ceiling raised by hand.
	require.Len(t, internal, 1, "a key with a recorded ceiling must not be asked twice")
	require.Equal(t, conv.PtrEmpty(37), internal[0].limit)

	require.Len(t, chat, 2, "a key with no recorded ceiling must be asked again after the commit")
	require.Nil(t, chat[0].limit, "the first ask has no ceiling to pass")
	require.Equal(t, "free", chat[0].accountTypeSeen, "the first ask runs before the restore commits")
	require.True(t, chat[0].demotedSeen)

	require.Nil(t, chat[1].limit, "the second ask resolves the ceiling rather than passing the stale one")
	require.Equal(t, "enterprise", chat[1].accountTypeSeen, "the second ask must see the committed restore, or it resolves the same free-tier ceiling again")
	require.False(t, chat[1].demotedSeen, "the second ask must see the trial running, or defaultLimitForOrg misses it")

	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).Disabled)
}

// The re-arm is already committed, so an error here would misreport it.
func TestRearmTrial_RecapFailureStillReportsSuccess(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	orgID := "org_rearm_recap_fails"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 0, disabled: true})

	// Failing every call would abort at the revival and never reach the recap.
	provisioner.failAfter = 1
	provisioner.failWith = errors.New("openrouter is down")

	res, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err, "a failed recap must not report the committed re-arm as failed")
	require.Equal(t, orgID, res.ID)

	// Without this the test passes on an implementation that never recaps.
	require.Len(t, provisioner.revivals, 2, "the recap must have been attempted")
	failed := provisioner.revivals[1]
	require.Nil(t, failed.limit)
	require.Equal(t, "enterprise", failed.accountTypeSeen, "the swallowed failure must be the post-commit ask")

	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
	require.False(t, readTrial(t, ctx, conn, orgID).DemotedAt.Valid)
}

func TestRearmTrial_WritesAnAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_audit", "enterprise")

	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-rearm-audit",
		Email:       "operator@example.test",
		OIDCSubject: "oidc-subject-rearm-audit",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)

	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_audit", Days: 14})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, "organization", entry.SubjectType)

	// id, name and slug all differ in the fixture, so these pin the right one.
	require.Equal(t, "org_rearm_audit Name", entry.SubjectDisplay)
	require.Equal(t, "org_rearm_audit-slug", entry.SubjectSlug)
	require.NotNil(t, entry.ActorDisplayName, "the entry must name who re-armed the trial")
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *entry.ActorDisplayName)

	// Comparing this with the demotion's entry shows whether the tier came back.
	var metadata struct {
		AccountType string    `json:"account_type"`
		TrialEndsAt time.Time `json:"trial_ends_at"`
	}
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, "enterprise", metadata.AccountType)

	// The date the database wrote, not one recomputed from a second clock.
	require.WithinDuration(t, readTrial(t, ctx, conn, "org_rearm_audit").EndsAt.Time, metadata.TrialEndsAt, 0)

	// The trial lifecycle event; any other would deliver this to nobody.
	_, err = audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: "org_rearm_audit",
		EventType:      string(events.OrganizationEnterpriseTrialV1.EventType()),
	})
	require.NoError(t, err, "a re-arm must enqueue an outbox entry on the enterprise trial event")
}

func TestRearmTrial_AuditEntryNamesTheTeamAndNotTheOperator(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_actor", "enterprise")

	const operatorEmail = "operator@example.test"
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-rearm-actor",
		Email:       operatorEmail,
		OIDCSubject: "oidc-subject-rearm-actor",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_actor", Days: 14})
	require.NoError(t, err)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)

	// The customer reads this feed, so a Speakeasy action carries the collective
	// label. The read-side mask cannot reach this entry: it matches an actor id
	// against a Gram user, and an admin session has an OIDC subject instead.
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *entry.ActorDisplayName)

	// Without this, an entry naming nobody at all would satisfy the one above.
	require.Equal(t, "oidc-subject-rearm-actor", entry.ActorID,
		"the entry must still record which operator acted, in the field the customer's feed does not render")
	require.Equal(t, "user", entry.ActorType)

	// The subject is opaque, so it is not the email in another shape.
	for name, field := range map[string]string{
		"actor display name": conv.PtrValOr(entry.ActorDisplayName, ""),
		"actor id":           entry.ActorID,
		"subject display":    entry.SubjectDisplay,
		"subject slug":       entry.SubjectSlug,
		"metadata":           string(entry.Metadata),
		"before snapshot":    string(entry.BeforeSnapshot),
		"after snapshot":     string(entry.AfterSnapshot),
	} {
		require.NotContains(t, field, operatorEmail, "the operator's email must not reach the customer's audit feed through the %s", name)
	}
}

func TestRearmTrial_DayCountBounds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	cases := []struct {
		name    string
		days    int
		wantErr bool
	}{
		// A zero or negative count would re-arm a trial that is already over.
		{name: "large negative", days: -365, wantErr: true},
		{name: "minus one", days: -1, wantErr: true},
		{name: "zero", days: 0, wantErr: true},

		{name: "minimum", days: constants.MinTrialRearmDays},
		{name: "maximum", days: constants.MaxTrialRearmDays},

		{name: "one past the maximum", days: constants.MaxTrialRearmDays + 1, wantErr: true},
		{name: "far past the maximum", days: 100000, wantErr: true},

		// 1<<32 + 1 narrows to exactly 1, so a handler that narrowed before
		// checking would accept it and re-arm for a day.
		{name: "int32 overflow to a negative", days: math.MaxInt32 + 1, wantErr: true},
		{name: "int32 overflow to a valid day count", days: math.MaxUint32 + 2, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org_rearm_bound_" + tc.name
			seededEndsAt := seedDemotedTrial(t, ctx, conn, orgID, "enterprise")

			_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: tc.days})

			after := readTrial(t, ctx, conn, orgID)
			if tc.wantErr {
				requireOopsCode(t, err, oops.CodeInvalid)
				require.True(t, after.DemotedAt.Valid, "a rejected day count must leave the trial demoted")
				require.WithinDuration(t, seededEndsAt, after.EndsAt.Time, time.Second)
				require.Equal(t, "free", readOrgState(t, ctx, conn, orgID).GramAccountType)
				return
			}

			require.NoError(t, err)
			require.WithinDuration(t, time.Now().UTC().Add(time.Duration(tc.days)*24*time.Hour), after.EndsAt.Time, time.Minute)
		})
	}
}

// The other copy of the bounds. Every other test calls svc.RearmTrial directly,
// so deleting Minimum, Maximum or MinLength(1) leaves all of them green.
func TestRearmTrialRequestBody_DesignBoundsAreEnforced(t *testing.T) {
	t.Parallel()

	id := "org_rearm_validate"
	minDays := constants.MinTrialRearmDays
	maxDays := constants.MaxTrialRearmDays

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

			err := srv.ValidateRearmTrialRequestBody(&srv.RearmTrialRequestBody{ID: tc.id, Days: tc.days})
			if tc.wantErr {
				require.Error(t, err, "the request decoder must reject this before the handler runs")
				return
			}
			require.NoError(t, err)
		})
	}
}

// Below 500 on purpose: the admin app trusts a response body only below 500,
// and this one names the deployment setting the operator has to fix.
func TestRearmTrial_WithoutOpenRouterConfiguration(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminServiceWithOpenRouter(t, TrialKeysUnavailable{})
	seedDemotedTrial(t, ctx, conn, "org_rearm_unconfigured", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_unconfigured", Days: 14})
	requireOopsCode(t, err, oops.CodeInvalid)

	require.True(t, readTrial(t, ctx, conn, "org_rearm_unconfigured").DemotedAt.Valid,
		"a re-arm that could not revive the keys must leave the trial demoted")
	require.Equal(t, "free", readOrgState(t, ctx, conn, "org_rearm_unconfigured").GramAccountType)
}

func TestRearmTrial_TouchesOnlyTheTargetRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	seedDemotedTrial(t, ctx, conn, "org_rearm_target", "enterprise")
	seedDemotedTrial(t, ctx, conn, "org_rearm_neighbour", "enterprise")
	neighbourBefore := readTrial(t, ctx, conn, "org_rearm_neighbour")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_target", Days: 14})
	require.NoError(t, err)

	neighbourAfter := readTrial(t, ctx, conn, "org_rearm_neighbour")
	require.True(t, neighbourAfter.DemotedAt.Valid, "re-arming must not spill onto other trials")
	require.Equal(t, neighbourBefore.EndsAt.Time, neighbourAfter.EndsAt.Time)
	require.Equal(t, neighbourBefore.UpdatedAt.Time, neighbourAfter.UpdatedAt.Time)

	neighbourState := readOrgState(t, ctx, conn, "org_rearm_neighbour")
	require.Equal(t, "free", neighbourState.GramAccountType)
	require.False(t, neighbourState.Whitelisted)

	for _, revival := range provisioner.revivals {
		require.Equal(t, "org_rearm_target", revival.orgID, "only the target organization's keys may be revived")
	}
	require.True(t, readOpenRouterKey(t, ctx, conn, "org_rearm_neighbour", openrouter.KeyTypeChat).Disabled)
}

// A half-failed re-arm ends with the operator pressing the button again. The
// second call must be a conflict, or re-arm becomes an unbounded extend.
func TestRearmTrial_IsIdempotentAcrossARetry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_twice", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_twice", Days: 14})
	require.NoError(t, err)
	first := readTrial(t, ctx, conn, "org_rearm_twice")

	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_twice", Days: 365})
	requireOopsCode(t, err, oops.CodeConflict)

	second := readTrial(t, ctx, conn, "org_rearm_twice")
	require.Equal(t, first.EndsAt.Time, second.EndsAt.Time,
		"a second re-arm must not move a trial that is already running")
	require.Equal(t, first.UpdatedAt.Time, second.UpdatedAt.Time)
}

func TestRearmTrial_RestoresADisabledOrganizationsTrialWithoutEnablingIt(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	// Disabled and trial state are independent axes, as they are for extend.
	orgID := "org_rearm_disabled"
	disabledAt := time.Now().UTC().Add(-time.Hour)
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", disabledAt: &disabledAt})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
	require.True(t, state.DisabledAt.Valid, "re-arming a trial must not enable a disabled organization")
}

// A sequence, not concurrency: neither order may leave the organization half
// restored.
func TestRearmTrial_SurvivesTheSweeperReachingTheOrganizationFirst(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_sweep", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_sweep", Days: 14})
	require.NoError(t, err)

	// The sweeper's own selection query, run after the re-arm committed.
	expired, err := trialsRepo.New(conn).ListExpiredTrials(ctx)
	require.NoError(t, err)
	require.NotContains(t, expired, "org_rearm_sweep")

	// And its write, which must find nothing to do.
	_, err = trialsRepo.New(conn).MarkTrialDemoted(ctx, "org_rearm_sweep")
	require.Error(t, err, "a sweep arriving after a re-arm must demote nothing")

	require.True(t, readOrgState(t, ctx, conn, "org_rearm_sweep").Whitelisted)
}

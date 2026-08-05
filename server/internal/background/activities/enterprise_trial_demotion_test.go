package activities_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/enterprisetrials/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// trialProvisioner records which of an organization's keys were locked down and
// can be made to fail, which is the only openrouter.Provisioner behaviour the
// sweeper depends on.
type trialProvisioner struct {
	mu       sync.Mutex
	disabled []string
	failWith error
}

var _ openrouter.Provisioner = (*trialProvisioner)(nil)

func (p *trialProvisioner) DisableAPIKey(_ context.Context, orgID string, keyType openrouter.KeyType) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failWith != nil {
		return p.failWith
	}
	p.disabled = append(p.disabled, orgID+":"+string(keyType))

	return nil
}

func (p *trialProvisioner) disabledKeys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.disabled...)
}

func (p *trialProvisioner) ProvisionAPIKey(context.Context, string, openrouter.KeyType) (string, error) {
	return "", errors.New("not implemented")
}

func (p *trialProvisioner) RefreshAPIKeyLimit(context.Context, string, openrouter.KeyType, *int) (int, error) {
	return 0, errors.New("not implemented")
}

func (p *trialProvisioner) GetCreditsUsed(context.Context, string, openrouter.KeyType) (float64, int, error) {
	return 0, 0, errors.New("not implemented")
}

func (p *trialProvisioner) GetKeyUsage(context.Context, string) (float64, *int64, error) {
	return 0, nil, errors.New("not implemented")
}

func (p *trialProvisioner) ReconcileMonthlyCredits(_ context.Context, _ string, _ openrouter.KeyType, currentLimit int64, _ *int64) (int64, error) {
	return currentLimit, nil
}

func (p *trialProvisioner) GetModelUsage(context.Context, string, string, openrouter.KeyType) (*openrouter.ModelUsage, error) {
	return nil, errors.New("not implemented")
}

type trialTestInstance struct {
	conn        *pgxpool.Pool
	trials      *trialsrepo.Queries
	orgs        *orgrepo.Queries
	provisioner *trialProvisioner
	activity    *activities.DemoteExpiredEnterpriseTrials
}

func newTrialTestInstance(t *testing.T) (context.Context, *trialTestInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "enterprisetrialdemotion")
	require.NoError(t, err)

	provisioner := &trialProvisioner{mu: sync.Mutex{}, disabled: nil, failWith: nil}

	return ctx, &trialTestInstance{
		conn:        conn,
		trials:      trialsrepo.New(conn),
		orgs:        orgrepo.New(conn),
		provisioner: provisioner,
		activity: activities.NewDemoteExpiredEnterpriseTrials(
			testenv.NewLogger(t),
			conn,
			provisioner,
			audit.NewLogger(),
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

	_, err = ti.trials.CreateEnterpriseTrial(ctx, trialsrepo.CreateEnterpriseTrialParams{
		OrganizationID: orgID,
		EndsAt:         pgtype.Timestamptz{Time: endsAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	return orgID
}

func TestDemoteExpiredEnterpriseTrials_LocksOutExpiredTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	endsAt := time.Now().Add(-time.Hour).UTC()
	orgID := newTrialOrg(t, ctx, ti, endsAt)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)

	expired, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, expired)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredEnterpriseTrialArgs{OrganizationID: orgID}))

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.False(t, org.Whitelisted)

	trial, err := ti.trials.GetEnterpriseTrial(ctx, orgID)
	require.NoError(t, err)
	require.True(t, trial.DemotedAt.Valid)

	require.ElementsMatch(t, []string{orgID + ":chat", orgID + ":internal"}, ti.provisioner.disabledKeys())

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
func TestDemoteExpiredEnterpriseTrials_ListSkipsTrialsThatAreNotDue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)

	active := newTrialOrg(t, ctx, ti, time.Now().Add(24*time.Hour).UTC())
	converted := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	demoted := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	expired := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	_, err := ti.trials.MarkEnterpriseTrialConverted(ctx, converted)
	require.NoError(t, err)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredEnterpriseTrialArgs{OrganizationID: demoted}))

	due, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{expired}, due)
	require.NotContains(t, due, active)
}

// A conversion that lands between the list and the write must win, and the
// organization must be left exactly as the conversion left it.
func TestDemoteExpiredEnterpriseTrials_DemoteSkipsTrialConvertedAfterListing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	expired, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, expired)

	_, err = ti.trials.MarkEnterpriseTrialConverted(ctx, orgID)
	require.NoError(t, err)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredEnterpriseTrialArgs{OrganizationID: orgID}))

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	require.True(t, org.Whitelisted)

	trial, err := ti.trials.GetEnterpriseTrial(ctx, orgID)
	require.NoError(t, err)
	require.False(t, trial.DemotedAt.Valid)
}

// Temporal retries a failed activity, so a second demotion of the same trial
// must not write a second audit entry.
func TestDemoteExpiredEnterpriseTrials_DemoteIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredEnterpriseTrialArgs{OrganizationID: orgID}))

	trial, err := ti.trials.GetEnterpriseTrial(ctx, orgID)
	require.NoError(t, err)
	firstDemotedAt := trial.DemotedAt.Time

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)

	require.NoError(t, ti.activity.Demote(ctx, activities.DemoteExpiredEnterpriseTrialArgs{OrganizationID: orgID}))

	trial, err = ti.trials.GetEnterpriseTrial(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, firstDemotedAt, trial.DemotedAt.Time)

	again, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialDemoted)
	require.NoError(t, err)
	require.Equal(t, after, again)
}

// The keys are locked down first on purpose: a stamped demoted_at drops the row
// out of the next sweep, so a failed lockdown would never be retried.
func TestDemoteExpiredEnterpriseTrials_KeyLockdownFailureLeavesTrialArmed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTrialTestInstance(t)
	orgID := newTrialOrg(t, ctx, ti, time.Now().Add(-time.Hour).UTC())
	ti.provisioner.failWith = errors.New("openrouter unavailable")

	require.Error(t, ti.activity.Demote(ctx, activities.DemoteExpiredEnterpriseTrialArgs{OrganizationID: orgID}))

	org, err := ti.orgs.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	require.True(t, org.Whitelisted)

	trial, err := ti.trials.GetEnterpriseTrial(ctx, orgID)
	require.NoError(t, err)
	require.False(t, trial.DemotedAt.Valid)

	due, err := ti.activity.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, due)
}

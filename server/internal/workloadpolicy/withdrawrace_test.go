package workloadpolicy_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// A withdrawal that holds the issuer when an admit arrives makes the admit wait,
// then refuse with not-found once the issuer is gone.
func TestAdmitSubject_WaitsForAConcurrentIssuerWithdrawal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuerID := uuid.MustParse(registerAnthropic(t, ctx, ti, true).Issuers[0].ID)
	agent := newAgent(t, ctx, ti, "assigned-workload")

	withdrawal := testenv.BeginTx(t, ctx, ti.conn)
	q := repo.New(withdrawal)
	_, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{OrganizationID: ti.orgID, ID: issuerID})
	require.NoError(t, err)
	_, err = q.SoftDeleteWorkloadIssuer(ctx, repo.SoftDeleteWorkloadIssuerParams{OrganizationID: ti.orgID, ProjectID: uuid.NullUUID{}, ID: issuerID})
	require.NoError(t, err)

	result := runBlockedBy(t, ctx, ti, withdrawal, func(ctx context.Context) error {
		_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agent)
		return err
	})
	requireOopsCode(t, <-result, oops.CodeNotFound)
	requireNoLiveChildren(t, ctx, ti, issuerID)
}

// An admit that holds the issuer when a withdrawal arrives makes the withdrawal
// wait, then cascade over the rows the admit wrote.
func TestWithdrawIssuer_WaitsForAConcurrentAdmitAndCascadesOverIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuerID := uuid.MustParse(registerAnthropic(t, ctx, ti, true).Issuers[0].ID)
	agent := newAgent(t, ctx, ti, "assigned-workload")

	admission := testenv.BeginTx(t, ctx, ti.conn)
	q := repo.New(admission)
	_, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{OrganizationID: ti.orgID, ID: issuerID})
	require.NoError(t, err)
	_, err = q.CreateWorkloadAdmission(ctx, repo.CreateWorkloadAdmissionParams{
		OrganizationID:   ti.orgID,
		ProjectID:        uuid.NullUUID{},
		WorkloadIssuerID: issuerID,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             pgtype.Text{},
		Tags:             []string{},
	})
	require.NoError(t, err)
	_, err = q.UpsertWorkloadAgentAssignment(ctx, repo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: issuerID,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		AgentID:          agent,
	})
	require.NoError(t, err)

	result := runBlockedBy(t, ctx, ti, admission, func(ctx context.Context) error {
		_, err := ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			ID:               issuerID.String(),
		})
		if err != nil {
			return fmt.Errorf("withdraw issuer: %w", err)
		}
		return nil
	})
	require.NoError(t, <-result)
	requireNoLiveChildren(t, ctx, ti, issuerID)
}

// runBlockedBy starts call, waits until it is blocked on a lock held by holder,
// then commits holder so call resumes. Waiting on pg_blocking_pids rather than a
// sleep is what makes the test drive the interleaving instead of hoping for it.
func runBlockedBy(t *testing.T, ctx context.Context, ti *testInstance, holder pgx.Tx, call func(context.Context) error) <-chan error {
	t.Helper()

	holderPID, err := testrepo.New(holder).BackendPIDFixture(ctx)
	require.NoError(t, err)

	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() { result <- call(bounded) }()

	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(ti.conn).CountBackendsBlockedByFixture(ctx, holderPID)
		return err == nil && blocked > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, holder.Commit(ctx))

	return result
}

// The policy list joins through the issuer and would hide orphans, so this asks
// the child tables directly.
func requireNoLiveChildren(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) {
	t.Helper()

	q := repo.New(ti.conn)
	admissions, err := q.SoftDeleteWorkloadAdmissionsByIssuer(ctx, repo.SoftDeleteWorkloadAdmissionsByIssuerParams{OrganizationID: ti.orgID, WorkloadIssuerID: issuerID})
	require.NoError(t, err)
	require.Empty(t, admissions, "a live admission was left behind the withdrawn issuer")
	assignments, err := q.SoftDeleteWorkloadAgentAssignmentsByIssuer(ctx, repo.SoftDeleteWorkloadAgentAssignmentsByIssuerParams{OrganizationID: ti.orgID, WorkloadIssuerID: issuerID})
	require.NoError(t, err)
	require.Empty(t, assignments, "a live agent assignment was left behind the withdrawn issuer")
}

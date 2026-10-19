package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
)

func TestAssistantWorkloadIdentityLockedUntilRegistrationCommit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	a, err := assistantrepo.New(ti.conn).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID, CreatedByUserID: conv.ToPGText(fx.userID),
		Name: "Locked workload test", Model: "test/model", Instructions: "test", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(t, err)
	trigger, err := triggerrepo.New(ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: fx.orgID, ProjectID: fx.target.ProjectID, DefinitionSlug: "manual", Name: "Locked root", TargetKind: "assistant", TargetRef: a.ID.String(), TargetDisplay: "Locked workload test", ConfigJson: []byte(`{}`), Status: "active",
	})
	require.NoError(t, err)
	identities := assistantidentity.New(ti.serverURL.String(), ti.audit)
	setup := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, identities.Provision(ctx, setup, assistantidentity.ProvisionParams{OrganizationID: fx.orgID, ProjectID: fx.target.ProjectID, AssistantID: a.ID, ActorUserID: fx.userID}))
	require.NoError(t, setup.Commit(ctx))

	tx := testenv.BeginTx(t, ctx, ti.conn)
	resolved, err := identities.ResolveLocked(ctx, tx, fx.orgID, fx.target.ProjectID, a.ID, trigger.ID)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolved.State)
	require.NotNil(t, resolved.Identity)
	issuerID := resolved.Identity.IssuerID
	// Each writer's SQLc lock must remain blocked after resolution returns.
	// Bound the probe with a deadline rather than starting an unbounded waiter.
	for _, lock := range []func(context.Context, pgx.Tx) error{
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := identityrepo.New(tx).LockAssistant(ctx, identityrepo.LockAssistantParams{ProjectID: fx.target.ProjectID, AssistantID: a.ID})
			return err
		},
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := identityrepo.New(tx).LockTriggers(ctx, identityrepo.LockTriggersParams{ProjectID: fx.target.ProjectID, TriggerIds: []uuid.UUID{trigger.ID}})
			return err
		},
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := workloadrepo.New(tx).LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: fx.orgID, ID: issuerID})
			return err
		},
	} {
		other := testenv.BeginTx(t, ctx, ti.conn)
		probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		err := lock(probeCtx, other)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		// Cancellation can close the connection; cleanup tolerates that case.
		_ = other.Rollback(ctx)
	}
	require.NoError(t, tx.Commit(ctx))

	// Resolution must use the caller's transaction, not a new pool snapshot:
	// an uncommitted admission or assignment withdrawal must already deny it.
	for _, mutation := range []string{"admission", "assignment"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			tx := testenv.BeginTx(t, ctx, ti.conn)
			q := workloadrepo.New(tx)
			// Take the ordinary locks first, then simulate a transaction-local withdrawal.
			_, err := identities.ResolveLocked(ctx, tx, fx.orgID, fx.target.ProjectID, a.ID, trigger.ID)
			require.NoError(t, err)
			if mutation == "admission" {
				rows, err := q.SoftDeleteWorkloadAdmissionsByIssuer(ctx, workloadrepo.SoftDeleteWorkloadAdmissionsByIssuerParams{OrganizationID: fx.orgID, WorkloadIssuerID: issuerID})
				require.NoError(t, err)
				require.NotEmpty(t, rows)
			} else {
				rows, err := q.SoftDeleteWorkloadAgentAssignmentsByIssuer(ctx, workloadrepo.SoftDeleteWorkloadAgentAssignmentsByIssuerParams{OrganizationID: fx.orgID, WorkloadIssuerID: issuerID})
				require.NoError(t, err)
				require.NotEmpty(t, rows)
			}
			denied, err := identities.ResolveLocked(ctx, tx, fx.orgID, fx.target.ProjectID, a.ID, trigger.ID)
			require.NoError(t, err)
			require.Equal(t, assistantidentity.Unavailable, denied.State)
			require.Nil(t, denied.Identity)
			require.NoError(t, tx.Rollback(ctx))
		})
	}
}

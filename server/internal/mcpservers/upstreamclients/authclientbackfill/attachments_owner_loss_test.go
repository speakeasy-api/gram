package authclientbackfill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Owner loss locks the user or membership before latching the owner's agents.
// The backfill locks every candidate owner and membership before any agent,
// so an owner losing their membership, or being disabled, while it runs over
// several attachments latches their agents without waiting on the backfill:
// neither aborts, the lost owner's attachment is remediated and the others
// are carried over.
func TestRun_AttachmentGrantsLetAConcurrentOwnerLossFinish(t *testing.T) {
	t.Parallel()
	for name, lose := range map[string]func(t *testing.T, f *fixture, tx pgx.Tx, owner string){
		"membership removed": func(t *testing.T, f *fixture, tx pgx.Tx, owner string) {
			t.Helper()
			require.NoError(t, orgrepo.New(tx).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: f.orgID, UserID: pgtype.Text{String: owner, Valid: true}}))
		},
		"user disabled": func(t *testing.T, f *fixture, tx pgx.Tx, owner string) {
			t.Helper()
			require.NoError(t, New(tx).DisableUserFixture(t.Context(), owner))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			registration := f.client("subject")
			f.associate(f.server(), registration)
			f.run(true)
			kept, lost := f.member(), f.member()
			if kept > lost {
				// Owners lock in id order: the kept owner locks before the
				// backfill waits on the lost one, which the probe relies on.
				kept, lost = lost, kept
			}
			keptAgents := []uuid.UUID{f.agent(kept), f.agent(kept)}
			for _, agentID := range keptAgents {
				f.attach(agentID, registration, "user:"+kept, kept, 0)
			}
			lostAgent := f.agent(lost)
			lostBinding := f.attach(lostAgent, registration, "user:"+lost, lost, 0)

			tx, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; the writes use generated SQLc methods.
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
			lose(t, f, tx, lost)

			done := make(chan Report, 1)
			go func() {
				report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
				assert.NoError(t, err)
				done <- report
			}()
			require.Eventually(t, func() bool {
				_, err := f.q.ProbeUserLockFixture(t.Context(), kept)
				var pgErr *pgconn.PgError
				return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.LockNotAvailable
			}, 10*time.Second, 20*time.Millisecond, "the backfill holds the owners and waits on the lost owner")

			// The owner loss latches the agents while the backfill waits: it
			// holds no agent yet.
			latchCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = agentsrepo.New(tx).LatchAgentsForOwnerLossByUser(latchCtx, agentsrepo.LatchAgentsForOwnerLossByUserParams{
				OwnerUserID: lost, OwnerReassignmentReason: pgtype.Text{String: "membership_removed", Valid: true},
			})
			require.NoError(t, err, "latching the owner's agents does not wait on the backfill")
			require.NoError(t, tx.Commit(t.Context()))

			report := <-done
			require.Empty(t, report.PhaseErrors)
			require.Equal(t, []AttachmentRemediation{{BindingID: lostBinding.String(), AgentID: lostAgent.String(), Reason: AttachmentAgentInactive}}, report.AttachmentRemediations)
			f.requireNoConnectionGrant(lostAgent)
			for _, agentID := range keptAgents {
				agent := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String())
				require.Contains(t, f.grantsOf(agent), authz.Grant{
					PrincipalUrn: agent.String(), Scope: authz.ScopeMCPAuth,
					Selector: authz.ConnectionAuthClientSelector(registration.String(), kept), Inferred: true,
				})
			}
		})
	}
}

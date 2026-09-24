package agentmanagement

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Record actual query acquisition order, not just the order of a sorted helper.
// Forward every operation through the same transaction so row-lock and live
// authorization behavior remain real.
type membershipLockRecorder struct {
	repo.DBTX
	t       *testing.T
	locks   []string
	onAgent func()
}

func (r *membershipLockRecorder) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.HasPrefix(query, "-- name: LockActiveOrganizationUser :one"):
		require.Equal(r.t, "org-lock-order", args[1])
		userID, ok := args[0].(pgtype.Text)
		require.True(r.t, ok)
		r.locks = append(r.locks, userID.String)
	case strings.HasPrefix(query, "-- name: GetAgentByIDForUpdate :one"):
		r.locks = append(r.locks, "agent")
		if r.onAgent != nil {
			r.onAgent()
		}
	}
	return r.DBTX.QueryRow(ctx, query, args...)
}

func TestMembershipLocksUseCanonicalOrderBeforeAgent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-lock-order")
	for _, userID := range []string{"a-user", "z-user"} {
		seedOrganizationUser(t, db, "org-lock-order", userID)
	}
	for _, operation := range []string{"create", "transfer", "credential"} {
		for _, pair := range [][2]string{{"z-user", "a-user"}, {"a-user", "z-user"}, {"z-user", "z-user"}} {
			t.Run(operation+"/"+pair[0]+"/"+pair[1], func(t *testing.T) {
				t.Parallel()
				caller, owner := pair[0], pair[1]
				agent := createAgent(t, db, "org-lock-order", owner, operation+"-"+caller+"-"+owner)
				engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
				for _, scope := range []authz.Scope{authz.ScopeAgentWrite, authz.ScopeAgentTransfer, authz.ScopeAgentAuthorize} {
					allow(engine, scope, agent.ID)
				}
				authorizer := NewAuthorizer(engine)
				ctx := validatedHumanContext(t, "org-lock-order", caller)
				err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
					recorder := &membershipLockRecorder{DBTX: tx, t: t}
					var err error
					switch operation {
					case "create":
						_, err = authorizer.RequireCreate(ctx, recorder, agent.ID, owner)
					case "transfer":
						_, _, err = authorizer.RequireTransfer(ctx, recorder, agent.ID, owner)
					case "credential":
						_, _, err = authorizer.RequireAgentOwnerForUpdate(ctx, recorder, agent.ID, OwnedAgentAuthorize)
					}
					require.NoError(t, err)
					want := []string{"a-user", "z-user"}
					if caller == owner {
						want = []string{caller}
					}
					if operation != "create" {
						want = append(want, "agent")
					}
					require.Equal(t, want, recorder.locks)
					return nil
				})
				require.NoError(t, err)
			})
		}
	}
}

func TestAgentOwnerLockRejectsOwnershipChangeDuringAcquisition(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-lock-order")
	for _, userID := range []string{"a-user", "z-user", "replacement"} {
		seedOrganizationUser(t, db, "org-lock-order", userID)
	}
	agent := createAgent(t, db, "org-lock-order", "z-user", "Owner race agent")
	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	allow(engine, authz.ScopeAgentAuthorize, agent.ID)
	authorizer := NewAuthorizer(engine)
	ctx := validatedHumanContext(t, "org-lock-order", "a-user")
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		recorder := &membershipLockRecorder{DBTX: tx, t: t, onAgent: func() {
			// A transfer can commit after the observation and before our agent
			// lock. It does not change the pinned old-owner membership.
			_, err := repo.New(db).TransferAgent(ctx, repo.TransferAgentParams{
				OrganizationID: "org-lock-order", ID: agent.ID, OwnerUserID: "replacement",
			})
			require.NoError(t, err)
		}}
		_, _, err := authorizer.RequireAgentOwnerForUpdate(ctx, recorder, agent.ID, OwnedAgentAuthorize)
		requireOopsCode(t, err, oops.CodeForbidden)
		require.Equal(t, []string{"a-user", "z-user", "agent"}, recorder.locks, "must not acquire the replacement membership out of order")
		return nil
	})
	require.NoError(t, err)
}

package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const proposalTestSubject = "user:staff-subject"

type proposalFixture struct {
	db     *pgxpool.Pool
	store  *proposalStore
	cipher *encryption.Client
	owner  proposalOwner
	orgA   string
	orgB   string
	reauth func()
}

func newProposalFixture(t *testing.T, name string) proposalFixture {
	t.Helper()
	ctx := t.Context()
	db, err := staffMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	orgs := orgrepo.New(db)
	orgA, orgB := "org_a_"+uuid.NewString(), "org_b_"+uuid.NewString()
	require.NoError(t, orgs.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: orgA, Name: "Synthetic A", Slug: "synthetic-a-" + uuid.NewString()}))
	require.NoError(t, orgs.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: orgB, Name: "Synthetic B", Slug: "synthetic-b-" + uuid.NewString()}))

	clientID := "client_" + uuid.NewString()
	require.NoError(t, (postgresStaffClientStore{db: db}).RegisterClient(ctx, staffOAuthClient{ID: clientID, Name: "Test editor", SecretHash: "", RedirectURIs: []string{"http://localhost:5555/callback"}, SecretExpiresAt: nil}))
	authorizations := postgresStaffAuthorizationStore{db: db}
	grants := postgresStaffGrantStore{db: db}
	verifier := "test-verifier-" + uuid.NewString() + uuid.NewString()
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	encryptedSession, err := cipher.Encrypt([]byte("browser-session"))
	require.NoError(t, err)
	now := time.Now()
	authorization := staffAuthorization{
		Subject: proposalTestSubject, ClientID: clientID, SessionEnc: encryptedSession, ResourceURI: staffAudience,
		Scopes: []string{ScopeRead, ScopeWrite}, CodeHash: staffTokenHash("code-" + uuid.NewString()), CodeChallenge: staffChallengeForTest(verifier),
		RedirectURI: "http://localhost:5555/callback", ExpiresAt: now.Add(2 * time.Hour), GrantExpires: now.Add(10 * time.Minute),
	}
	require.NoError(t, authorizations.Authorize(ctx, authorization))
	connection, err := grants.ValidateGrant(ctx, authorization.CodeHash, clientID, authorization.RedirectURI, verifier, now)
	require.NoError(t, err)

	return proposalFixture{
		db:     db,
		store:  newProposalStore(db, nil),
		cipher: cipher,
		owner:  proposalOwner{SubjectURN: proposalTestSubject, ClientRowID: connection.ClientRowID, ConnectionID: connection.ID, Generation: connection.Generation},
		orgA:   orgA,
		orgB:   orgB,
		reauth: func() {
			authorization.CodeHash = staffTokenHash("code-" + uuid.NewString())
			require.NoError(t, authorizations.Authorize(ctx, authorization))
		},
	}
}

func featureProposal(organizationID, key string, enabled bool) NewProposal {
	arguments, _ := json.Marshal(map[string]any{"feature": "logs", "enabled": enabled})
	expected, _ := json.Marshal(map[string]any{"feature": "logs", "enabled": !enabled})
	preview, _ := json.Marshal(map[string]any{"organization_id": organizationID, "before": !enabled, "after": enabled})
	return NewProposal{
		Operation: OperationSetOrganizationFeature, SchemaVersion: 1,
		Target:         ProposalTarget{OrganizationID: organizationID},
		IdempotencyKey: key, Arguments: arguments, ExpectedState: expected, Preview: preview,
	}
}

func countWriteEvents(t *testing.T, db *pgxpool.Pool, proposalID uuid.UUID, event string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM admin_mcp_write_events WHERE proposal_id = $1 AND event = $2`, proposalID, event).Scan(&count)) //nolint:glint // notestingrawsql: Assert the staff event trail directly.
	return count
}

func organizationName(t *testing.T, db *pgxpool.Pool, id string) string {
	t.Helper()
	org, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), id)
	require.NoError(t, err)
	return org.Name
}

// renameRunner stands in for a business mutator: it changes the target
// organisation inside the execution transaction, scoped by the stored target.
func renameRunner(calls *atomic.Int32, name string, fail error) ProposalRunner {
	return func(ctx context.Context, tx pgx.Tx, p Proposal) (string, json.RawMessage, error) {
		calls.Add(1)
		tag, err := tx.Exec(ctx, `UPDATE organization_metadata SET name = $2 WHERE id = $1`, p.Target.OrganizationID, name) //nolint:glint // notestingrawsql: Synthetic business write inside the execution transaction.
		if err != nil {
			return "", nil, fmt.Errorf("rename synthetic organization: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return "", nil, ErrStaleState
		}
		if fail != nil {
			return "", nil, fail
		}
		return "succeeded", json.RawMessage(`{"renamed":true}`), nil
	}
}

func TestProposalStoreIdempotentCreate(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_create")
	ctx, now := t.Context(), time.Now()

	first, replay, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "retry-1", true), now)
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, ProposalPendingApproval, first.Status)
	require.NotEmpty(t, first.ProposalDigest)
	require.Equal(t, 1, countWriteEvents(t, f.db, first.ID, "prepared"))

	again, replay, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "retry-1", true), now)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, first.ID, again.ID)

	// Same retry key, different tenant or different change: refused.
	_, _, err = f.store.Create(ctx, f.owner, featureProposal(f.orgB, "retry-1", true), now)
	require.ErrorIs(t, err, ErrProposalConflict)
	_, _, err = f.store.Create(ctx, f.owner, featureProposal(f.orgA, "retry-1", false), now)
	require.ErrorIs(t, err, ErrProposalConflict)

	// Retry keys survive reconsent: a new generation cannot mint a second proposal.
	f.reauth()
	_, _, err = f.store.Create(ctx, f.owner, featureProposal(f.orgA, "retry-2", true), now)
	require.ErrorIs(t, err, ErrConnectionChanged, "the stale generation can no longer prepare")
}

func TestProposalStoreRejectsInvalidTargets(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_targets")
	ctx, now := t.Context(), time.Now()

	tenantless := featureProposal("", "k", true)
	_, _, err := f.store.Create(ctx, f.owner, tenantless, now)
	require.Error(t, err)

	global := featureProposal(f.orgA, "global", true)
	global.Operation = OperationUpdateSupportMatrix
	_, _, err = f.store.Create(ctx, f.owner, global, now)
	require.Error(t, err, "a platform-global operation must not carry a tenant")

	unknown := featureProposal(f.orgA, "unknown", true)
	unknown.Operation = WriteOperation("delete_global_issuer")
	_, _, err = f.store.Create(ctx, f.owner, unknown, now)
	require.Error(t, err)

	missingOrg := featureProposal("org_missing_"+uuid.NewString(), "missing", true)
	_, _, err = f.store.Create(ctx, f.owner, missingOrg, now)
	require.Error(t, err, "the organisation foreign key rejects a non-existent tenant")
}

func TestProposalStoreLimitsPendingProposals(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_pending")
	ctx, now := t.Context(), time.Now()
	for i := range maxPendingProposals {
		_, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "pending-"+uuid.NewString(), i%2 == 0), now)
		require.NoError(t, err)
	}
	_, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "one-too-many", true), now)
	require.ErrorIs(t, err, ErrTooManyPending)
}

func allowProposalBrowser(context.Context, pgx.Tx, Proposal) error { return nil }

func TestProposalStoreApprovalIsBoundToSubjectAndDigest(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_approve")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "approve", true), now)
	require.NoError(t, err)

	_, err = f.store.GetForSubject(ctx, p.ID, "user:other-staff")
	require.ErrorIs(t, err, ErrProposalNotFound, "another staff member cannot see the proposal")
	_, err = f.store.Approve(ctx, p.ID, "user:other-staff", p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.ErrorIs(t, err, ErrProposalNotFound, "another staff member cannot approve")
	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, "stale-digest", now, allowProposalBrowser, allowProposalBrowser)
	require.ErrorIs(t, err, ErrProposalChanged)

	stale := func(context.Context, pgx.Tx, Proposal) error { return ErrStaleState }
	approvedStale, err := f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, stale)
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, approvedStale.ID)
	closed, err := f.store.GetForSubject(ctx, p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, closed.Status)
	require.Equal(t, reasonStaleState, closed.InvalidationReason)

	q, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "expire", true), now)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, q.ID, proposalTestSubject, q.ProposalDigest, now.Add(proposalLifetime+time.Second), allowProposalBrowser, allowProposalBrowser)
	require.ErrorIs(t, err, ErrProposalExpired)
}

func TestProposalStoreExecuteOnceAndReplay(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_execute")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "execute", true), now)
	require.NoError(t, err)
	var calls atomic.Int32
	run := renameRunner(&calls, "Renamed A", nil)

	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: run})
	require.ErrorIs(t, err, ErrProposalNotApproved, "a model cannot execute without browser approval")
	require.Zero(t, calls.Load())
	require.Equal(t, 1, countWriteEvents(t, f.db, p.ID, "execute_refused"))

	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)

	otherOwner := f.owner
	otherOwner.SubjectURN = "user:other-staff"
	_, _, err = f.store.Execute(ctx, otherOwner, p.ID, now, ProposalExecution{Run: run})
	require.ErrorIs(t, err, ErrProposalNotFound)

	// Concurrent executes serialise on the proposal row: one write, one receipt.
	var wg sync.WaitGroup
	results := make([]error, 4)
	replays := make([]bool, 4)
	for i := range results {
		wg.Go(func() {
			_, replays[i], results[i] = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: run})
		})
	}
	wg.Wait()
	executed := 0
	for i, err := range results {
		require.NoError(t, err)
		if !replays[i] {
			executed++
		}
	}
	require.Equal(t, 1, executed)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "Renamed A", organizationName(t, f.db, f.orgA))
	require.Equal(t, "Synthetic B", organizationName(t, f.db, f.orgB), "the other tenant is untouched")
	require.Equal(t, 1, countWriteEvents(t, f.db, p.ID, "executed"))

	receipt, replay, err := f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: run})
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, ProposalSucceeded, receipt.Status)
	require.JSONEq(t, `{"renamed":true}`, string(receipt.ResultPayload))
	require.Equal(t, int32(1), calls.Load())

	newOwner := f.owner
	newOwner.Generation = uuid.New()
	_, _, err = f.store.Execute(ctx, newOwner, p.ID, now, ProposalExecution{Run: run})
	require.ErrorIs(t, err, ErrConnectionChanged, "reconsent cannot replay a receipt from a previous generation")
	require.Equal(t, int32(1), calls.Load())

	_, err = f.store.Reject(ctx, p.ID, proposalTestSubject)
	require.ErrorIs(t, err, ErrProposalClosed, "an executed proposal cannot be rejected")
}

func TestProposalStoreFailedWriteRollsBack(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_rollback")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "rollback", true), now)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)

	var calls atomic.Int32
	failure := errors.New("audit write failed")
	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: renameRunner(&calls, "Should Not Persist", failure)})
	require.ErrorIs(t, err, failure)
	require.Equal(t, "Synthetic A", organizationName(t, f.db, f.orgA), "the business change rolled back with the failure")
	still, err := f.store.GetForSubject(ctx, p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, still.Status, "a failed attempt does not consume the approval")
	require.Equal(t, 1, countWriteEvents(t, f.db, p.ID, "execute_refused"), "the refusal survives the rollback")
	require.Zero(t, countWriteEvents(t, f.db, p.ID, "executed"))

	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: renameRunner(&calls, "Recovered", nil)})
	require.NoError(t, err)
	require.Equal(t, "Recovered", organizationName(t, f.db, f.orgA))
}

func TestProposalStoreStaleStateInvalidates(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_stale")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "stale", true), now)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)

	stale := func(context.Context, pgx.Tx, Proposal) (string, json.RawMessage, error) {
		return "", nil, ErrStaleState
	}
	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: stale})
	require.ErrorIs(t, err, ErrStaleState)
	closed, err := f.store.GetForSubject(ctx, p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, closed.Status)

	var calls atomic.Int32
	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: renameRunner(&calls, "Never", nil)})
	require.ErrorIs(t, err, ErrProposalInvalidated, "an invalidated proposal cannot be resurrected")
	require.Zero(t, calls.Load())
}

func TestProposalStoreReconsentInvalidatesApproval(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_reconsent")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "reconsent", true), now)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)

	f.reauth()
	var calls atomic.Int32
	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: renameRunner(&calls, "Never", nil)})
	require.ErrorIs(t, err, ErrConnectionChanged)
	require.Zero(t, calls.Load())
	closed, err := f.store.GetForSubject(ctx, p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, closed.Status)
	require.Equal(t, reasonConnectionChanged, closed.InvalidationReason)
}

func TestProposalStoreExecuteHoldsLocksThroughAfterCommit(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_locks")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "locks", true), now)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)

	var order []string
	var lockCalls atomic.Int32
	lock := func(ctx context.Context, seen Proposal) (*pgxpool.Conn, func(), error) {
		lockCalls.Add(1)
		require.Equal(t, p.ID, seen.ID)
		conn, err := f.db.Acquire(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("acquire synthetic execution lock: %w", err)
		}
		order = append(order, "lock")
		return conn, func() { order = append(order, "release"); conn.Release() }, nil
	}
	var calls atomic.Int32
	failing := ProposalExecution{
		Lock:        lock,
		Run:         renameRunner(&calls, "Never", errors.New("run failed")),
		AfterCommit: func(context.Context, Proposal) { order = append(order, "after_commit") },
	}
	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, failing)
	require.Error(t, err)
	require.Equal(t, []string{"lock", "release"}, order, "a failed run never reaches the post-commit hook")

	order = nil
	execution := ProposalExecution{
		Lock: lock,
		Run:  renameRunner(&calls, "Locked Rename", nil),
		AfterCommit: func(_ context.Context, receipt Proposal) {
			require.Equal(t, ProposalSucceeded, receipt.Status)
			order = append(order, "after_commit")
		},
	}
	_, replay, err := f.store.Execute(ctx, f.owner, p.ID, now, execution)
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, []string{"lock", "after_commit", "release"}, order, "locks are held until the post-commit hook finishes")
	require.Equal(t, "Locked Rename", organizationName(t, f.db, f.orgA))

	order = nil
	before := lockCalls.Load()
	_, replay, err = f.store.Execute(ctx, f.owner, p.ID, now, execution)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, before, lockCalls.Load(), "a replay does not reacquire locks")
	require.Empty(t, order)
}

func TestProposalStoreRejectCancelsApproval(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_proposal_reject")
	ctx, now := t.Context(), time.Now()
	p, _, err := f.store.Create(ctx, f.owner, featureProposal(f.orgA, "reject", true), now)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, p.ID, proposalTestSubject, p.ProposalDigest, now, allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)

	_, err = f.store.Reject(ctx, p.ID, "user:other-staff")
	require.ErrorIs(t, err, ErrProposalNotFound)
	rejected, err := f.store.Reject(ctx, p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalRejected, rejected.Status)

	var calls atomic.Int32
	_, _, err = f.store.Execute(ctx, f.owner, p.ID, now, ProposalExecution{Run: renameRunner(&calls, "Never", nil)})
	require.ErrorIs(t, err, ErrProposalClosed)
	require.Zero(t, calls.Load())
}

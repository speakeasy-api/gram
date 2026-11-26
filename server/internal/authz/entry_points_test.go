package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type outcome string

const (
	allowed  outcome = "allowed"
	refused  outcome = "refused"
	invalid  outcome = "invalid"
	noChecks outcome = "no checks"
)

// Require and Decide answer the same checks the same way, except where a case
// names the difference. The caller holds project read on proj_b and a block
// on proj_a. Example: Either(proj_a, proj_b) is refused by both, because the
// block on proj_a refuses the whole group.
func TestRequireAndDecideAgree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		enforced bool
		checks   []Check
		require  outcome
		decide   outcome
		// differs says why the two answers differ, when they do.
		differs string
	}{
		{
			name:     "plain check, allowed",
			enforced: true,
			checks:   []Check{projectRead("proj_b")},
			require:  allowed,
			decide:   allowed,
		},
		{
			name:     "plain check, not granted",
			enforced: true,
			checks:   []Check{projectRead("proj_c")},
			require:  refused,
			decide:   refused,
		},
		{
			name:     "Either, second alternative allowed",
			enforced: true,
			checks:   []Check{Either(projectRead("proj_c"), projectRead("proj_b"))},
			require:  allowed,
			decide:   allowed,
		},
		{
			name:     "Either, block on one alternative",
			enforced: true,
			checks:   []Check{Either(projectRead("proj_a"), projectRead("proj_b"))},
			require:  refused,
			decide:   refused,
		},
		{
			name:     "Either, no alternative granted",
			enforced: true,
			checks:   []Check{Either(projectRead("proj_a"), projectRead("proj_c"))},
			require:  refused,
			decide:   refused,
		},
		{
			name:     "empty Either",
			enforced: true,
			checks:   []Check{Either()},
			require:  invalid,
			decide:   invalid,
		},
		{
			name:     "empty Either, skipped caller",
			enforced: false,
			checks:   []Check{Either()},
			require:  invalid,
			decide:   invalid,
		},
		{
			name:     "Either, skipped caller",
			enforced: false,
			checks:   []Check{Either(projectRead("proj_a"), projectRead("proj_b"))},
			require:  allowed,
			decide:   allowed,
		},
		{
			name:     "plain checks, skipped caller",
			enforced: false,
			checks:   []Check{projectRead("proj_a"), projectRead("proj_c")},
			require:  allowed,
			decide:   allowed,
		},
		{
			name:     "no checks",
			enforced: true,
			checks:   nil,
			require:  noChecks,
			decide:   refused,
			differs:  "an empty list page is a page with nothing on it, not an error",
		},
		{
			name:     "probe",
			enforced: true,
			checks:   []Check{projectRead("proj_b").Probe()},
			require:  invalid,
			decide:   allowed,
			differs:  "gating an action needs the concrete resource",
		},
		{
			name:     "Either next to a plain check",
			enforced: true,
			checks:   []Check{Either(projectRead("proj_c"), projectRead("proj_b")), projectRead("proj_b")},
			require:  invalid,
			decide:   allowed,
			differs:  "Decide answers each check on its own",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.require != tc.decide, tc.differs != "", "a difference must say why")

			ctx, engine, _ := entryPointEngine(t, tc.enforced)

			err := engine.Require(ctx, tc.checks...)
			requireOutcome(t, tc.require, err == nil, err)

			d, err := engine.Decide(ctx, tc.checks...)
			requireOutcome(t, tc.decide, d.AllAllowed, err)
		})
	}
}

// A challenge row from DecideAndRecord names the alternative that allowed a
// group. Example: Either(proj_c, proj_b) is recorded as proj_b; refused, it
// lists both.
func TestDecideAndRecord_recordsWinningAlternative(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		check    Check
		recorded []string
	}{
		{name: "allowed", check: Either(projectRead("proj_c"), projectRead("proj_b")), recorded: []string{"proj_b"}},
		{name: "refused", check: Either(projectRead("proj_c"), projectRead("proj_d")), recorded: []string{"proj_c", "proj_d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, engine, conn := entryPointEngine(t, true)
			_, err := engine.DecideAndRecord(ctx, tc.check)
			require.NoError(t, err)

			rows := listChallengeOutboxRows(t, conn)
			require.Len(t, rows, 1)
			message := &authzv1.Challenge{}
			require.NoError(t, proto.Unmarshal(rows[0].Message, message))
			ids := make([]string, 0, len(message.GetRequestedChecks()))
			for _, c := range message.GetRequestedChecks() {
				ids = append(ids, c.GetResourceId())
			}
			require.Equal(t, tc.recorded, ids)
		})
	}
}

// A group inside a group is a mistake, whether or not RBAC applies to the
// caller.
func TestEither_refusesNestedGroup(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		enforced bool
		check    Check
	}{
		{name: "enforced", enforced: true, check: Either(projectRead("proj_b"))},
		{name: "skipped caller", enforced: false, check: Either(projectRead("proj_b"))},
		{name: "empty, skipped caller", enforced: false, check: Either()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, engine, _ := entryPointEngine(t, tc.enforced)
			require.ErrorIs(t, engine.Require(ctx, Either(tc.check)), ErrInvalidCheck)
		})
	}
}

func requireOutcome(t *testing.T, want outcome, ok bool, err error) {
	t.Helper()

	var shareable *oops.ShareableError
	if errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden {
		ok, err = false, nil
	}
	switch want {
	case allowed:
		require.NoError(t, err)
		require.True(t, ok)
	case refused:
		require.NoError(t, err)
		require.False(t, ok)
	case invalid:
		require.ErrorIs(t, err, ErrInvalidCheck)
	case noChecks:
		require.ErrorIs(t, err, ErrNoChecks)
	}
}

func entryPointEngine(t *testing.T, enforced bool) (context.Context, *Engine, *pgxpool.Pool) {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	var ctx context.Context
	if enforced {
		ctx = GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{
			NewGrant(ScopeProjectRead, "proj_b"),
			NewGrant(ScopeProjectBlockedRead, "proj_a"),
		})
	} else {
		ctx = contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
			ActiveOrganizationID: orgID,
			UserID:               "user-one",
			SessionID:            nil,
		})
	}
	seedOrganization(t, ctx, conn, orgID)
	return ctx, NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient()), conn
}

// allAllowed answers checks the way a handler's branch does: without a
// challenge row.
func allAllowed(ctx context.Context, engine *Engine, checks ...Check) (bool, error) {
	decisions, err := engine.Decide(ctx, checks...)
	return decisions.AllAllowed, err
}

// allowedEach answers each check the way a list page does.
func allowedEach(ctx context.Context, engine *Engine, checks []Check) ([]bool, error) {
	decisions, err := engine.DecideAndRecord(ctx, checks...)
	if err != nil {
		return nil, err
	}
	allowed := make([]bool, len(decisions.Each))
	for i, decision := range decisions.Each {
		allowed[i] = decision.Allowed
	}
	return allowed, nil
}

// allowedIDs keeps the resource IDs of the allowed checks.
func allowedIDs(ctx context.Context, engine *Engine, checks []Check) ([]string, error) {
	allowed, err := allowedEach(ctx, engine, checks)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(checks))
	for i, ok := range allowed {
		if ok {
			ids = append(ids, checks[i].ResourceID)
		}
	}
	return ids, nil
}

func decisionReasons(ctx context.Context, engine *Engine, checks []Check) ([]Reason, error) {
	decisions, err := engine.Decide(ctx, checks...)
	if err != nil {
		return nil, err
	}
	reasons := make([]Reason, len(decisions.Each))
	for i, decision := range decisions.Each {
		reasons[i] = decision.Reason
	}
	return reasons, nil
}

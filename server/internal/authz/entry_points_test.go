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
// on proj_a. Example: AnyOf(proj_a, proj_b) is refused by both, because the
// block on proj_a vetoes the whole group.
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
			name:     "AnyOf, second alternative allowed",
			enforced: true,
			checks:   []Check{AnyOf(projectRead("proj_c"), projectRead("proj_b"))},
			require:  allowed,
			decide:   allowed,
		},
		{
			name:     "AnyOf, block on one alternative",
			enforced: true,
			checks:   []Check{AnyOf(projectRead("proj_a"), projectRead("proj_b"))},
			require:  refused,
			decide:   refused,
		},
		{
			name:     "empty AnyOf",
			enforced: true,
			checks:   []Check{AnyOf()},
			require:  invalid,
			decide:   invalid,
		},
		{
			name:     "empty AnyOf, skipped caller",
			enforced: false,
			checks:   []Check{AnyOf()},
			require:  invalid,
			decide:   invalid,
		},
		{
			name:     "AnyOf, skipped caller",
			enforced: false,
			checks:   []Check{AnyOf(projectRead("proj_a"), projectRead("proj_b"))},
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
			name:     "AnyOf next to a plain check",
			enforced: true,
			checks:   []Check{AnyOf(projectRead("proj_c"), projectRead("proj_b")), projectRead("proj_b")},
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

// A challenge row from Decide names the alternative that allowed an AnyOf.
// Example: AnyOf(proj_c, proj_b) is recorded as proj_b; refused, it lists
// both.
func TestDecide_recordsWinningAlternative(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		check    Check
		recorded []string
	}{
		{name: "allowed", check: AnyOf(projectRead("proj_c"), projectRead("proj_b")), recorded: []string{"proj_b"}},
		{name: "refused", check: AnyOf(projectRead("proj_c"), projectRead("proj_d")), recorded: []string{"proj_c", "proj_d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, engine, conn := entryPointEngine(t, true)
			_, err := engine.Decide(ctx, tc.check)
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

// RequireAny takes its alternatives as separate checks, so an AnyOf passed
// to it is a mistake, whether or not RBAC applies to the caller.
func TestRequireAny_refusesAnyOf(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		enforced bool
		check    Check
	}{
		{name: "enforced", enforced: true, check: AnyOf(projectRead("proj_b"))},
		{name: "skipped caller", enforced: false, check: AnyOf(projectRead("proj_b"))},
		{name: "empty, skipped caller", enforced: false, check: AnyOf()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, engine, _ := entryPointEngine(t, tc.enforced)
			require.ErrorIs(t, engine.RequireAny(ctx, tc.check), ErrInvalidCheck)
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

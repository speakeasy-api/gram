package authz

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Decide answers every check in the order passed. Example: grants on
// proj_a only, asked about proj_a, proj_b and proj_a again, answer yes, no,
// yes, so AllAllowed is false.
func TestDecide_answersEachCheckInOrder(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, "proj_a")})

	d, err := engine.Decide(ctx, projectRead("proj_a"), projectRead("proj_b"), projectRead("proj_a"))
	require.NoError(t, err)
	require.False(t, d.AllAllowed)
	require.Len(t, d.Each, 3)
	require.Equal(t, []bool{true, false, true}, allowedOf(d))
	require.Equal(t, []Reason{ReasonNone, ReasonNotGranted, ReasonNone}, reasonsOf(d))
	require.True(t, d.Each[1].Enforced)

	d, err = engine.Decide(ctx, projectRead("proj_a"))
	require.NoError(t, err)
	require.True(t, d.AllAllowed)
}

// No checks is never permission: AllAllowed is false and nothing errors, so
// an empty list page shows nothing.
func TestDecide_noChecksIsNotAllAllowed(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	d, err := engine.Decide(ctx)
	require.NoError(t, err)
	require.False(t, d.AllAllowed)
	require.Empty(t, d.Each)
}

// A caller RBAC skips is allowed every check, and each decision says it was
// not enforced.
func TestDecide_unenforcedCallerIsAllowedAndSaysSo(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	// No dashboard session: RBAC skips this caller.
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-one",
		UserID:               "user-one",
		SessionID:            nil,
	})

	d, err := engine.Decide(ctx, projectRead("proj_a"), projectRead("proj_b"))
	require.NoError(t, err)
	require.True(t, d.AllAllowed)
	for _, decision := range d.Each {
		require.False(t, decision.Enforced)
	}
}

// For a caller RBAC skips, a valid Either check is not reported as invalid:
// its alternatives are what gets validated.
func TestDecide_unenforcedEitherIsNotReportedInvalid(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-one",
		UserID:               "user-one",
		SessionID:            nil,
	})

	d, err := engine.Decide(ctx, Either(projectRead("proj_a"), projectRead("proj_b")))
	require.NoError(t, err)
	require.True(t, d.AllAllowed)
	require.NotContains(t, logs.String(), "invalid authz check")
}

// A refusal from Decide is a branch, not an error, and an Either check can sit
// next to other checks.
func TestDecide_acceptsEitherNextToOtherChecks(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, "proj_a")})

	d, err := engine.Decide(ctx, Either(projectRead("proj_b"), projectRead("proj_a")), projectRead("proj_b"))
	require.NoError(t, err)
	require.Equal(t, []bool{true, false}, allowedOf(d))
}

// Filter records one list row, whatever the number of checks — including a
// one-item list, which must not look like a gate refusal on that resource.
func TestFilter_writesListRowForOneOrMoreChecks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		checks         []Check
		wantOutcome    authzrepo.Outcome
		wantCandidates uint32
		wantAllowed    uint32
	}{
		{
			name:           "one refused item",
			checks:         []Check{projectRead("proj_b")},
			wantOutcome:    authzrepo.OutcomeDeny,
			wantCandidates: 1,
			wantAllowed:    0,
		},
		{
			name:           "several items",
			checks:         []Check{projectRead("proj_a"), projectRead("proj_b"), projectRead("proj_c")},
			wantOutcome:    authzrepo.OutcomeAllow,
			wantCandidates: 3,
			wantAllowed:    1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org_" + uuid.NewString()
			ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_a")})
			conn := newTestDB(t)
			seedOrganization(t, ctx, conn, orgID)
			engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

			_, err := engine.Filter(ctx, tc.checks...)
			require.NoError(t, err)

			rows := listChallengeOutboxRows(t, conn)
			require.Len(t, rows, 1)
			message := &authzv1.Challenge{}
			require.NoError(t, proto.Unmarshal(rows[0].Message, message))
			require.Equal(t, string(authzrepo.OperationFilter), message.GetOperation())
			require.Equal(t, string(tc.wantOutcome), message.GetOutcome())
			require.Equal(t, tc.wantCandidates, message.GetFilterCandidateCount())
			require.Equal(t, tc.wantAllowed, message.GetFilterAllowedCount())
			require.Empty(t, message.GetResourceId())
		})
	}
}

// Recording a decision about several checks writes one list row, whatever the
// number of checks.
func TestRecord_writesOneListRowForSeveralChecks(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_a")})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	d, err := engine.Decide(ctx, projectRead("proj_a"), projectRead("proj_b"), projectRead("proj_c"))
	require.NoError(t, err)
	engine.Record(ctx, d)

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, string(authzrepo.OperationFilter), message.GetOperation())
	require.Equal(t, string(authzrepo.OutcomeAllow), message.GetOutcome())
	require.Equal(t, uint32(3), message.GetFilterCandidateCount())
	require.Equal(t, uint32(1), message.GetFilterAllowedCount())
	require.Empty(t, message.GetResourceId())
}

// Recording a decision about one check writes the row Require writes for it,
// so a named server's row has the scope and resource the access log offers to
// grant. Example: proj_b, refused, is recorded as a refused Require on proj_b.
func TestRecord_writesOneCheckAsRequireDoes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		check Check
	}{
		{name: "allowed", check: projectRead("proj_a")},
		{name: "refused", check: projectRead("proj_b")},
		{name: "allowed group", check: Either(projectRead("proj_b"), projectRead("proj_a"))},
		{name: "refused group", check: Either(projectRead("proj_b"), projectRead("proj_c"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorded := func(record func(ctx context.Context, engine *Engine)) *authzv1.Challenge {
				t.Helper()
				orgID := "org_" + uuid.NewString()
				ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_a")})
				conn := newTestDB(t)
				seedOrganization(t, ctx, conn, orgID)
				record(ctx, NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient()))
				rows := listChallengeOutboxRows(t, conn)
				require.Len(t, rows, 1)
				message := &authzv1.Challenge{}
				require.NoError(t, proto.Unmarshal(rows[0].Message, message))
				return message
			}
			required := recorded(func(ctx context.Context, engine *Engine) {
				_ = engine.Require(ctx, tc.check)
			})
			decided := recorded(func(ctx context.Context, engine *Engine) {
				d, err := engine.Decide(ctx, tc.check)
				require.NoError(t, err)
				engine.Record(ctx, d)
			})

			require.Equal(t, required.GetOperation(), decided.GetOperation())
			require.Equal(t, required.GetOutcome(), decided.GetOutcome())
			require.Equal(t, required.GetReason(), decided.GetReason())
			require.Equal(t, required.GetScope(), decided.GetScope())
			require.Equal(t, required.GetResourceId(), decided.GetResourceId())
			require.Len(t, decided.GetRequestedChecks(), len(required.GetRequestedChecks()))
			require.Len(t, decided.GetMatchedGrants(), len(required.GetMatchedGrants()))
		})
	}
}

// Decide records nothing, allowed or refused: it answers a question, and the
// caller records the answer with Record when the access log should show it.
func TestDecide_recordsNothing(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_a")})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	d, err := engine.Decide(ctx, projectRead("proj_a"), projectRead("proj_b"))
	require.NoError(t, err)
	require.Equal(t, []bool{true, false}, allowedOf(d))

	require.Empty(t, listChallengeOutboxRows(t, conn))
}

func projectRead(id string) Check {
	return Check{Scope: ScopeProjectRead, ResourceID: id}
}

func allowedOf(d Decisions) []bool {
	allowed := make([]bool, 0, len(d.Each))
	for _, decision := range d.Each {
		allowed = append(allowed, decision.Allowed)
	}
	return allowed
}

func reasonsOf(d Decisions) []Reason {
	reasons := make([]Reason, 0, len(d.Each))
	for _, decision := range d.Each {
		reasons = append(reasons, decision.Reason)
	}
	return reasons
}

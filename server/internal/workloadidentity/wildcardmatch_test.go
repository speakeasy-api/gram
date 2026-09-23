package workloadidentity_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// A Claude Tag subject: the organization is fixed, the trailing agent id is
// minted per Slack channel and is what a wildcard rule covers.
const (
	fleetStem     = "wimse://identity.anthropic.com/org/org-abc/agent/"
	fleetRule     = fleetStem + "*"
	channelOne    = fleetStem + "agent-111"
	channelTwo    = fleetStem + "agent-222"
	otherOrgAgent = "wimse://identity.anthropic.com/org/org-zzz/agent/agent-111"
)

// seedAdmissionRule writes an admission with a match kind. Raw SQL for the
// same reason seedAdmission uses it: writes belong to the management API.
func seedAdmissionRule(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.NullUUID, issuerID uuid.UUID, subject string, kind workloadidentity.MatchKind) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: see seedAdmission
		t.Context(), `
		INSERT INTO workload_identity_admissions
		  (organization_id, project_id, workload_issuer_id, subject, match_kind)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, organizationID, projectID, issuerID, subject, string(kind)).Scan(&id)
	require.NoError(t, err)

	return id
}

func seedAssignmentRule(t *testing.T, conn *pgxpool.Pool, organizationID string, issuerID uuid.UUID, subject string, kind workloadidentity.MatchKind, agentID uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: see seedAssignment
		t.Context(), `
		INSERT INTO workload_agent_assignments
		  (organization_id, workload_issuer_id, subject, match_kind, agent_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, organizationID, issuerID, subject, string(kind), agentID).Scan(&id)
	require.NoError(t, err)

	return id
}

func allowWildcardAdmission(t *testing.T, conn *pgxpool.Pool, issuerID uuid.UUID, allow bool) {
	t.Helper()

	_, err := conn.Exec( //nolint:glint // notestingrawsql: see seedIssuer
		t.Context(), `UPDATE workload_issuers SET allow_wildcard_admission = $2 WHERE id = $1`, issuerID, allow)
	require.NoError(t, err)
}

func TestIsAdmitted_WildcardAdmitsEveryChannelUnderIt(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	seedAdmissionRule(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, fleetRule, workloadidentity.MatchKindWildcard)

	// The point of the feature: neither agent id was known when the rule was
	// written, and a recreated channel needs no new row.
	for _, subject := range []string{channelOne, channelTwo} {
		params := f.params()
		params.Subject = subject
		admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
		require.NoError(t, err)
		require.True(t, admitted, "subject %q should be admitted by the wildcard", subject)
	}
}

func TestIsAdmitted_WildcardDoesNotReachAnotherTenantOfTheSharedIssuer(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	seedAdmissionRule(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, fleetRule, workloadidentity.MatchKindWildcard)

	// Every Anthropic organization's tokens come from the same issuer, so this is
	// the check that the wildcard pins one of them rather than trusting the issuer.
	params := f.params()
	params.Subject = otherOrgAgent
	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, admitted)
}

// Clearing the issuer's permission must revoke wildcard rules already written, not
// merely stop new ones being created.
func TestIsAdmitted_WildcardIsInertWhenTheIssuerDoesNotPermitIt(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	seedAdmissionRule(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, fleetRule, workloadidentity.MatchKindWildcard)

	params := f.params()
	params.Subject = channelOne
	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, admitted)

	allowWildcardAdmission(t, conn, f.issuerID, false)

	admitted, err = workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, admitted, "clearing allow_wildcard_admission must revoke the existing wildcard rule")
}

// An exact row must keep matching exactly, so widening the query did not turn
// every stored subject into a wildcard.
func TestIsAdmitted_ExactStillRequiresTheWholeSubject(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	seedAdmissionRule(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, fleetStem, workloadidentity.MatchKindExact)

	params := f.params()
	params.Subject = channelOne
	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, admitted, "an exact rule holding the wildcard must not admit a longer subject")

	params.Subject = fleetStem
	admitted, err = workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, admitted)
}

func TestResolveAssignedAgent_WildcardAssignmentCoversTheFleet(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, fleetRule, workloadidentity.MatchKindWildcard, f.agentID)

	// The POC shape: every Claude Tag channel maps to one Gram agent.
	for _, subject := range []string{channelOne, channelTwo} {
		params := f.params()
		params.Subject = subject
		agentID, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
		require.NoError(t, err)
		require.True(t, ok, "subject %q should resolve through the wildcard", subject)
		require.Equal(t, f.agentID, agentID)
	}
}

// Most specific wins, which is what lets one channel be pinned to a different
// agent while the rest of the fleet keeps the default.
func TestResolveAssignedAgent_ExactOverridesWildcard(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	pinned := seedAgent(t, conn, f.tenant.organizationID)

	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, fleetRule, workloadidentity.MatchKindWildcard, f.agentID)
	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, channelOne, workloadidentity.MatchKindExact, pinned)

	params := f.params()
	params.Subject = channelOne
	agentID, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, pinned, agentID, "the exact assignment must win over the wildcard")

	params.Subject = channelTwo
	agentID, ok, err = workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, f.agentID, agentID, "an unpinned channel keeps the fleet default")
}

func TestResolveAssignedAgent_LongerWildcardWins(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	narrower := seedAgent(t, conn, f.tenant.organizationID)

	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, fleetRule, workloadidentity.MatchKindWildcard, f.agentID)
	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, fleetStem+"agent-1*", workloadidentity.MatchKindWildcard, narrower)

	params := f.params()
	params.Subject = channelOne
	agentID, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, narrower, agentID)

	params.Subject = channelTwo
	agentID, ok, err = workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, f.agentID, agentID)
}

// Admission and assignment have to agree on the issuer's permission, or a
// subject admitted by wildcard resolves to no agent and is refused for the wrong
// reason.
func TestResolveAssignedAgent_WildcardIsInertWhenTheIssuerDoesNotPermitIt(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, fleetRule, workloadidentity.MatchKindWildcard, f.agentID)

	params := f.params()
	params.Subject = channelOne
	_, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateSubjectRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		kind          workloadidentity.MatchKind
		subject       string
		allowWildcard bool
		wantErr       error
	}{
		{name: "exact is always allowed", kind: workloadidentity.MatchKindExact, subject: channelOne, allowWildcard: false, wantErr: nil},
		{name: "exact refuses a star, since the author meant a wildcard", kind: workloadidentity.MatchKindExact, subject: fleetRule, allowWildcard: true, wantErr: workloadidentity.ErrExactSubjectHasWildcard},
		{name: "wildcard needs the issuer to permit it", kind: workloadidentity.MatchKindWildcard, subject: fleetRule, allowWildcard: false, wantErr: workloadidentity.ErrWildcardNotPermitted},
		{name: "wildcard is allowed when the issuer permits it", kind: workloadidentity.MatchKindWildcard, subject: fleetRule, allowWildcard: true, wantErr: nil},

		// The whole point of requiring the terminator: a bare stem would match
		// the same subjects without saying so.
		{name: "a wildcard without its star is refused", kind: workloadidentity.MatchKindWildcard, subject: fleetStem, allowWildcard: true, wantErr: workloadidentity.ErrWildcardSuffixRequired},

		// An interior star would allow matching a suffix while leaving the middle
		// open, which is strictly more dangerous than a stem.
		{name: "an interior star is refused", kind: workloadidentity.MatchKindWildcard, subject: "wimse://identity.anthropic.com/org/*/agent/*", allowWildcard: true, wantErr: workloadidentity.ErrWildcardNotTerminal},

		// A bare star would admit every subject the issuer signs, which on a
		// shared issuer is every other tenant.
		{name: "a bare star is refused", kind: workloadidentity.MatchKindWildcard, subject: "*", allowWildcard: true, wantErr: workloadidentity.ErrWildcardStemEmpty},

		{name: "empty subject matches nothing", kind: workloadidentity.MatchKindExact, subject: "", allowWildcard: true, wantErr: workloadidentity.ErrSubjectEmpty},
		{name: "unknown kind", kind: workloadidentity.MatchKind("regex"), subject: fleetRule, allowWildcard: true, wantErr: workloadidentity.ErrMatchKindUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := workloadidentity.ValidateSubjectRule(tc.kind, tc.subject, tc.allowWildcard)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestWildcardStem(t *testing.T) {
	t.Parallel()

	// Mirrors what the lookup queries compute in SQL, so the two cannot drift.
	require.Equal(t, fleetStem, workloadidentity.WildcardStem(fleetRule))
}

func TestParseMatchKind(t *testing.T) {
	t.Parallel()

	// Empty is exact, matching the column default, so a caller that does not
	// care about matching does not have to name it.
	for _, value := range []string{"", "exact"} {
		kind, err := workloadidentity.ParseMatchKind(value)
		require.NoError(t, err)
		require.Equal(t, workloadidentity.MatchKindExact, kind)
	}

	kind, err := workloadidentity.ParseMatchKind("wildcard")
	require.NoError(t, err)
	require.Equal(t, workloadidentity.MatchKindWildcard, kind)

	_, err = workloadidentity.ParseMatchKind("glob")
	require.ErrorIs(t, err, workloadidentity.ErrMatchKindUnknown)
}

package workloadidentity_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// A Claude Tag subject: the organization is fixed, the trailing agent id is
// minted per Slack channel and is what a prefix rule covers.
const (
	prefixRule    = "wimse://identity.anthropic.com/org/org-abc/agent/"
	channelOne    = prefixRule + "agent-111"
	channelTwo    = prefixRule + "agent-222"
	otherOrgAgent = "wimse://identity.anthropic.com/org/org-zzz/agent/agent-111"
)

// seedPrefixAdmission writes an admission with a match kind. Raw SQL for the
// same reason seedAdmission uses it: writes belong to the management API.
func seedPrefixAdmission(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.NullUUID, issuerID uuid.UUID, subject string, kind workloadidentity.MatchKind) uuid.UUID {
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

func seedPrefixAssignment(t *testing.T, conn *pgxpool.Pool, organizationID string, issuerID uuid.UUID, subject string, kind workloadidentity.MatchKind, agentID uuid.UUID) uuid.UUID {
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

func allowPrefixAdmission(t *testing.T, conn *pgxpool.Pool, issuerID uuid.UUID, allow bool) {
	t.Helper()

	_, err := conn.Exec( //nolint:glint // notestingrawsql: see seedIssuer
		t.Context(), `UPDATE workload_issuers SET allow_prefix_admission = $2 WHERE id = $1`, issuerID, allow)
	require.NoError(t, err)
}

func TestIsAdmitted_PrefixAdmitsEveryChannelUnderIt(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	seedPrefixAdmission(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, prefixRule, workloadidentity.MatchKindPrefix)

	// The point of the feature: neither agent id was known when the rule was
	// written, and a recreated channel needs no new row.
	for _, subject := range []string{channelOne, channelTwo} {
		params := f.params()
		params.Subject = subject
		admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
		require.NoError(t, err)
		require.True(t, admitted, "subject %q should be admitted by the prefix", subject)
	}
}

func TestIsAdmitted_PrefixDoesNotReachAnotherTenantOfTheSharedIssuer(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	seedPrefixAdmission(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, prefixRule, workloadidentity.MatchKindPrefix)

	// Every Anthropic organization's tokens come from the same issuer, so this is
	// the check that the prefix pins one of them rather than trusting the issuer.
	params := f.params()
	params.Subject = otherOrgAgent
	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, admitted)
}

// Clearing the issuer's permission must revoke prefix rules already written, not
// merely stop new ones being created.
func TestIsAdmitted_PrefixIsInertWhenTheIssuerDoesNotPermitIt(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	seedPrefixAdmission(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, prefixRule, workloadidentity.MatchKindPrefix)

	params := f.params()
	params.Subject = channelOne
	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, admitted)

	allowPrefixAdmission(t, conn, f.issuerID, false)

	admitted, err = workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, admitted, "clearing allow_prefix_admission must revoke the existing prefix rule")
}

// An exact row must keep matching exactly, so widening the query did not turn
// every stored subject into a prefix.
func TestIsAdmitted_ExactStillRequiresTheWholeSubject(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAdmissionFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	seedPrefixAdmission(t, conn, f.tenant.organizationID, organizationTier(), f.issuerID, prefixRule, workloadidentity.MatchKindExact)

	params := f.params()
	params.Subject = channelOne
	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, admitted, "an exact rule holding the prefix must not admit a longer subject")

	params.Subject = prefixRule
	admitted, err = workloadidentity.IsAdmitted(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, admitted)
}

func TestResolveAssignedAgent_PrefixAssignmentCoversTheFleet(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	seedPrefixAssignment(t, conn, f.tenant.organizationID, f.issuerID, prefixRule, workloadidentity.MatchKindPrefix, f.agentID)

	// The POC shape: every Claude Tag channel maps to one Gram agent.
	for _, subject := range []string{channelOne, channelTwo} {
		params := f.params()
		params.Subject = subject
		agentID, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
		require.NoError(t, err)
		require.True(t, ok, "subject %q should resolve through the prefix", subject)
		require.Equal(t, f.agentID, agentID)
	}
}

// Most specific wins, which is what lets one channel be pinned to a different
// agent while the rest of the fleet keeps the default.
func TestResolveAssignedAgent_ExactOverridesPrefix(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	pinned := seedAgent(t, conn, f.tenant.organizationID)

	seedPrefixAssignment(t, conn, f.tenant.organizationID, f.issuerID, prefixRule, workloadidentity.MatchKindPrefix, f.agentID)
	seedPrefixAssignment(t, conn, f.tenant.organizationID, f.issuerID, channelOne, workloadidentity.MatchKindExact, pinned)

	params := f.params()
	params.Subject = channelOne
	agentID, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, pinned, agentID, "the exact assignment must win over the prefix")

	params.Subject = channelTwo
	agentID, ok, err = workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, f.agentID, agentID, "an unpinned channel keeps the fleet default")
}

func TestResolveAssignedAgent_LongerPrefixWins(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowPrefixAdmission(t, conn, f.issuerID, true)
	narrower := seedAgent(t, conn, f.tenant.organizationID)

	seedPrefixAssignment(t, conn, f.tenant.organizationID, f.issuerID, prefixRule, workloadidentity.MatchKindPrefix, f.agentID)
	seedPrefixAssignment(t, conn, f.tenant.organizationID, f.issuerID, prefixRule+"agent-1", workloadidentity.MatchKindPrefix, narrower)

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
// subject admitted by prefix resolves to no agent and is refused for the wrong
// reason.
func TestResolveAssignedAgent_PrefixIsInertWhenTheIssuerDoesNotPermitIt(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	seedPrefixAssignment(t, conn, f.tenant.organizationID, f.issuerID, prefixRule, workloadidentity.MatchKindPrefix, f.agentID)

	params := f.params()
	params.Subject = channelOne
	_, ok, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, params)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateSubjectRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		kind        workloadidentity.MatchKind
		subject     string
		allowPrefix bool
		wantErr     error
	}{
		{name: "exact is always allowed", kind: workloadidentity.MatchKindExact, subject: channelOne, allowPrefix: false, wantErr: nil},
		{name: "exact may contain a star, since a subject is opaque", kind: workloadidentity.MatchKindExact, subject: "weird*subject", allowPrefix: false, wantErr: nil},
		{name: "prefix needs the issuer to permit it", kind: workloadidentity.MatchKindPrefix, subject: prefixRule, allowPrefix: false, wantErr: workloadidentity.ErrPrefixNotPermitted},
		{name: "prefix is allowed when the issuer permits it", kind: workloadidentity.MatchKindPrefix, subject: prefixRule, allowPrefix: true, wantErr: nil},
		{name: "a glob is refused rather than stored as a literal", kind: workloadidentity.MatchKindPrefix, subject: prefixRule + "*", allowPrefix: true, wantErr: workloadidentity.ErrPrefixIsNotAGlob},
		{name: "empty subject matches nothing", kind: workloadidentity.MatchKindExact, subject: "", allowPrefix: true, wantErr: workloadidentity.ErrSubjectEmpty},
		{name: "unknown kind", kind: workloadidentity.MatchKind("regex"), subject: prefixRule, allowPrefix: true, wantErr: workloadidentity.ErrMatchKindUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := workloadidentity.ValidateSubjectRule(tc.kind, tc.subject, tc.allowPrefix)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
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

	kind, err := workloadidentity.ParseMatchKind("prefix")
	require.NoError(t, err)
	require.Equal(t, workloadidentity.MatchKindPrefix, kind)

	_, err = workloadidentity.ParseMatchKind("glob")
	require.ErrorIs(t, err, workloadidentity.ErrMatchKindUnknown)
}

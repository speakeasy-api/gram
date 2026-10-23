package workloadpolicy_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func admit(t *testing.T, ctx context.Context, ti *testInstance, subject string, matchKind string, agentID uuid.UUID) (*gen.WorkloadIdentityPolicy, error) {
	t.Helper()

	policy, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          subject,
		MatchKind:        matchKind,
		Name:             nil,
		Tags:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    false,
	})
	if err != nil {
		// Wrapped so wrapcheck is satisfied; every assertion on this error uses
		// errors.Is or errors.As, which both see through it.
		return nil, fmt.Errorf("admit subject: %w", err)
	}

	return policy, nil
}

func TestAdmitSubject_WildcardAdmitsTheFleetAndAssignsItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)

	// The onboarding this API exists for: the agent id is minted per Slack
	// channel and never shown, so an exact subject cannot be typed in advance.
	policy, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 1)
	admission := policy.Admissions[0]
	require.Equal(t, fleetRule, admission.Subject)
	require.Equal(t, string(workloadidentity.MatchKindWildcard), admission.MatchKind)
	require.Equal(t, anthropicIssuer, admission.Issuer)
	require.Equal(t, "Claude Tag", admission.IssuerName)
	// Admitted and assigned in one call: a subject with no agent is refused at
	// the token endpoint, so it must not be reachable from here.
	require.Equal(t, agentID.String(), admission.AgentID)
	require.Equal(t, "claude-tag-poc", admission.AgentName)
	require.True(t, admission.WildcardActive)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The snapshot has to carry the breadth of the grant, not just the subject:
	// a wildcard rule stands for every subject under its stem.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, fleetRule, snapshot["subject"])
	require.Equal(t, "wildcard", snapshot["match_kind"])
	require.Equal(t, agentID.String(), snapshot["assigned_agent_id"])
	require.Equal(t, "organization", snapshot["tier"])
}

func TestAdmitSubject_RefusesAWildcardTheIssuerDoesNotPermit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, false)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// Two gates, neither implied by the other. This is the early, legible
	// refusal; the lookup re-checks the same permission on every read.
	_, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorIs(t, err, workloadidentity.ErrWildcardNotPermitted)
}

func TestAdmitSubject_RefusesAStarInAnExactSubject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// An exact rule containing "*" stores a literal that matches nothing, and
	// then reads as correct in the list afterwards.
	_, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorIs(t, err, workloadidentity.ErrExactSubjectHasWildcard)
}

func TestAdmitSubject_RefusesAnUnregisteredIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// Resolve, do not validate: the lookup is tenancy-scoped, so an issuer the
	// caller cannot see is a not-found rather than a check after the fact.
	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdmitSubject_RefusesAnUnknownAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)

	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), uuid.New())
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdmitSubject_RefusesTheSameSubjectTwiceAtOneTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	require.NoError(t, err)

	_, err = admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAdmitSubject_RequiresWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	readOnly := withScopes(t, ctx, ti, authz.ScopeWorkloadRead)
	_, err := admit(t, readOnly, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAdmitSubject_RefusesTwoIssuersSharingAURLAtOneTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = asAPIKey(t, ctx)

	// The issuer column is not unique, and the per-tier name index lets two rows
	// in one project carry the same URL under different names. Choosing between
	// them would silently decide which jwks_uri verifies this subject and whether
	// wildcards are permitted for it, so it has to be a refusal — tier precedence
	// resolves across tiers, never within one.
	for _, name := range []string{"Claude Tag A", "Claude Tag B"} {
		_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
			SessionToken:           nil,
			ApikeyToken:            nil,
			ProjectSlugInput:       nil,
			Name:                   name,
			Issuer:                 anthropicIssuer,
			JwksURI:                anthropicJWKS,
			Description:            nil,
			AllowWildcardAdmission: new(false),
			ProjectScoped:          true,
		})
		require.NoError(t, err)
	}

	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             nil,
		Tags:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    true,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "same tier")
}

func admitWithTags(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID, tags []string) (*gen.WorkloadIdentityPolicy, error) {
	t.Helper()

	policy, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             nil,
		Tags:             tags,
		AgentID:          agentID.String(),
		ProjectScoped:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("admit subject: %w", err)
	}

	return policy, nil
}

func TestAdmitSubject_StoresTagsTrimmedAndDeduplicated(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	policy, err := admitWithTags(t, ctx, ti, agentID, []string{"  support  ", "slack", "support"})
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 1)
	require.Equal(t, []string{"support", "slack"}, policy.Admissions[0].Tags)
}

func TestAdmitSubject_RendersNoTagsAsAnEmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	policy, err := admitWithTags(t, ctx, ti, agentID, nil)
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 1)
	require.NotNil(t, policy.Admissions[0].Tags)
	require.Empty(t, policy.Admissions[0].Tags)
}

func TestAdmitSubject_RefusesABlankTag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := admitWithTags(t, ctx, ti, agentID, []string{"support", "   "})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestAdmitSubject_ReusesIssuerForDistinctAgents(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registered := registerAnthropic(t, ctx, ti, false)
	first := newAgent(t, ctx, ti, "first-workload")
	second := newAgent(t, ctx, ti, "second-workload")
	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), first)
	require.NoError(t, err)
	policy, err := admit(t, ctx, ti, fleetStem+"agent-222", string(workloadidentity.MatchKindExact), second)
	require.NoError(t, err)
	require.Len(t, policy.Issuers, 1)
	require.Equal(t, registered.Issuers[0].ID, policy.Issuers[0].ID)
	require.Len(t, policy.Admissions, 2)
	for _, admission := range policy.Admissions {
		expected := first
		if admission.Subject != channelOne {
			expected = second
		}
		require.Equal(t, expected.String(), admission.AgentID)
		require.Equal(t, expected, resolvedAgent(t, ctx, ti, admission))
	}
}

func TestAdmitSubject_LegacyAPIKeyRetainsAuthorization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registerAnthropic(t, ctx, ti, false)
	agent := newAgent(t, ctx, ti, "assigned-workload")
	writer := withScopes(t, ctx, ti, authz.ScopeWorkloadRead, authz.ScopeWorkloadWrite)
	_, err := admit(t, asAPIKey(t, writer), ti, channelOne, string(workloadidentity.MatchKindExact), agent)
	require.NoError(t, err)
}

func TestAdmitSubject_RevalidatesIssuerAfterLockWait(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registered := registerAnthropic(t, ctx, ti, true)
	agent := newAgent(t, ctx, ti, "assigned-workload")
	holder := testenv.BeginTx(t, ctx, ti.conn)
	affected, err := testrepo.New(holder).SetWorkloadIssuerWildcardAdmissionForTest(ctx, testrepo.SetWorkloadIssuerWildcardAdmissionForTestParams{OrganizationID: ti.orgID, ProjectID: uuid.NullUUID{}, ID: uuid.MustParse(registered.Issuers[0].ID), AllowWildcardAdmission: false})
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
	holderPID, err := testrepo.New(holder).BackendPIDFixture(ctx)
	require.NoError(t, err)

	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := admit(t, bounded, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agent)
		result <- err
	}()
	// Wait for the admission to read the old policy and block on our write,
	// rather than relying on a sleep to arrange the stale snapshot.
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(ti.conn).CountBackendsBlockedByFixture(ctx, holderPID)
		return err == nil && blocked > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, holder.Commit(ctx))
	requireOopsCode(t, <-result, oops.CodeInvalid)
	policy, err := ti.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Empty(t, policy.Admissions)
}

func TestAdmitSubject_OrganizationAdmissionCannotReuseProjectIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registerProjectGitHub(t, ctx, ti)
	agent := newAgent(t, ctx, ti, "assigned-workload")
	_, err := ti.service.AdmitSubject(asAPIKey(t, ctx), &gen.AdmitSubjectPayload{
		Issuer:        githubIssuer,
		Subject:       "repo:example/service:environment:production",
		MatchKind:     string(workloadidentity.MatchKindExact),
		AgentID:       agent.String(),
		ProjectScoped: false,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSubjectAssignment_OwnerAuthorization(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"admit", "update"} {
		for _, scenario := range []string{"owner", "workload-denied", "scope-override", "owner-latched", "other-owner", "other-owner-authorized", "other-owner-wrong-grant"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestService(t)
				registerAnthropic(t, ctx, ti, false)
				initial := newAgent(t, ctx, ti, "initial-workload")
				target := newAgent(t, ctx, ti, "target-workload")
				var admissionID string
				if operation == "update" {
					admissionID = admitLabelled(t, ctx, ti, initial).ID
				}
				principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, ti.orgID, ti.userID)
				require.NoError(t, err)
				grants, err := authz.LoadGrants(ctx, ti.conn, ti.orgID, principals)
				require.NoError(t, err)
				require.False(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeAgentAuthorize, ResourceKind: authz.ResourceKindAgent, ResourceID: target.String()}))
				seedSubjectAssignmentGrant(t, ctx, ti, authz.ScopeWorkloadRead, ti.orgID)
				seedSubjectAssignmentGrant(t, ctx, ti, authz.ScopeWorkloadWrite, ti.orgID)
				writer := withScopes(t, ctx, ti, authz.ScopeWorkloadRead, authz.ScopeWorkloadWrite)
				switch scenario {
				case "workload-denied":
					writer = withScopes(t, ctx, ti, authz.ScopeWorkloadRead, authz.ScopeWorkloadWrite, authz.ScopeWorkloadBlockedWrite)
				case "scope-override":
					writer = contextvalues.SetRBACScopeOverride(writer, "[]")
				case "owner-latched":
					require.NoError(t, testrepo.New(ti.conn).SetAgentOwnerLatchFixture(ctx, target))
				case "other-owner", "other-owner-authorized", "other-owner-wrong-grant":
					otherUser := "user_" + uuid.NewString()
					_, err = usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: otherUser, Email: otherUser + "@example.com", DisplayName: "Other owner"})
					require.NoError(t, err)
					_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: ti.orgID, UserID: conv.ToPGText(otherUser)})
					require.NoError(t, err)
					err = testrepo.New(ti.conn).SetAgentOwnerFixture(ctx, testrepo.SetAgentOwnerFixtureParams{OwnerUserID: otherUser, OrganizationID: ti.orgID, ID: target})
					require.NoError(t, err)
				}
				if scenario == "other-owner-wrong-grant" {
					seedSubjectAssignmentGrant(t, ctx, ti, authz.ScopeAgentAuthorize, initial.String())
				}
				if scenario == "other-owner-authorized" {
					seedSubjectAssignmentGrant(t, ctx, ti, authz.ScopeAgentAuthorize, target.String())
				}
				if operation == "admit" {
					_, err = admit(t, writer, ti, channelOne, string(workloadidentity.MatchKindExact), target)
				} else {
					payload := updateSubjectPayload(admissionID)
					payload.AgentID = new(target.String())
					_, err = ti.service.UpdateSubject(writer, payload)
				}
				if scenario == "owner" || scenario == "other-owner-authorized" {
					require.NoError(t, err)
				} else {
					requireOopsCode(t, err, oops.CodeForbidden)
				}
			})
		}
	}
}

// seedSubjectAssignmentGrant writes a live grant for the fixture's human caller.
// Unlike withScopes, it is visible to fresh membership/grant authorization.
func seedSubjectAssignmentGrant(t *testing.T, ctx context.Context, ti *testInstance, scope authz.Scope, resourceID string) {
	t.Helper()
	selectors, err := authz.NewSelector(scope, resourceID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ti.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, ti.userID),
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

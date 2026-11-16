package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// insertChatReadDenial records a denied chat:read check on one chat, the way
// the authorization engine logs it when a transcript is refused.
func insertChatReadDenial(t *testing.T, ti *testInstance, orgID, principalURN, chatID string) string {
	t.Helper()
	challengeID := uuid.NewString()
	insertCHChallengeRow(t, ti, orgID, challengeID, "deny", principalURN, string(authz.ScopeChatRead), authz.ResourceKindChat, chatID, nil, nil)
	return challengeID
}

func chatSelector(chatID string) *gen.Selector {
	return &gen.Selector{
		ResourceKind:    authz.ResourceKindChat,
		ResourceID:      chatID,
		ClientType:      nil,
		ConnectionOwner: nil,
		UpstreamClient:  nil,
		Disposition:     nil,
		Tool:            nil,
		ProjectID:       nil,
		ServerURL:       nil,
	}
}

func createChatReadGrantForChallenges(ctx context.Context, ti *testInstance, principalURN string, selector *gen.Selector, challengeIDs []string) (*gen.TemporaryGrant, error) {
	return ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{
		SessionToken: nil,
		PrincipalUrn: principalURN,
		Scope:        string(authz.ScopeChatRead),
		Selector:     selector,
		ExpiresAt:    inOneHour(),
		Reason:       new("Investigate a flagged finding"),
		ChallengeIds: challengeIDs,
	})
}

func listChallengesByIDs(t *testing.T, ctx context.Context, ti *testInstance, ids []string) []*gen.AuthzChallenge {
	t.Helper()
	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:       nil,
		PrincipalUrn:  nil,
		Scope:         nil,
		ExcludeScopes: nil,
		ProjectID:     nil,
		Resolved:      nil,
		Ids:           ids,
		From:          nil,
		To:            nil,
		Limit:         50,
		Offset:        0,
		ApikeyToken:   nil,
		SessionToken:  nil,
	})
	require.NoError(t, err)
	return result.Challenges
}

func requireNoResolutions(t *testing.T, ctx context.Context, ti *testInstance, orgID string, ids []string) {
	t.Helper()
	resolutions, err := accessrepo.New(ti.conn).ListChallengeResolutions(ctx, accessrepo.ListChallengeResolutionsParams{
		OrganizationID: orgID,
		ChallengeIds:   ids,
	})
	require.NoError(t, err)
	require.Empty(t, resolutions)
}

func TestTemporaryGrant_ResolvesAnsweredChallenges(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	chatID := uuid.NewString()
	first := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)
	second := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)

	resolveBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessChallengeResolve)
	require.NoError(t, err)

	grant, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), chatSelector(chatID), []string{first, second})
	require.NoError(t, err)
	require.Equal(t, "active", grant.Status)

	resolveAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessChallengeResolve)
	require.NoError(t, err)
	require.Equal(t, resolveBefore+2, resolveAfter, "each answered challenge is audited as resolved")

	challenges := listChallengesByIDs(t, ctx, ti, []string{first, second})
	require.Len(t, challenges, 2)
	for _, challenge := range challenges {
		require.NotNil(t, challenge.ResolutionType)
		require.Equal(t, "temporary_grant", *challenge.ResolutionType)
		require.Nil(t, challenge.ResolutionRoleSlug)
		require.NotNil(t, challenge.ResolutionTemporaryGrant)
		require.Equal(t, grant.ID, challenge.ResolutionTemporaryGrant.ID)
		require.Equal(t, "active", challenge.ResolutionTemporaryGrant.Status)
		require.NotNil(t, challenge.ResolutionTemporaryGrant.ExpiresAt)
		require.Equal(t, grant.ExpiresAt, *challenge.ResolutionTemporaryGrant.ExpiresAt)
	}

	expireTemporaryGrant(t, ctx, ti.conn, grant.ID)
	challenges = listChallengesByIDs(t, ctx, ti, []string{first})
	require.Len(t, challenges, 1)
	require.Equal(t, "expired", challenges[0].ResolutionTemporaryGrant.Status)
	require.NotNil(t, challenges[0].ResolutionTemporaryGrant.ExpiresAt)
}

func TestTemporaryGrant_ResolvedChallengeShowsRevokedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	chatID := uuid.NewString()
	challengeID := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)

	// A grant on every chat answers a denial on one of them.
	grant, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), nil, []string{challengeID})
	require.NoError(t, err)

	_, err = ti.service.RevokeTemporaryGrant(ctx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: grant.ID})
	require.NoError(t, err)

	challenges := listChallengesByIDs(t, ctx, ti, []string{challengeID})
	require.Len(t, challenges, 1)
	require.Equal(t, "temporary_grant", *challenges[0].ResolutionType)
	require.Equal(t, "revoked", challenges[0].ResolutionTemporaryGrant.Status)
	require.Nil(t, challenges[0].ResolutionTemporaryGrant.ExpiresAt)
}

func TestTemporaryGrant_BucketsCarryTemporaryGrantResolution(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	chatID := uuid.NewString()
	challengeID := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)
	grant, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), chatSelector(chatID), []string{challengeID})
	require.NoError(t, err)

	principal := recipient.String()
	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
		Outcome:      nil,
		PrincipalUrn: &principal,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        50,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Buckets, 1)
	bucket := result.Buckets[0]
	require.NotNil(t, bucket.ResolutionType)
	require.Equal(t, "temporary_grant", *bucket.ResolutionType)
	require.NotNil(t, bucket.ResolutionTemporaryGrant)
	require.Equal(t, grant.ID, bucket.ResolutionTemporaryGrant.ID)
	require.Equal(t, "active", bucket.ResolutionTemporaryGrant.Status)
}

func TestTemporaryGrant_RejectsChallengesItDoesNotAnswer(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)
	chatID := uuid.NewString()

	otherMember := insertChatReadDenial(t, ti, orgID, userPrincipalURN("someone_else"), chatID)
	otherChat := insertChatReadDenial(t, ti, orgID, recipient.String(), uuid.NewString())
	otherScope := uuid.NewString()
	insertCHChallengeRow(t, ti, orgID, otherScope, "deny", recipient.String(), string(authz.ScopeLogRead), authz.ResourceKindProject, uuid.NewString(), nil, nil)
	allowed := uuid.NewString()
	insertCHChallengeRow(t, ti, orgID, allowed, "allow", recipient.String(), string(authz.ScopeChatRead), authz.ResourceKindChat, chatID, nil, nil)
	legacy := uuid.NewString()
	insertCHChallengeRowWithSelector(t, ti, orgID, legacy, "deny", recipient.String(), string(authz.ScopeChatRead), authz.ResourceKindChat, chatID, "", nil, nil)
	otherOrg := uuid.NewString()
	insertCHChallengeRow(t, ti, "another-org", otherOrg, "deny", recipient.String(), string(authz.ScopeChatRead), authz.ResourceKindChat, chatID, nil, nil)

	cases := map[string]string{
		"raised by another principal":  otherMember,
		"on a chat the grant excludes": otherChat,
		"for another scope":            otherScope,
		"not a denial":                 allowed,
		"without a recorded selector":  legacy,
		"in another organization":      otherOrg,
	}
	for name, challengeID := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), chatSelector(chatID), []string{challengeID})
			requireOopsCode(t, err, oops.CodeBadRequest)
			requireNoResolutions(t, ctx, ti, orgID, []string{challengeID})
			// Every case is refused, so no case may leave a grant behind.
			require.Empty(t, listPrincipalGrants(t, ctx, ti.conn, orgID, recipient), "a rejected request gives no access")
		})
	}
}

func TestTemporaryGrant_AlreadyResolvedChallengeRollsBackGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)
	chatID := uuid.NewString()
	challengeID := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)

	resourceKind := authz.ResourceKindChat
	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:             nil,
		SessionToken:            nil,
		ChallengeIds:            []string{challengeID},
		PrincipalUrn:            recipient.String(),
		Scope:                   string(authz.ScopeChatRead),
		ResourceKind:            &resourceKind,
		ResourceID:              &chatID,
		ResolutionType:          "dismissed",
		RoleSlug:                nil,
		RoleAssignmentConfirmed: nil,
	})
	require.NoError(t, err)

	_, err = createChatReadGrantForChallenges(ctx, ti, recipient.String(), chatSelector(chatID), []string{challengeID})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, listPrincipalGrants(t, ctx, ti.conn, orgID, recipient), "the grant is not written when its challenges cannot be resolved")

	challenges := listChallengesByIDs(t, ctx, ti, []string{challengeID})
	require.Len(t, challenges, 1)
	require.Equal(t, "dismissed", *challenges[0].ResolutionType)
	require.Nil(t, challenges[0].ResolutionTemporaryGrant)
}

func TestTemporaryGrant_WithoutChallengesResolvesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)
	challengeID := insertChatReadDenial(t, ti, orgID, recipient.String(), uuid.NewString())

	_, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), nil, nil)
	require.NoError(t, err)
	requireNoResolutions(t, ctx, ti, orgID, []string{challengeID})
}

func TestListScopes_ReportsTemporaryGrantable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	result, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	grantable := make(map[string]bool, len(result.Scopes))
	for _, scope := range result.Scopes {
		grantable[scope.Slug] = scope.TemporaryGrantable
	}
	require.True(t, grantable[string(authz.ScopeChatRead)])
	require.True(t, grantable[string(authz.ScopeLogRead)])
	require.False(t, grantable[string(authz.ScopeOrgAdmin)])
	for _, scope := range temporaryGrantScopes {
		if _, listed := grantable[string(scope)]; listed {
			require.True(t, grantable[string(scope)], "%s can be given temporarily", scope)
		}
	}
}

func TestTemporaryGrant_RefusesChallengeAnExclusionStillBlocks(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	environmentID := uuid.NewString()
	seedGrant(t, ctx, ti.conn, orgID, recipient, authz.ScopeEnvironmentBlockedRead, environmentID)
	challengeID := uuid.NewString()
	insertCHChallengeRow(t, ti, orgID, challengeID, "deny", recipient.String(), string(authz.ScopeEnvironmentRead), authz.ResourceKindEnvironment, environmentID, nil, nil)

	_, err := ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{
		SessionToken: nil,
		PrincipalUrn: recipient.String(),
		Scope:        string(authz.ScopeEnvironmentRead),
		Selector:     nil,
		ExpiresAt:    inOneHour(),
		Reason:       nil,
		ChallengeIds: []string{challengeID},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	requireNoResolutions(t, ctx, ti, orgID, []string{challengeID})
	for _, grant := range listPrincipalGrants(t, ctx, ti.conn, orgID, recipient) {
		require.NotEqual(t, string(authz.ScopeEnvironmentRead), grant.Scope, "the grant is rolled back with the refused resolution")
	}
}

func TestTemporaryGrant_ChallengesOnlyWithAGrantToOneMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	challengeID := insertChatReadDenial(t, ti, orgID, userPrincipalURN("someone"), uuid.NewString())

	_, err := createChatReadGrantForChallenges(ctx, ti, "user:all", nil, []string{challengeID})
	requireOopsCode(t, err, oops.CodeBadRequest)
	requireNoResolutions(t, ctx, ti, orgID, []string{challengeID})
}

func TestTemporaryGrant_RestartedGrantLeavesEarlierResolutionExpired(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	chatID := uuid.NewString()
	first := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)
	grant, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), chatSelector(chatID), []string{first})
	require.NoError(t, err)
	expireTemporaryGrant(t, ctx, ti.conn, grant.ID)

	// The same access given again restarts the expired row under the same ID.
	second := insertChatReadDenial(t, ti, orgID, recipient.String(), chatID)
	restarted, err := createChatReadGrantForChallenges(ctx, ti, recipient.String(), chatSelector(chatID), []string{second})
	require.NoError(t, err)
	require.Equal(t, grant.ID, restarted.ID)

	byID := map[string]*gen.AuthzChallenge{}
	for _, challenge := range listChallengesByIDs(t, ctx, ti, []string{first, second}) {
		byID[challenge.ID] = challenge
	}
	require.Equal(t, "expired", byID[first].ResolutionTemporaryGrant.Status, "the earlier grant ended")
	require.Nil(t, byID[first].ResolutionTemporaryGrant.ExpiresAt, "the later grant's deadline is not the earlier one's")
	require.Equal(t, "active", byID[second].ResolutionTemporaryGrant.Status)
	require.NotNil(t, byID[second].ResolutionTemporaryGrant.ExpiresAt)
}

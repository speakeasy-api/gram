package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authz"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestResolveChallenge_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.ResolveChallenge(t.Context(), &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{uuid.NewString()},
		PrincipalUrn:   "user:test",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestResolveChallenge_Dismissed(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	challengeID := uuid.NewString()

	result, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   "user:denied-user",
		Scope:          "build:write",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Resolutions, 1)

	res := result.Resolutions[0]
	require.NotEmpty(t, res.ID)
	require.Equal(t, authCtx.ActiveOrganizationID, res.OrganizationID)
	require.Equal(t, challengeID, res.ChallengeID)
	require.Equal(t, "user:denied-user", res.PrincipalUrn)
	require.Equal(t, "build:write", res.Scope)
	require.Equal(t, "dismissed", res.ResolutionType)
	require.Contains(t, res.ResolvedBy, "user:")
	require.Nil(t, res.RoleSlug)
	require.Nil(t, res.ResourceKind)
	require.Nil(t, res.ResourceID)

	_, parseErr := time.Parse(time.RFC3339, res.CreatedAt)
	require.NoError(t, parseErr)
}

func TestResolveChallenge_RoleAssignedAddsCustomRoleAndPreservesExistingRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	userID := "denied-user"
	workosUserID := "workos-denied-user"
	membershipID := "membership-denied-user"
	principalURN := "user:" + userID

	existingRoleID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_existing", "Existing", "existing", "Existing access"))
	targetRoleID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_editor", "Editor", "editor", "Can read the organization"))
	seedGrant(t, ctx, ti.conn, orgID, seededRolePrincipal(t, ctx, ti.conn, orgID, "editor"), authz.ScopeOrgRead, orgID)
	seedConnectedUser(t, ctx, ti.conn, orgID, userID, "denied@example.test", "Denied User", workosUserID, membershipID)
	seedRoleAssignment(t, ctx, ti.conn, orgID, userID, mockMember(mockidp.MockOrgID, membershipID, workosUserID, "existing"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, membershipID, []string{"editor", "existing"}).Return(nil, nil).Once()

	challengeID := uuid.NewString()
	insertCHChallenge(t, ti, orgID, challengeID, "deny", principalURN, string(authz.ScopeOrgRead))
	roleSlug := "editor"
	resourceKind := authz.ResourceKindOrg

	result, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   principalURN,
		Scope:          string(authz.ScopeOrgRead),
		ResourceKind:   &resourceKind,
		ResourceID:     &orgID,
		ResolutionType: "role_assigned",
		RoleSlug:       &roleSlug,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Resolutions, 1)

	res := result.Resolutions[0]
	require.Equal(t, orgID, res.OrganizationID)
	require.Equal(t, challengeID, res.ChallengeID)
	require.Equal(t, "role_assigned", res.ResolutionType)
	require.NotNil(t, res.RoleSlug)
	require.Equal(t, "editor", *res.RoleSlug)

	members, err := ti.service.roleMgr.ListMembers(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, members.Members, 1)
	require.ElementsMatch(t, []string{existingRoleID, targetRoleID}, members.Members[0].RoleIds)
}

func TestResolveChallenge_RoleAssignedRejectsRoleThatDoesNotCoverResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	userID := "denied-user"
	workosUserID := "workos-denied-user"
	membershipID := "membership-denied-user"
	principalURN := "user:" + userID
	allowedProjectID := uuid.NewString()
	challengedProjectID := uuid.NewString()

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_reader", "Reader", "reader", "Reads one project"))
	seedGrant(t, ctx, ti.conn, orgID, seededRolePrincipal(t, ctx, ti.conn, orgID, "reader"), authz.ScopeProjectRead, allowedProjectID)
	seedConnectedUser(t, ctx, ti.conn, orgID, userID, "denied@example.test", "Denied User", workosUserID, membershipID)

	challengeID := uuid.NewString()
	insertCHChallengeRow(t, ti, orgID, challengeID, "deny", principalURN, string(authz.ScopeProjectRead), authz.ResourceKindProject, challengedProjectID, &userID, nil)
	roleSlug := "reader"
	resourceKind := authz.ResourceKindProject

	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   principalURN,
		Scope:          string(authz.ScopeProjectRead),
		ResourceKind:   &resourceKind,
		ResourceID:     &challengedProjectID,
		ResolutionType: "role_assigned",
		RoleSlug:       &roleSlug,
	})
	require.Error(t, err)

	resolutions, queryErr := accessrepo.New(ti.conn).ListChallengeResolutions(ctx, accessrepo.ListChallengeResolutionsParams{
		OrganizationID: orgID,
		ChallengeIds:   []string{challengeID},
	})
	require.NoError(t, queryErr)
	require.Empty(t, resolutions)

	members, listErr := ti.service.roleMgr.ListMembers(ctx, orgID)
	require.NoError(t, listErr)
	require.Len(t, members.Members, 1)
	require.Empty(t, members.Members[0].RoleIds)
}

func TestResolveChallenge_RoleAssignedRejectsLegacySelectorlessChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	userID := "legacy-denied-user"
	workosUserID := "workos-legacy-denied-user"
	membershipID := "membership-legacy-denied-user"
	principalURN := "user:" + userID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_reader", "Reader", "reader", "Reads the organization"))
	seedGrant(t, ctx, ti.conn, orgID, seededRolePrincipal(t, ctx, ti.conn, orgID, "reader"), authz.ScopeOrgRead, orgID)
	seedConnectedUser(t, ctx, ti.conn, orgID, userID, "legacy-denied@example.test", "Legacy Denied User", workosUserID, membershipID)

	challengeID := uuid.NewString()
	insertCHChallengeRowWithSelector(t, ti, orgID, challengeID, "deny", principalURN, string(authz.ScopeOrgRead), authz.ResourceKindOrg, orgID, "", &userID, nil)
	roleSlug := "reader"
	resourceKind := authz.ResourceKindOrg

	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   principalURN,
		Scope:          string(authz.ScopeOrgRead),
		ResourceKind:   &resourceKind,
		ResourceID:     &orgID,
		ResolutionType: "role_assigned",
		RoleSlug:       &roleSlug,
	})
	require.Error(t, err)

	resolutions, queryErr := accessrepo.New(ti.conn).ListChallengeResolutions(ctx, accessrepo.ListChallengeResolutionsParams{
		OrganizationID: orgID,
		ChallengeIds:   []string{challengeID},
	})
	require.NoError(t, queryErr)
	require.Empty(t, resolutions)
}

func TestResolveChallenge_WithResourceFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	challengeID := uuid.NewString()
	kind := "project"
	rid := "proj_abc"

	result, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   "user:denied-user",
		Scope:          "build:read",
		ResourceKind:   &kind,
		ResourceID:     &rid,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Resolutions, 1)

	res := result.Resolutions[0]
	require.NotNil(t, res.ResourceKind)
	require.Equal(t, "project", *res.ResourceKind)
	require.NotNil(t, res.ResourceID)
	require.Equal(t, "proj_abc", *res.ResourceID)
}

func TestResolveChallenge_RoleAssigned_MissingSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{uuid.NewString()},
		PrincipalUrn:   "user:test",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "role_assigned",
		RoleSlug:       nil, // missing!
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestResolveChallenge_Dismissed_WithSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	slug := "editor"

	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{uuid.NewString()},
		PrincipalUrn:   "user:test",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       &slug, // not allowed
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestResolveChallenge_DuplicateIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	challengeID := uuid.NewString()

	payload := &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   "user:denied-user",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	}

	// First resolve succeeds with 1 resolution.
	result, err := ti.service.ResolveChallenge(ctx, payload)
	require.NoError(t, err)
	require.Len(t, result.Resolutions, 1)

	// Second resolve with same challenge_id succeeds but returns 0 (already resolved, skipped).
	result, err = ti.service.ResolveChallenge(ctx, payload)
	require.NoError(t, err)
	require.Empty(t, result.Resolutions)
}

func TestResolveChallenge_MixedReplayIsRejectedAtomically(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	firstID := uuid.NewString()
	secondID := uuid.NewString()
	payload := func(ids []string) *gen.ResolveChallengePayload {
		return &gen.ResolveChallengePayload{
			ApikeyToken:    nil,
			SessionToken:   nil,
			ChallengeIds:   ids,
			PrincipalUrn:   "user:denied-user",
			Scope:          "org:read",
			ResourceKind:   nil,
			ResourceID:     nil,
			ResolutionType: "dismissed",
			RoleSlug:       nil,
		}
	}

	result, err := ti.service.ResolveChallenge(ctx, payload([]string{firstID}))
	require.NoError(t, err)
	require.Len(t, result.Resolutions, 1)

	_, err = ti.service.ResolveChallenge(ctx, payload([]string{firstID, secondID}))
	require.Error(t, err)

	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID
	resolutions, queryErr := accessrepo.New(ti.conn).ListChallengeResolutions(ctx, accessrepo.ListChallengeResolutionsParams{
		OrganizationID: orgID,
		ChallengeIds:   []string{firstID, secondID},
	})
	require.NoError(t, queryErr)
	require.Len(t, resolutions, 1)
	require.Equal(t, firstID, resolutions[0].ChallengeID)
}

func TestResolveChallenge_BatchMultipleIds(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}

	result, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   ids,
		PrincipalUrn:   "user:denied-user",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Resolutions, 3)

	resolvedIDs := make(map[string]bool)
	for _, res := range result.Resolutions {
		resolvedIDs[res.ChallengeID] = true
	}
	for _, id := range ids {
		require.True(t, resolvedIDs[id], "expected challenge %s to be resolved", id)
	}
}

func TestResolveChallenge_TransactionPersistsResolutionAndAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	userID := "denied-user"
	workosUserID := "workos-denied-user"
	membershipID := "membership-denied-user"
	principalURN := "user:" + userID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_existing", "Existing", "existing", "Existing access"))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_editor", "Editor", "editor", "Can read the organization"))
	seedGrant(t, ctx, ti.conn, orgID, seededRolePrincipal(t, ctx, ti.conn, orgID, "editor"), authz.ScopeOrgRead, orgID)
	seedConnectedUser(t, ctx, ti.conn, orgID, userID, "denied@example.test", "Denied User", workosUserID, membershipID)
	seedRoleAssignment(t, ctx, ti.conn, orgID, userID, mockMember(mockidp.MockOrgID, membershipID, workosUserID, "existing"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, membershipID, []string{"editor", "existing"}).Return(nil, nil).Once()

	challengeID := uuid.NewString()
	insertCHChallenge(t, ti, orgID, challengeID, "deny", principalURN, string(authz.ScopeOrgRead))
	roleSlug := "editor"
	resourceKind := authz.ResourceKindOrg

	// Resolve a challenge via the service (which uses a transaction internally).
	result, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   principalURN,
		Scope:          string(authz.ScopeOrgRead),
		ResourceKind:   &resourceKind,
		ResourceID:     &orgID,
		ResolutionType: "role_assigned",
		RoleSlug:       &roleSlug,
	})
	require.NoError(t, err)
	require.Len(t, result.Resolutions, 1)

	// Verify the resolution was persisted to the database (proves tx committed).
	rows, err := accessrepo.New(ti.conn).ListChallengeResolutions(ctx, accessrepo.ListChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   []string{challengeID},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "resolution must be persisted after transaction commit")

	row := rows[0]
	require.Equal(t, challengeID, row.ChallengeID)
	require.Equal(t, principalURN, row.PrincipalUrn)
	require.Equal(t, string(authz.ScopeOrgRead), row.Scope)
	require.Equal(t, "role_assigned", row.ResolutionType)
	require.True(t, row.RoleSlug.Valid)
	require.Equal(t, "editor", row.RoleSlug.String)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
}

func TestResolveChallenge_BatchPersistsAllResolutions(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}

	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   ids,
		PrincipalUrn:   "user:denied-user",
		Scope:          "build:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.NoError(t, err)

	// Verify all three resolutions were persisted in a single transaction.
	rows, err := accessrepo.New(ti.conn).ListChallengeResolutions(ctx, accessrepo.ListChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   ids,
	})
	require.NoError(t, err)
	require.Len(t, rows, 3, "all batch resolutions must be persisted atomically")

	persistedIDs := make(map[string]bool, len(rows))
	for _, r := range rows {
		persistedIDs[r.ChallengeID] = true
	}
	for _, id := range ids {
		require.True(t, persistedIDs[id], "challenge %s must be persisted", id)
	}
}

func TestResolveChallenge_EmptyIds(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	_, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeIds:   []string{},
		PrincipalUrn:   "user:test",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

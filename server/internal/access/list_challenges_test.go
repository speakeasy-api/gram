package access

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestListChallenges_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	// Call with a bare context (no auth context set).
	_, err := ti.service.ListChallenges(t.Context(), &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestListChallenges_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Challenges)
	require.Equal(t, 0, result.Total)
}

func TestListChallenges_ReturnsCHData(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	challengeID := uuid.NewString()
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, challengeID, "deny", "user:test-user", "org:admin")

	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)
	require.Equal(t, 1, result.Total)

	c := result.Challenges[0]
	require.Equal(t, challengeID, c.ID)
	require.Equal(t, authCtx.ActiveOrganizationID, c.OrganizationID)
	require.Equal(t, "user:test-user", c.PrincipalUrn)
	require.Equal(t, "deny", c.Outcome)
	require.Equal(t, "org:admin", c.Scope)

	// No resolution yet.
	require.Nil(t, c.ResolvedAt)
	require.Nil(t, c.ResolutionType)
	require.Nil(t, c.ResolvedBy)
	require.Nil(t, c.ResolutionRoleSlug)
}

func TestListChallenges_EnrichesWithResolution(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	challengeID := uuid.NewString()
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, challengeID, "deny", "user:u1", "build:write")

	// Insert a PG resolution for this challenge.
	_, err := accessrepo.New(ti.conn).InsertChallengeResolutions(ctx, accessrepo.InsertChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   []string{challengeID},
		PrincipalUrn:   "user:u1",
		Scope:          "build:write",
		ResourceKind:   "",
		ResourceID:     "",
		ResolutionType: "role_assigned",
		RoleSlug:       conv.PtrToPGText(conv.PtrEmpty("editor")),
		ResolvedBy:     "user:admin1",
	})
	require.NoError(t, err)

	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)

	c := result.Challenges[0]
	require.NotNil(t, c.ResolvedAt)
	_, parseErr := time.Parse(time.RFC3339, *c.ResolvedAt)
	require.NoError(t, parseErr)
	require.NotNil(t, c.ResolutionType)
	require.Equal(t, "role_assigned", *c.ResolutionType)
	require.NotNil(t, c.ResolvedBy)
	require.Equal(t, "user:admin1", *c.ResolvedBy)
	require.NotNil(t, c.ResolutionRoleSlug)
	require.Equal(t, "editor", *c.ResolutionRoleSlug)
}

func TestListChallenges_EnrichesWithUserData(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	userID := uuid.NewString()
	// Seed user in PG.
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       "alice@example.com",
		DisplayName: "Alice",
		PhotoUrl:    conv.PtrToPGText(conv.PtrEmpty("https://example.com/alice.jpg")),
		Admin:       false,
	})
	require.NoError(t, err)

	challengeID := uuid.NewString()
	insertCHChallengeWithUser(t, ti, authCtx.ActiveOrganizationID, challengeID, "deny", fmt.Sprintf("user:%s", userID), "org:read", &userID, nil)

	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)

	c := result.Challenges[0]
	require.NotNil(t, c.UserEmail)
	require.Equal(t, "alice@example.com", *c.UserEmail)
	require.NotNil(t, c.PhotoURL)
	require.Equal(t, "https://example.com/alice.jpg", *c.PhotoURL)
}

func TestListChallenges_FilterByOutcome(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	denyID := uuid.NewString()
	allowID := uuid.NewString()
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, denyID, "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, allowID, "allow", "user:u1", "org:read")

	outcome := "deny"
	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      &outcome,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)
	require.Equal(t, denyID, result.Challenges[0].ID)
}

func TestListChallenges_FilterByResolved(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	resolvedID := uuid.NewString()
	unresolvedID := uuid.NewString()
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, resolvedID, "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, unresolvedID, "deny", "user:u2", "org:admin")

	// Resolve only the first challenge.
	_, err := accessrepo.New(ti.conn).InsertChallengeResolutions(ctx, accessrepo.InsertChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   []string{resolvedID},
		PrincipalUrn:   "user:u1",
		Scope:          "org:read",
		ResourceKind:   "",
		ResourceID:     "",
		ResolutionType: "dismissed",
		RoleSlug:       conv.PtrToPGText(nil),
		ResolvedBy:     "user:admin1",
	})
	require.NoError(t, err)

	// Filter: resolved=true
	resolvedTrue := true
	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     &resolvedTrue,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)
	require.Equal(t, resolvedID, result.Challenges[0].ID)

	// Filter: resolved=false
	resolvedFalse := false
	result, err = ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     &resolvedFalse,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)
	require.Equal(t, unresolvedID, result.Challenges[0].ID)
}

func TestListChallenges_IsolatesByOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	myID := uuid.NewString()
	otherID := uuid.NewString()
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, myID, "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, "org-other-"+uuid.NewString(), otherID, "deny", "user:u1", "org:read")

	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 1)
	require.Equal(t, myID, result.Challenges[0].ID)
}

func TestListChallenges_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	for i := range 5 {
		insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", fmt.Sprintf("user:u%d", i), "org:read")
	}

	result, err := ti.service.ListChallenges(ctx, &gen.ListChallengesPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     nil,
		Limit:        2,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 2)
	require.Equal(t, 5, result.Total)
}

// --- challenge test helpers ---

// newChallengeTestService creates a test service with a unique org ID per test
// so CH data (shared table) doesn't leak between parallel tests.
func newChallengeTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx, ti := newTestAccessService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ActiveOrganizationID = "test-org-" + uuid.NewString()
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	// Seed org in PG so FK on authz_challenge_resolutions is satisfied.
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          authCtx.ActiveOrganizationID,
		Name:        "Test Org",
		Slug:        "test-org-" + uuid.NewString()[:8],
		WorkosID:    conv.PtrToPGText(nil),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return ctx, ti
}

func challengeAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	return authCtx
}

// insertCHChallenge inserts a minimal challenge row into ClickHouse for testing.
func insertCHChallenge(t *testing.T, ti *testInstance, orgID, challengeID, outcome, principalURN, scope string) {
	t.Helper()
	insertCHChallengeWithUser(t, ti, orgID, challengeID, outcome, principalURN, scope, nil, nil)
}

// insertCHChallengeWithUser inserts a challenge row with optional user enrichment fields.
func insertCHChallengeWithUser(t *testing.T, ti *testInstance, orgID, challengeID, outcome, principalURN, scope string, userID, userEmail *string) {
	t.Helper()

	err := ti.chConn.Exec(t.Context(), `
		INSERT INTO authz_challenges (
			id, timestamp, organization_id, project_id,
			trace_id, span_id,
			principal_urn, principal_type,
			user_id, user_email,
			role_slugs,
			operation, outcome, reason,
			scope, resource_kind, resource_id, selector,
			expanded_scopes,
			"requested_checks.scope", "requested_checks.resource_kind", "requested_checks.resource_id", "requested_checks.selector",
			"matched_grants.principal_urn", "matched_grants.scope", "matched_grants.selector", "matched_grants.matched_via_check_scope",
			evaluated_grant_count, filter_candidate_count, filter_allowed_count
		) VALUES (
			?, now64(9), ?, '',
			'00000000000000000000000000000000', '0000000000000000',
			?, 'user',
			?, ?,
			array(),
			'require', ?, 'no_grants',
			?, '', '', '',
			array(),
			array(), array(), array(), array(),
			array(), array(), array(), array(),
			0, 0, 0
		)`,
		challengeID, orgID,
		principalURN,
		userID, userEmail,
		outcome,
		scope,
	)
	require.NoError(t, err)
}

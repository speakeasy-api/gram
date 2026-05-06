package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestResolveChallenge_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.ResolveChallenge(t.Context(), &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeID:    uuid.NewString(),
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

	res, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeID:    challengeID,
		PrincipalUrn:   "user:denied-user",
		Scope:          "build:write",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

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

func TestResolveChallenge_RoleAssigned(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	challengeID := uuid.NewString()
	roleSlug := "editor"

	res, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeID:    challengeID,
		PrincipalUrn:   "user:denied-user",
		Scope:          "org:admin",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "role_assigned",
		RoleSlug:       &roleSlug,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Equal(t, authCtx.ActiveOrganizationID, res.OrganizationID)
	require.Equal(t, challengeID, res.ChallengeID)
	require.Equal(t, "role_assigned", res.ResolutionType)
	require.NotNil(t, res.RoleSlug)
	require.Equal(t, "editor", *res.RoleSlug)
}

func TestResolveChallenge_WithResourceFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	challengeID := uuid.NewString()
	kind := "project"
	rid := "proj_abc"

	res, err := ti.service.ResolveChallenge(ctx, &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeID:    challengeID,
		PrincipalUrn:   "user:denied-user",
		Scope:          "build:read",
		ResourceKind:   &kind,
		ResourceID:     &rid,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	})
	require.NoError(t, err)
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
		ChallengeID:    uuid.NewString(),
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
		ChallengeID:    uuid.NewString(),
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

func TestResolveChallenge_DuplicateConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	challengeID := uuid.NewString()

	payload := &gen.ResolveChallengePayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		ChallengeID:    challengeID,
		PrincipalUrn:   "user:denied-user",
		Scope:          "org:read",
		ResourceKind:   nil,
		ResourceID:     nil,
		ResolutionType: "dismissed",
		RoleSlug:       nil,
	}

	// First resolve succeeds.
	_, err := ti.service.ResolveChallenge(ctx, payload)
	require.NoError(t, err)

	// Second resolve with same challenge_id + org conflicts.
	_, err = ti.service.ResolveChallenge(ctx, payload)
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

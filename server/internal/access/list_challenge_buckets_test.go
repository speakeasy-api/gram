package access

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListChallengeBuckets_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.ListChallengeBuckets(t.Context(), &gen.ListChallengeBucketsPayload{
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

func TestListChallengeBuckets_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)

	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Empty(t, result.Buckets)
	require.Equal(t, 0, result.Total)
}

func TestListChallengeBuckets_GroupsByDimensions(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	// Insert 3 challenges with the same dimensions — should collapse into 1 bucket.
	for range 3 {
		insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", "user:u1", "org:admin")
	}

	// Insert 1 challenge with a different scope — separate bucket.
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", "user:u1", "build:read")

	// ClickHouse eventual consistency.
	time.Sleep(100 * time.Millisecond)

	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 2)

	// Find the org:admin bucket.
	var adminBucket *gen.ChallengeBucket
	for _, b := range result.Buckets {
		if b.Scope == "org:admin" {
			adminBucket = b
			break
		}
	}
	require.NotNil(t, adminBucket, "expected org:admin bucket")
	require.Equal(t, 3, adminBucket.ChallengeCount)
	require.Len(t, adminBucket.ChallengeIds, 3)
}

func TestListChallengeBuckets_FilterByOutcome(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "allow", "user:u1", "org:read")

	time.Sleep(100 * time.Millisecond)

	outcome := "deny"
	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 1)
	require.Equal(t, "deny", result.Buckets[0].Outcome)
}

func TestListChallengeBuckets_FilterByResolved(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	resolvedID := uuid.NewString()
	unresolvedID := uuid.NewString()
	// Different principals so they land in different buckets.
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, resolvedID, "deny", "user:resolved-user", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, unresolvedID, "deny", "user:unresolved-user", "org:read")

	// Resolve only the first.
	_, err := accessrepo.New(ti.conn).InsertChallengeResolutions(ctx, accessrepo.InsertChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   []string{resolvedID},
		PrincipalUrn:   "user:resolved-user",
		Scope:          "org:read",
		ResourceKind:   "",
		ResourceID:     "",
		ResolutionType: "dismissed",
		RoleSlug:       conv.PtrToPGText(nil),
		ResolvedBy:     "user:admin1",
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Filter: resolved=true
	resolvedTrue := true
	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 1)
	require.NotNil(t, result.Buckets[0].ResolvedAt)

	// Filter: resolved=false
	resolvedFalse := false
	result, err = ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 1)
	require.Equal(t, "user:unresolved-user", result.Buckets[0].PrincipalUrn)
}

func TestListChallengeBuckets_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	// 5 distinct principals = 5 buckets.
	for i := range 5 {
		insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", fmt.Sprintf("user:u%d", i), "org:read")
	}

	time.Sleep(100 * time.Millisecond)

	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 2)
	require.Equal(t, 5, result.Total)
}

func TestListChallengeBuckets_IsolatesByOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, "org-other-"+uuid.NewString(), uuid.NewString(), "deny", "user:u1", "org:read")

	time.Sleep(100 * time.Millisecond)

	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 1)
}

func TestListChallengeBuckets_AllChallengeIdsReturned(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	// Insert 15 challenges with the same dimensions.
	ids := make(map[string]bool, 15)
	for range 15 {
		id := uuid.NewString()
		ids[id] = true
		insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, id, "deny", "user:u1", "org:admin")
	}

	time.Sleep(100 * time.Millisecond)

	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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
	require.Len(t, result.Buckets, 1)
	require.Equal(t, 15, result.Buckets[0].ChallengeCount)
	require.Len(t, result.Buckets[0].ChallengeIds, 15)

	// Verify all inserted IDs are present.
	for _, cid := range result.Buckets[0].ChallengeIds {
		require.True(t, ids[cid], "unexpected challenge ID: %s", cid)
	}
}

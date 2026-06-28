package access

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		assert.Len(c, result.Buckets, 2)

		// Find the org:admin bucket.
		var adminBucket *gen.ChallengeBucket
		for _, b := range result.Buckets {
			if b.Scope == "org:admin" {
				adminBucket = b
				break
			}
		}
		if !assert.NotNil(c, adminBucket, "expected org:admin bucket") {
			return
		}
		assert.Equal(c, 3, adminBucket.ChallengeCount)
		assert.Len(c, adminBucket.ChallengeIds, 3)
	}, 10*time.Second, 100*time.Millisecond)
}

func TestListChallengeBuckets_FilterByOutcome(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "allow", "user:u1", "org:read")

	outcome := "deny"
	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		if !assert.Len(c, result.Buckets, 1) {
			return
		}
		assert.Equal(c, "deny", result.Buckets[0].Outcome)
	}, 10*time.Second, 100*time.Millisecond)
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

	// Filter: resolved=true
	resolvedTrue := true
	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		if !assert.Len(c, result.Buckets, 1) {
			return
		}
		assert.NotNil(c, result.Buckets[0].ResolvedAt)
	}, 10*time.Second, 100*time.Millisecond)

	// Filter: resolved=false
	resolvedFalse := false
	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		assert.Len(c, result.Buckets, 2)
		assert.Equal(c, 5, result.Total)
	}, 10*time.Second, 100*time.Millisecond)
}

// TestListChallengeBuckets_SuppressesUsersOutsideOrg mirrors the row-level test
// for the bucketed endpoint: member and unknown-principal buckets survive, the
// outside-org user's bucket is suppressed, and the total reflects the filter.
func TestListChallengeBuckets_SuppressesUsersOutsideOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID

	memberID := seedOrgMember(t, ctx, ti, orgID, "member@example.com")
	insertCHChallengeWithUser(t, ti, orgID, uuid.NewString(), "deny", "user:"+memberID, "org:read", &memberID, nil)

	insertCHChallengeWithUser(t, ti, orgID, uuid.NewString(), "deny", "api_key:ext", "org:read", nil, nil)

	outsiderID := seedNonMemberUser(t, ctx, ti, "staff@speakeasy.com")
	insertCHChallengeWithUser(t, ti, orgID, uuid.NewString(), "deny", "user:"+outsiderID, "org:read", &outsiderID, nil)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		urns := make(map[string]bool, len(result.Buckets))
		for _, b := range result.Buckets {
			urns[b.PrincipalUrn] = true
		}
		assert.Len(c, result.Buckets, 2)
		assert.Equal(c, 2, result.Total)
		assert.True(c, urns["user:"+memberID], "org member bucket should be present")
		assert.True(c, urns["api_key:ext"], "unknown principal bucket should be present")
		assert.False(c, urns["user:"+outsiderID], "outside-org user bucket should be suppressed")
	}, 10*time.Second, 100*time.Millisecond)
}

func TestListChallengeBuckets_IsolatesByOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, uuid.NewString(), "deny", "user:u1", "org:read")
	insertCHChallenge(t, ti, "org-other-"+uuid.NewString(), uuid.NewString(), "deny", "user:u1", "org:read")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		assert.Len(c, result.Buckets, 1)
	}, 10*time.Second, 100*time.Millisecond)
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
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
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}
		if !assert.Len(c, result.Buckets, 1) {
			return
		}
		assert.Equal(c, 15, result.Buckets[0].ChallengeCount)
		assert.Len(c, result.Buckets[0].ChallengeIds, 15)

		// Verify all inserted IDs are present.
		for _, cid := range result.Buckets[0].ChallengeIds {
			assert.True(c, ids[cid], "unexpected challenge ID: %s", cid)
		}
	}, 10*time.Second, 100*time.Millisecond)
}

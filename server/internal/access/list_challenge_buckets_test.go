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

func TestListChallengeBuckets_DeduplicatesRepeatedChallengeIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)
	challengeID := uuid.NewString()

	// Repeated physical rows are collapsed by stable challenge ID in reads.
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, challengeID, "allow", "user:u1", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, challengeID, "allow", "user:u1", "org:read")

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
		if !assert.NoError(c, err) || !assert.NotNil(c, result) {
			return
		}
		if !assert.Len(c, result.Buckets, 1) {
			return
		}
		assert.Equal(c, 1, result.Buckets[0].ChallengeCount)
		assert.Equal(c, []string{challengeID}, result.Buckets[0].ChallengeIds)
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
	unresolvedID2 := uuid.NewString()
	// Different principals so they land in different buckets.
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, resolvedID, "deny", "user:resolved-user", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, unresolvedID, "deny", "user:unresolved-user", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, unresolvedID2, "deny", "user:unresolved-user-2", "org:read")

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
		assert.Equal(c, 1, result.Total)
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
		Limit:        1,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Buckets, 1)
	require.Equal(t, 2, result.Total)
	require.Contains(t, []string{"user:unresolved-user", "user:unresolved-user-2"}, result.Buckets[0].PrincipalUrn)
}

func TestListChallengeBuckets_FilterByResolvedWithLargeResolutionSet(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	authCtx := challengeAuthContext(t, ctx)

	resolvedID := uuid.NewString()
	unresolvedID := uuid.NewString()
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, resolvedID, "deny", "user:resolved-user", "org:read")
	insertCHChallenge(t, ti, authCtx.ActiveOrganizationID, unresolvedID, "deny", "user:unresolved-user", "org:read")

	resolvedIDs := make([]string, 8_000)
	resolvedIDs[0] = resolvedID
	for i := 1; i < len(resolvedIDs); i++ {
		resolvedIDs[i] = uuid.NewString()
	}
	_, err := accessrepo.New(ti.conn).InsertChallengeResolutions(ctx, accessrepo.InsertChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   resolvedIDs,
		PrincipalUrn:   "user:resolved-user",
		Scope:          "org:read",
		ResourceKind:   "",
		ResourceID:     "",
		ResolutionType: "dismissed",
		RoleSlug:       conv.PtrToPGText(nil),
		ResolvedBy:     "user:admin1",
	})
	require.NoError(t, err)

	resolved := true
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
			Outcome:      nil,
			PrincipalUrn: nil,
			Scope:        nil,
			ProjectID:    nil,
			Resolved:     &resolved,
			Limit:        20,
			Offset:       0,
			ApikeyToken:  nil,
			SessionToken: nil,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.Len(c, result.Buckets, 1) {
			return
		}
		assert.Equal(c, 1, result.Total)
		assert.Equal(c, resolvedID, result.Buckets[0].ID)
	}, 10*time.Second, 100*time.Millisecond)
	stalePage, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     &resolved,
		Limit:        20,
		Offset:       10,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Empty(t, stalePage.Buckets)
	require.Equal(t, 1, stalePage.Total)

	resolved = false
	result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
		Outcome:      nil,
		PrincipalUrn: nil,
		Scope:        nil,
		ProjectID:    nil,
		Resolved:     &resolved,
		Limit:        20,
		Offset:       0,
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Buckets, 1)
	require.Equal(t, 1, result.Total)
	require.Equal(t, unresolvedID, result.Buckets[0].ID)
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

// TestListChallengeBuckets_ExcludesUnattributedBuckets covers the rows written
// by batch Filter/FindMatched calls: they carry no scope and no resource, so
// there is nothing to render and nothing to grant against. They must not reach
// the caller, and must not be counted in the total.
func TestListChallengeBuckets_ExcludesUnattributedBuckets(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID

	insertCHChallenge(t, ti, orgID, uuid.NewString(), "deny", "user:u1", "org:admin")
	insertCHChallengeUnattributed(t, ti, orgID, uuid.NewString(), "allow", "user:u2")

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
		assert.Equal(c, "org:admin", result.Buckets[0].Scope)
		assert.Equal(c, 1, result.Total)
	}, 10*time.Second, 100*time.Millisecond)
}

// TestListChallengeBuckets_UnattributedBucketsDoNotStarvePage reproduces the
// reported bug: unattributed rows are written on the hottest paths (project and
// toolset listing, MCP tools/list) so they are always the most recent buckets.
// When they are paginated as if they were displayable they fill the whole page
// and the caller is handed nothing to show, even though displayable buckets
// exist behind them.
func TestListChallengeBuckets_UnattributedBucketsDoNotStarvePage(t *testing.T) {
	t.Parallel()

	ctx, ti := newChallengeTestService(t)
	orgID := challengeAuthContext(t, ctx).ActiveOrganizationID

	// Older: the buckets a user actually wants to see.
	insertCHChallenge(t, ti, orgID, uuid.NewString(), "deny", "user:u1", "org:admin")
	insertCHChallenge(t, ti, orgID, uuid.NewString(), "allow", "user:u1", "org:read")

	// Newer: one unattributed bucket per principal, enough to fill a page.
	for i := range 3 {
		insertCHChallengeUnattributed(t, ti, orgID, uuid.NewString(), "allow", fmt.Sprintf("api_key:k%d", i))
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := ti.service.ListChallengeBuckets(ctx, &gen.ListChallengeBucketsPayload{
			Outcome:      nil,
			PrincipalUrn: nil,
			Scope:        nil,
			ProjectID:    nil,
			Resolved:     nil,
			Limit:        3,
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
		if !assert.Len(c, result.Buckets, 2) {
			return
		}
		for _, b := range result.Buckets {
			assert.NotEmpty(c, b.Scope)
			assert.NotNil(c, b.ResourceKind)
			assert.NotNil(c, b.ResourceID)
		}
		assert.Equal(c, 2, result.Total)
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

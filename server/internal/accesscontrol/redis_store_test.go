package accesscontrol

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizeMatchValue_URL(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindFullURL, " HTTPS://Example.COM/path/ ")
	if got != "https://example.com/path/" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "https://example.com/path/")
	}
}

func TestCanonicalizeMatchValue_FullURLStripsDefaultPortAndFragment(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindFullURL, "https://example.com:443/mcp#tools")
	if got != "https://example.com/mcp" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "https://example.com/mcp")
	}
}

func TestCanonicalizeMatchValue_FullURLSortsQueryKeys(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindFullURL, "https://example.com/mcp?z=last&a=first")
	if got != "https://example.com/mcp?a=first&z=last" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "https://example.com/mcp?a=first&z=last")
	}
}

func TestCanonicalizeMatchValue_URLHostExtractsDefaultPortHostFromURL(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindURLHost, "HTTPS://Example.COM:443/path")
	if got != "example.com" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "example.com")
	}
}

func TestCanonicalizeMatchValue_CommandWhitespace(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindServerIdentity, "  mcp__github__   ")
	if got != "mcp__github__" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "mcp__github__")
	}
}

func TestCanonicalizeMatchValue_ServerIdentityLowercases(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindServerIdentity, "  Linear MCP  ")
	if got != "linear mcp" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "linear mcp")
	}
}

func TestCanonicalizeMatchValue_UnknownKindCollapsesWhitespace(t *testing.T) {
	got := CanonicalizeMatchValue("command", "  run   this\ttool  ")
	if got != "run this tool" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "run this tool")
	}
}

func TestRedisStoreUpsertRequestIdempotentByFingerprint(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	request := testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	request.RequestFingerprint = "fingerprint-1"

	created, wasCreated, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, wasCreated)

	duplicate := testRequest("req-2", "proj-1", RequestStatusRequested, request.RequestedAt.Add(time.Minute))
	duplicate.RequestFingerprint = "fingerprint-1"
	got, wasCreated, err := store.UpsertRequest(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, wasCreated)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "fingerprint-1", got.RequestFingerprint)
}

func TestRedisStoreUpsertRequestIdempotentUpdatesBlockMetadata(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	firstBlockedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	lastBlockedAt := firstBlockedAt.Add(time.Minute)

	request := testRequest("req-1", "proj-1", RequestStatusRequested, firstBlockedAt)
	request.RequestFingerprint = "fingerprint-1"
	request.BlockedCount = 1
	request.FirstBlockedAt = firstBlockedAt
	request.LastBlockedAt = firstBlockedAt
	request.CreatedAt = firstBlockedAt
	request.UpdatedAt = firstBlockedAt
	created, wasCreated, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.Equal(t, 1, created.BlockedCount)
	require.Equal(t, firstBlockedAt, created.FirstBlockedAt)
	require.Equal(t, firstBlockedAt, created.LastBlockedAt)

	duplicate := testRequest("req-2", "proj-1", RequestStatusRequested, lastBlockedAt)
	duplicate.RequestFingerprint = "fingerprint-1"
	duplicate.BlockedCount = 1
	duplicate.FirstBlockedAt = lastBlockedAt
	duplicate.LastBlockedAt = lastBlockedAt
	duplicate.CreatedAt = lastBlockedAt
	duplicate.UpdatedAt = lastBlockedAt
	got, wasCreated, err := store.UpsertRequest(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, wasCreated)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, 2, got.BlockedCount)
	require.Equal(t, firstBlockedAt, got.FirstBlockedAt)
	require.Equal(t, lastBlockedAt, got.LastBlockedAt)
	require.Equal(t, firstBlockedAt, got.CreatedAt)
	require.Equal(t, lastBlockedAt, got.UpdatedAt)
}

func TestRedisStoreUpsertRequestIdempotentRefreshesMetadata(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	firstBlockedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	lastBlockedAt := firstBlockedAt.Add(time.Minute)
	originalToolName := "search"
	updatedToolName := "create_issue"
	updatedBlockReason := "risk policy denied tool"
	updatedRiskPolicyID := "6e4b83ed-b86f-4943-b15b-dfc698c25e4d"
	originalFullURL := "https://first.example.com/mcp"

	request := testRequest("req-1", "proj-1", RequestStatusRequested, firstBlockedAt)
	request.RequestFingerprint = "fingerprint-1"
	request.RequesterEmail = "first@example.com"
	request.RequesterDisplayName = "First Requester"
	request.DisplayName = "First Display"
	request.ObservedSummary.ToolName = &originalToolName
	request.ObservedSummary.FullURL = &originalFullURL
	created, wasCreated, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, wasCreated)

	duplicate := testRequest("req-2", "proj-1", RequestStatusRequested, lastBlockedAt)
	duplicate.RequestFingerprint = "fingerprint-1"
	duplicate.RequesterEmail = "second@example.com"
	duplicate.RequesterDisplayName = "Second Requester"
	duplicate.DisplayName = "Second Display"
	duplicate.ObservedSummary.ToolName = &updatedToolName
	duplicate.ObservedSummary.BlockReason = &updatedBlockReason
	duplicate.ObservedSummary.RiskPolicyID = &updatedRiskPolicyID
	got, wasCreated, err := store.UpsertRequest(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, wasCreated)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, firstBlockedAt, got.RequestedAt)
	require.Equal(t, firstBlockedAt, got.FirstBlockedAt)
	require.Equal(t, firstBlockedAt, got.CreatedAt)
	require.Equal(t, lastBlockedAt, got.LastBlockedAt)
	require.Equal(t, lastBlockedAt, got.UpdatedAt)
	require.Equal(t, "second@example.com", got.RequesterEmail)
	require.Equal(t, "Second Requester", got.RequesterDisplayName)
	require.Equal(t, "Second Display", got.DisplayName)
	require.Nil(t, got.ObservedSummary.FullURL)
	require.Equal(t, updatedToolName, *got.ObservedSummary.ToolName)
	require.Equal(t, updatedBlockReason, *got.ObservedSummary.BlockReason)
	require.Equal(t, updatedRiskPolicyID, *got.ObservedSummary.RiskPolicyID)
}

func TestRedisStoreUpsertRequestAllowsSameFingerprintForDifferentProject(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	request := testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	request.RequestFingerprint = "fingerprint-1"
	_, wasCreated, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, wasCreated)

	otherProject := testRequest("req-2", "proj-2", RequestStatusRequested, request.RequestedAt.Add(time.Minute))
	otherProject.RequestFingerprint = "fingerprint-1"
	got, wasCreated, err := store.UpsertRequest(ctx, otherProject)
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.Equal(t, "req-2", got.ID)
	require.Equal(t, "proj-2", got.ProjectID)
}

func TestRedisStoreUpsertRequestAllowsSameFingerprintForDifferentRequester(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	request := testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	request.RequestFingerprint = "fingerprint-1"
	_, wasCreated, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, wasCreated)

	otherRequester := testRequest("req-2", "proj-1", RequestStatusRequested, request.RequestedAt.Add(time.Minute))
	otherRequester.RequestFingerprint = "fingerprint-1"
	otherRequester.RequesterUserID = "requester-2"
	got, wasCreated, err := store.UpsertRequest(ctx, otherRequester)
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.Equal(t, "req-2", got.ID)
	require.Equal(t, "requester-2", got.RequesterUserID)
}

func TestRedisStoreUpsertRequestAllowsSameFingerprintAfterApproval(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	request := testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	request.RequestFingerprint = "fingerprint-1"
	_, wasCreated, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, wasCreated)

	_, err = store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1", RequestStatusApproved, "decider-1", "approved", []string{"rule-1"})
	require.NoError(t, err)

	nextRequest := testRequest("req-2", "proj-1", RequestStatusRequested, request.RequestedAt.Add(time.Minute))
	nextRequest.RequestFingerprint = "fingerprint-1"
	got, wasCreated, err := store.UpsertRequest(ctx, nextRequest)
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.Equal(t, "req-2", got.ID)
	require.Equal(t, RequestStatusRequested, got.Status)
}

func TestRedisStoreListRequestsFiltersAndSortsNewestFirst(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	otherOrganization := testRequest("other-organization", "proj-1", RequestStatusRequested, base.Add(5*time.Hour))
	otherOrganization.OrganizationID = "org-2"
	otherResourceType := testRequest("other-resource-type", "proj-1", RequestStatusRequested, base.Add(6*time.Hour))
	otherResourceType.ResourceType = "other_resource"
	for _, request := range []AccessApprovalRequest{
		testRequest("old", "proj-1", RequestStatusRequested, base),
		testRequest("new", "proj-1", RequestStatusRequested, base.Add(2*time.Hour)),
		testRequest("approved", "proj-1", RequestStatusApproved, base.Add(3*time.Hour)),
		testRequest("other-project", "proj-2", RequestStatusRequested, base.Add(4*time.Hour)),
		otherOrganization,
		otherResourceType,
	} {
		_, _, err := store.UpsertRequest(ctx, request)
		require.NoError(t, err)
	}

	got, err := store.ListRequests(ctx, RequestFilters{
		OrganizationID: "org-1",
		ProjectID:      "proj-1",
		ResourceType:   ResourceTypeShadowMCP,
		Status:         RequestStatusRequested,
		Limit:          1,
		Cursor:         "",
	})
	require.NoError(t, err)
	require.Equal(t, []AccessApprovalRequest{testRequest("new", "proj-1", RequestStatusRequested, base.Add(2*time.Hour))}, got.Requests)
	require.Equal(t, "new", got.NextCursor)

	got, err = store.ListRequests(ctx, RequestFilters{
		OrganizationID: "org-1",
		ProjectID:      "proj-1",
		ResourceType:   ResourceTypeShadowMCP,
		Status:         RequestStatusRequested,
		Limit:          1000,
		Cursor:         got.NextCursor,
	})
	require.NoError(t, err)
	require.Equal(t, []AccessApprovalRequest{testRequest("old", "proj-1", RequestStatusRequested, base)}, got.Requests)
	require.Empty(t, got.NextCursor)
}

func TestRedisStoreDecideRequestSetsDecisionFields(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	request := testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	_, _, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	deniedRequest := testRequest("req-2", "proj-1", RequestStatusRequested, request.RequestedAt.Add(time.Minute))
	_, _, err = store.UpsertRequest(ctx, deniedRequest)
	require.NoError(t, err)

	got, err := store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1", RequestStatusApproved, "user-1", "looks fine", []string{"rule-1", "rule-2"})
	require.NoError(t, err)
	require.Equal(t, RequestStatusApproved, got.Status)
	require.NotNil(t, got.DecidedAt)
	require.Equal(t, "user-1", got.DecidedBy)
	require.Equal(t, "looks fine", got.DecisionNote)
	require.Equal(t, []string{"rule-1", "rule-2"}, got.SourceRuleIDs)
	require.False(t, got.UpdatedAt.Equal(request.UpdatedAt))

	got, err = store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-2", RequestStatusDenied, "user-2", "not allowed", []string{"rule-3"})
	require.NoError(t, err)
	require.Equal(t, RequestStatusDenied, got.Status)
	require.NotNil(t, got.DecidedAt)
	require.Equal(t, "user-2", got.DecidedBy)
	require.Equal(t, "not allowed", got.DecisionNote)
	require.Equal(t, []string{"rule-3"}, got.SourceRuleIDs)
}

func TestRedisStoreDecideRequestRejectsSecondDecision(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	request := testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	_, _, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)

	approved, err := store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1", RequestStatusApproved, "user-1", "approved", []string{"rule-1"})
	require.NoError(t, err)
	require.Equal(t, RequestStatusApproved, approved.Status)

	_, err = store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1", RequestStatusDenied, "user-2", "denied", []string{"rule-2"})
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, err, ErrRequestAlreadyDecided)

	got, err := store.GetRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1")
	require.NoError(t, err)
	require.Equal(t, RequestStatusApproved, got.Status)
	require.Equal(t, "user-1", got.DecidedBy)
	require.Equal(t, "approved", got.DecisionNote)
	require.Equal(t, []string{"rule-1"}, got.SourceRuleIDs)
}

func TestRedisStoreDecideRequestWithRulesConflictLeavesRequestAndRulesUnchanged(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	request := testRequest("req-1", "proj-1", RequestStatusRequested, base)
	_, _, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)
	existing := testRule("existing", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "existing.example.com", base)
	_, err = store.CreateRule(ctx, existing)
	require.NoError(t, err)

	_, _, err = store.DecideRequestWithRules(
		ctx,
		"org-1",
		ResourceTypeShadowMCP,
		"req-1",
		RequestStatusApproved,
		"user-1",
		"approved",
		[]AccessRule{
			testRule("new", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "new.example.com", base.Add(time.Minute)),
			testRule("conflict", "proj-1", AccessScopeProject, DispositionDenied, MatchKindURLHost, "existing.example.com", base.Add(2*time.Minute)),
		},
	)
	require.ErrorIs(t, err, ErrConflict)

	gotRequest, err := store.GetRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1")
	require.NoError(t, err)
	require.Equal(t, RequestStatusRequested, gotRequest.Status)
	require.Empty(t, gotRequest.SourceRuleIDs)
	_, err = store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "new")
	require.ErrorIs(t, err, ErrNotFound)
	gotRule, err := store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "existing")
	require.NoError(t, err)
	require.Equal(t, existing, gotRule)
}

func TestRedisStoreDecideRequestWithRulesCommitsRequestAndRules(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	request := testRequest("req-1", "proj-1", RequestStatusRequested, base)
	_, _, err := store.UpsertRequest(ctx, request)
	require.NoError(t, err)

	decided, rules, err := store.DecideRequestWithRules(
		ctx,
		"org-1",
		ResourceTypeShadowMCP,
		"req-1",
		RequestStatusApproved,
		"user-1",
		"approved",
		[]AccessRule{
			testRule("rule-1", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "one.example.com", base.Add(time.Minute)),
			testRule("rule-2", "proj-2", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "two.example.com", base.Add(2*time.Minute)),
		},
	)
	require.NoError(t, err)
	require.Equal(t, RequestStatusApproved, decided.Status)
	require.Equal(t, []string{"rule-1", "rule-2"}, decided.SourceRuleIDs)
	require.Len(t, rules, 2)
	require.True(t, rules[0].Created)
	require.True(t, rules[1].Created)

	gotRequest, err := store.GetRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1")
	require.NoError(t, err)
	require.Equal(t, RequestStatusApproved, gotRequest.Status)
	_, err = store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "rule-1")
	require.NoError(t, err)
	_, err = store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "rule-2")
	require.NoError(t, err)

	_, _, err = store.DecideRequestWithRules(ctx, "org-1", ResourceTypeShadowMCP, "req-1", RequestStatusDenied, "user-2", "denied", nil)
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, err, ErrRequestAlreadyDecided)
}

func TestRedisStoreCreateRuleCanonicalizesAndRejectsDuplicate(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	rule := testRule("rule-1", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindFullURL, " HTTPS://Example.COM:443/mcp#tools ", time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))

	got, err := store.CreateRule(ctx, rule)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/mcp", got.MatchValue)

	duplicate := testRule("rule-2", "proj-1", AccessScopeProject, DispositionDenied, MatchKindFullURL, "https://example.com/mcp", got.CreatedAt.Add(time.Hour))
	_, err = store.CreateRule(ctx, duplicate)
	require.ErrorIs(t, err, ErrConflict)
}

func TestRedisStoreCreateRulesConflictLeavesNoEarlierRulesCommitted(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	existing := testRule("existing", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "existing.example.com", base)
	_, err := store.CreateRule(ctx, existing)
	require.NoError(t, err)

	_, err = store.CreateRules(ctx, []AccessRule{
		testRule("new", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "new.example.com", base.Add(time.Minute)),
		testRule("conflict", "proj-1", AccessScopeProject, DispositionDenied, MatchKindURLHost, "existing.example.com", base.Add(2*time.Minute)),
	})
	require.ErrorIs(t, err, ErrConflict)

	_, err = store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "new")
	require.ErrorIs(t, err, ErrNotFound)
	got, err := store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "existing")
	require.NoError(t, err)
	require.Equal(t, existing, got)
}

func TestRedisStoreListRulesFiltersAndSortsNewestFirst(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	for _, rule := range []AccessRule{
		testRule("old", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "old.example.com", base),
		testRule("new", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "new.example.com", base.Add(2*time.Hour)),
		testRule("denied", "proj-1", AccessScopeProject, DispositionDenied, MatchKindURLHost, "denied.example.com", base.Add(3*time.Hour)),
		testRule("org", "", AccessScopeOrganization, DispositionAllowed, MatchKindURLHost, "org.example.com", base.Add(4*time.Hour)),
		testRule("other-project", "proj-2", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "other.example.com", base.Add(5*time.Hour)),
	} {
		_, err := store.CreateRule(ctx, rule)
		require.NoError(t, err)
	}

	got, err := store.ListRules(ctx, RuleFilters{
		OrganizationID: "org-1",
		ProjectID:      "proj-1",
		AccessScope:    AccessScopeProject,
		ResourceType:   ResourceTypeShadowMCP,
		Disposition:    DispositionAllowed,
		Limit:          1000,
		Cursor:         "",
	})
	require.NoError(t, err)
	require.Equal(t, []AccessRule{
		testRule("new", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "new.example.com", base.Add(2*time.Hour)),
		testRule("old", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "old.example.com", base),
	}, got.Rules)
	require.Empty(t, got.NextCursor)
}

func TestRedisStoreListMatchingRulesScopesProjectAndOrganization(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	for _, rule := range []AccessRule{
		testRule("project", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "example.com", base.Add(time.Hour)),
		testRule("organization", "", AccessScopeOrganization, DispositionAllowed, MatchKindURLHost, "example.com", base),
		testRule("other-project", "proj-2", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "example.com", base.Add(2*time.Hour)),
	} {
		_, err := store.CreateRule(ctx, rule)
		require.NoError(t, err)
	}

	got, err := store.ListMatchingRules(ctx, MatchingRuleFilters{
		OrganizationID: "org-1",
		ProjectID:      "proj-1",
		ResourceType:   ResourceTypeShadowMCP,
		MatchKinds:     []string{MatchKindURLHost},
		MatchValues:    []string{"https://example.com:443/path"},
	})
	require.NoError(t, err)
	require.Equal(t, []AccessRule{
		testRule("project", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "example.com", base.Add(time.Hour)),
		testRule("organization", "", AccessScopeOrganization, DispositionAllowed, MatchKindURLHost, "example.com", base),
	}, got)
}

func TestRedisStoreDeleteRuleReturnsDeletedRuleAndRemovesIt(t *testing.T) {
	ctx := t.Context()
	store := newTestRedisStore()
	rule := testRule("rule-1", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "example.com", time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	created, err := store.CreateRule(ctx, rule)
	require.NoError(t, err)

	deleted, err := store.DeleteRule(ctx, "org-1", ResourceTypeShadowMCP, "rule-1")
	require.NoError(t, err)
	require.Equal(t, created, deleted)

	_, err = store.GetRule(ctx, "org-1", ResourceTypeShadowMCP, "rule-1")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStoreWriteUsesCacheMutate(t *testing.T) {
	ctx := t.Context()
	cache := newMemoryCache()
	store := NewRedisStore(cache, 0)
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	_, _, err := store.UpsertRequest(ctx, testRequest("req-1", "proj-1", RequestStatusRequested, base))
	require.NoError(t, err)
	_, err = store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-1", RequestStatusApproved, "user-1", "approved", nil)
	require.NoError(t, err)
	_, err = store.CreateRules(ctx, []AccessRule{testRule("rule-1", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "one.example.com", base)})
	require.NoError(t, err)
	_, err = store.GetOrCreateRules(ctx, []AccessRule{testRule("rule-1", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "one.example.com", base)})
	require.NoError(t, err)
	_, _, err = store.UpsertRequest(ctx, testRequest("req-2", "proj-1", RequestStatusRequested, base.Add(time.Minute)))
	require.NoError(t, err)
	_, _, err = store.DecideRequestWithRules(ctx, "org-1", ResourceTypeShadowMCP, "req-2", RequestStatusApproved, "user-1", "approved", []AccessRule{
		testRule("rule-2", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "two.example.com", base),
	})
	require.NoError(t, err)
	_, err = store.UpdateRule(ctx, testRule("rule-2", "proj-1", AccessScopeProject, DispositionAllowed, MatchKindURLHost, "updated.example.com", base))
	require.NoError(t, err)
	_, err = store.DeleteRule(ctx, "org-1", ResourceTypeShadowMCP, "rule-2")
	require.NoError(t, err)

	require.Equal(t, 8, cache.mutateCalls)
}

func TestRedisStoreWriteRequiresCacheMutate(t *testing.T) {
	ctx := t.Context()
	store := NewRedisStore(newNonMutatingMemoryCache(), 0)

	_, _, err := store.UpsertRequest(ctx, testRequest("req-1", "proj-1", RequestStatusRequested, time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)))
	require.ErrorContains(t, err, "access control cache does not support atomic mutation")
}

func newTestRedisStore() *RedisStore {
	return NewRedisStore(newMemoryCache(), 0)
}

func testRequest(id, projectID, status string, requestedAt time.Time) AccessApprovalRequest {
	return AccessApprovalRequest{
		ID:                   id,
		OrganizationID:       "org-1",
		ProjectID:            projectID,
		ResourceType:         ResourceTypeShadowMCP,
		Status:               status,
		RequesterUserID:      "requester-1",
		RequesterEmail:       "requester@example.com",
		RequesterDisplayName: "Requester",
		RequestFingerprint:   "fingerprint-" + id,
		DisplayName:          "Request " + id,
		ObservedSummary: ObservedSummary{
			Name:           nil,
			FullURL:        nil,
			URLHost:        nil,
			ServerIdentity: nil,
			ToolName:       nil,
			ToolCall:       nil,
			BlockReason:    nil,
			RiskPolicyID:   nil,
			RiskResultID:   nil,
		},
		BlockedCount:   1,
		FirstBlockedAt: requestedAt,
		LastBlockedAt:  requestedAt,
		RequestedAt:    requestedAt,
		DecidedAt:      nil,
		DecidedBy:      "",
		DecisionNote:   "",
		SourceRuleIDs:  nil,
		CreatedAt:      requestedAt,
		UpdatedAt:      requestedAt,
	}
}

func testRule(id, projectID, accessScope, disposition, matchKind, matchValue string, createdAt time.Time) AccessRule {
	return AccessRule{
		ID:             id,
		OrganizationID: "org-1",
		ProjectID:      projectID,
		AccessScope:    accessScope,
		ResourceType:   ResourceTypeShadowMCP,
		Disposition:    disposition,
		MatchKind:      matchKind,
		MatchValue:     matchValue,
		DisplayName:    "Rule " + id,
		ObservedSummary: ObservedSummary{
			Name:           nil,
			FullURL:        nil,
			URLHost:        nil,
			ServerIdentity: nil,
			ToolName:       nil,
			ToolCall:       nil,
			BlockReason:    nil,
			RiskPolicyID:   nil,
			RiskResultID:   nil,
		},
		SourceRequestID: "",
		CreatedBy:       "creator-1",
		UpdatedBy:       "updater-1",
		Reason:          "test",
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}
}

type memoryCache struct {
	mu          sync.Mutex
	values      map[string][]byte
	mutateCalls int
}

func newMemoryCache() *memoryCache {
	return &memoryCache{values: map[string][]byte{}}
}

func (c *memoryCache) Get(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.values[key]
	if !ok {
		return redisCache.ErrCacheMiss
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return err
	}
	return nil
}

func (c *memoryCache) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.Get(ctx, key, value); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, key)
	return nil
}

func (c *memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.values[key] = raw
	return nil
}

func (c *memoryCache) Update(ctx context.Context, key string, value any) error {
	c.mu.Lock()
	_, ok := c.values[key]
	c.mu.Unlock()
	if !ok {
		return redisCache.ErrCacheMiss
	}
	return c.Set(ctx, key, value, 0)
}

func (c *memoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, key)
	return nil
}

func (c *memoryCache) Mutate(_ context.Context, key string, value any, _ time.Duration, fn func(exists bool) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mutateCalls++

	raw, exists := c.values[key]
	if exists {
		if err := json.Unmarshal(raw, value); err != nil {
			return err
		}
	} else {
		valueOf := reflect.ValueOf(value)
		valueOf.Elem().Set(reflect.Zero(valueOf.Elem().Type()))
	}
	if err := fn(exists); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.values[key] = raw
	return nil
}

func (c *memoryCache) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (c *memoryCache) ListAppend(_ context.Context, _ string, _ any, _ time.Duration) error {
	return errors.New("not implemented")
}

func (c *memoryCache) ListRange(_ context.Context, _ string, _, _ int64, _ any) error {
	return errors.New("not implemented")
}

func (c *memoryCache) DeleteByPrefix(_ context.Context, prefix string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.values {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.values, key)
		}
	}
	return nil
}

type nonMutatingMemoryCache struct {
	inner *memoryCache
}

func newNonMutatingMemoryCache() *nonMutatingMemoryCache {
	return &nonMutatingMemoryCache{inner: newMemoryCache()}
}

func (c *nonMutatingMemoryCache) Get(ctx context.Context, key string, value any) error {
	return c.inner.Get(ctx, key, value)
}

func (c *nonMutatingMemoryCache) GetAndDelete(ctx context.Context, key string, value any) error {
	return c.inner.GetAndDelete(ctx, key, value)
}

func (c *nonMutatingMemoryCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return c.inner.Set(ctx, key, value, ttl)
}

func (c *nonMutatingMemoryCache) Update(ctx context.Context, key string, value any) error {
	return c.inner.Update(ctx, key, value)
}

func (c *nonMutatingMemoryCache) Delete(ctx context.Context, key string) error {
	return c.inner.Delete(ctx, key)
}

func (c *nonMutatingMemoryCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.inner.Expire(ctx, key, ttl)
}

func (c *nonMutatingMemoryCache) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	return c.inner.ListAppend(ctx, key, value, ttl)
}

func (c *nonMutatingMemoryCache) ListRange(ctx context.Context, key string, start, stop int64, value any) error {
	return c.inner.ListRange(ctx, key, start, stop, value)
}

func (c *nonMutatingMemoryCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	return c.inner.DeleteByPrefix(ctx, prefix)
}

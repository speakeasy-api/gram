package accesscontrol

import (
	"context"
	"encoding/json"
	"errors"
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

	got, err = store.DecideRequest(ctx, "org-1", ResourceTypeShadowMCP, "req-2", RequestStatusDenied, "user-2", "not allowed", []string{"rule-3"})
	require.NoError(t, err)
	require.Equal(t, RequestStatusDenied, got.Status)
	require.NotNil(t, got.DecidedAt)
	require.Equal(t, "user-2", got.DecidedBy)
	require.Equal(t, "not allowed", got.DecisionNote)
	require.Equal(t, []string{"rule-3"}, got.SourceRuleIDs)
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
		},
		RequestedAt:   requestedAt,
		DecidedAt:     nil,
		DecidedBy:     "",
		DecisionNote:  "",
		SourceRuleIDs: nil,
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
	values map[string][]byte
}

func newMemoryCache() *memoryCache {
	return &memoryCache{values: map[string][]byte{}}
}

func (c *memoryCache) Get(_ context.Context, key string, value any) error {
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
	delete(c.values, key)
	return nil
}

func (c *memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.values[key] = raw
	return nil
}

func (c *memoryCache) Update(ctx context.Context, key string, value any) error {
	if _, ok := c.values[key]; !ok {
		return redisCache.ErrCacheMiss
	}
	return c.Set(ctx, key, value, 0)
}

func (c *memoryCache) Delete(_ context.Context, key string) error {
	delete(c.values, key)
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
	for key := range c.values {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.values, key)
		}
	}
	return nil
}

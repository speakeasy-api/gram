package accesscontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

var (
	// ErrNotFound is returned when an access control request or rule does not exist.
	ErrNotFound = errors.New("access control record not found")
	// ErrConflict is returned when a rule duplicates an existing match target.
	ErrConflict = errors.New("access control record conflicts with an existing record")
)

// RedisStore persists access control state in Redis through the shared cache layer.
type RedisStore struct {
	cache cache.Cache
	ttl   time.Duration
}

type redisState struct {
	Requests []AccessApprovalRequest
	Rules    []AccessRule
}

var _ Store = (*RedisStore)(nil)

// NewRedisStore creates a Redis-backed Store using AlphaTTL when ttl is not positive.
func NewRedisStore(cacheImpl cache.Cache, ttl time.Duration) *RedisStore {
	if ttl <= 0 {
		ttl = AlphaTTL
	}
	return &RedisStore{
		cache: cacheImpl,
		ttl:   ttl,
	}
}

func (s *RedisStore) ListRequests(ctx context.Context, filters RequestFilters) (ListRequestsResult, error) {
	state, err := s.load(ctx, filters.OrganizationID, filters.ResourceType)
	if err != nil {
		return ListRequestsResult{}, err
	}

	rows := make([]AccessApprovalRequest, 0, len(state.Requests))
	for _, request := range state.Requests {
		if filters.Status != "" && request.Status != filters.Status {
			continue
		}
		if filters.ProjectID != "" && request.ProjectID != filters.ProjectID {
			continue
		}
		rows = append(rows, request)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].RequestedAt.After(rows[j].RequestedAt)
	})

	paged, nextCursor := pageByID(rows, filters.Cursor, normalizeLimit(filters.Limit), func(request AccessApprovalRequest) string {
		return request.ID
	})
	return ListRequestsResult{Requests: paged, NextCursor: nextCursor}, nil
}

func (s *RedisStore) UpsertRequest(ctx context.Context, request AccessApprovalRequest) (AccessApprovalRequest, bool, error) {
	state, err := s.load(ctx, request.OrganizationID, request.ResourceType)
	if err != nil {
		return AccessApprovalRequest{}, false, err
	}

	for _, existing := range state.Requests {
		if request.RequestFingerprint != "" &&
			existing.RequestFingerprint == request.RequestFingerprint &&
			existing.ProjectID == request.ProjectID &&
			existing.RequesterUserID == request.RequesterUserID &&
			existing.Status == RequestStatusRequested {
			return existing, false, nil
		}
	}

	if request.ID == "" {
		request.ID = uuid.NewString()
	}
	if request.Status == "" {
		request.Status = RequestStatusRequested
	}
	if request.RequestedAt.IsZero() {
		request.RequestedAt = time.Now().UTC()
	}

	state.Requests = append(state.Requests, request)
	if err := s.save(ctx, request.OrganizationID, request.ResourceType, state); err != nil {
		return AccessApprovalRequest{}, false, err
	}
	return request, true, nil
}

func (s *RedisStore) GetRequest(ctx context.Context, organizationID, resourceType, id string) (AccessApprovalRequest, error) {
	state, err := s.load(ctx, organizationID, resourceType)
	if err != nil {
		return AccessApprovalRequest{}, err
	}
	for _, request := range state.Requests {
		if request.ID == id {
			return request, nil
		}
	}
	return AccessApprovalRequest{}, ErrNotFound
}

func (s *RedisStore) DecideRequest(ctx context.Context, organizationID, resourceType, id, status, decidedBy, note string, sourceRuleIDs []string) (AccessApprovalRequest, error) {
	state, err := s.load(ctx, organizationID, resourceType)
	if err != nil {
		return AccessApprovalRequest{}, err
	}
	for i := range state.Requests {
		if state.Requests[i].ID != id {
			continue
		}

		now := time.Now().UTC()
		state.Requests[i].Status = status
		state.Requests[i].DecidedAt = &now
		state.Requests[i].DecidedBy = decidedBy
		state.Requests[i].DecisionNote = note
		state.Requests[i].SourceRuleIDs = slices.Clone(sourceRuleIDs)
		if err := s.save(ctx, organizationID, resourceType, state); err != nil {
			return AccessApprovalRequest{}, err
		}
		return state.Requests[i], nil
	}
	return AccessApprovalRequest{}, ErrNotFound
}

func (s *RedisStore) ListRules(ctx context.Context, filters RuleFilters) (ListRulesResult, error) {
	state, err := s.load(ctx, filters.OrganizationID, filters.ResourceType)
	if err != nil {
		return ListRulesResult{}, err
	}

	rows := make([]AccessRule, 0, len(state.Rules))
	for _, rule := range state.Rules {
		if filters.Disposition != "" && rule.Disposition != filters.Disposition {
			continue
		}
		if filters.AccessScope != "" && rule.AccessScope != filters.AccessScope {
			continue
		}
		if filters.ProjectID != "" && rule.ProjectID != filters.ProjectID {
			continue
		}
		rows = append(rows, rule)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})

	paged, nextCursor := pageByID(rows, filters.Cursor, normalizeLimit(filters.Limit), func(rule AccessRule) string {
		return rule.ID
	})
	return ListRulesResult{Rules: paged, NextCursor: nextCursor}, nil
}

func (s *RedisStore) CreateRule(ctx context.Context, rule AccessRule) (AccessRule, error) {
	state, err := s.load(ctx, rule.OrganizationID, rule.ResourceType)
	if err != nil {
		return AccessRule{}, err
	}

	rule.MatchValue = CanonicalizeMatchValue(rule.MatchKind, rule.MatchValue)
	for _, existing := range state.Rules {
		if sameRuleMatch(existing, rule) {
			return AccessRule{}, ErrConflict
		}
	}

	now := time.Now().UTC()
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	if rule.UpdatedAt.IsZero() {
		rule.UpdatedAt = rule.CreatedAt
	}

	state.Rules = append(state.Rules, rule)
	if err := s.save(ctx, rule.OrganizationID, rule.ResourceType, state); err != nil {
		return AccessRule{}, err
	}
	return rule, nil
}

func (s *RedisStore) UpdateRule(ctx context.Context, rule AccessRule) (AccessRule, error) {
	state, err := s.load(ctx, rule.OrganizationID, rule.ResourceType)
	if err != nil {
		return AccessRule{}, err
	}

	rule.MatchValue = CanonicalizeMatchValue(rule.MatchKind, rule.MatchValue)
	for _, existing := range state.Rules {
		if existing.ID == rule.ID {
			continue
		}
		if sameRuleMatch(existing, rule) {
			return AccessRule{}, ErrConflict
		}
	}
	for i := range state.Rules {
		if state.Rules[i].ID != rule.ID {
			continue
		}
		if rule.CreatedAt.IsZero() {
			rule.CreatedAt = state.Rules[i].CreatedAt
		}
		if rule.UpdatedAt.IsZero() {
			rule.UpdatedAt = time.Now().UTC()
		}
		state.Rules[i] = rule
		if err := s.save(ctx, rule.OrganizationID, rule.ResourceType, state); err != nil {
			return AccessRule{}, err
		}
		return rule, nil
	}
	return AccessRule{}, ErrNotFound
}

func (s *RedisStore) DeleteRule(ctx context.Context, organizationID, resourceType, id string) (AccessRule, error) {
	state, err := s.load(ctx, organizationID, resourceType)
	if err != nil {
		return AccessRule{}, err
	}
	for i, rule := range state.Rules {
		if rule.ID != id {
			continue
		}
		state.Rules = slices.Delete(state.Rules, i, i+1)
		if err := s.save(ctx, organizationID, resourceType, state); err != nil {
			return AccessRule{}, err
		}
		return rule, nil
	}
	return AccessRule{}, ErrNotFound
}

func (s *RedisStore) GetRule(ctx context.Context, organizationID, resourceType, id string) (AccessRule, error) {
	state, err := s.load(ctx, organizationID, resourceType)
	if err != nil {
		return AccessRule{}, err
	}
	for _, rule := range state.Rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return AccessRule{}, ErrNotFound
}

func (s *RedisStore) GetRuleByMatch(ctx context.Context, organizationID, resourceType, accessScope, projectID, matchKind, matchValue string) (AccessRule, error) {
	state, err := s.load(ctx, organizationID, resourceType)
	if err != nil {
		return AccessRule{}, err
	}
	matchValue = CanonicalizeMatchValue(matchKind, matchValue)
	for _, rule := range state.Rules {
		if rule.AccessScope == accessScope &&
			rule.ProjectID == projectID &&
			rule.MatchKind == matchKind &&
			rule.MatchValue == matchValue {
			return rule, nil
		}
	}
	return AccessRule{}, ErrNotFound
}

func (s *RedisStore) ListMatchingRules(ctx context.Context, filters MatchingRuleFilters) ([]AccessRule, error) {
	state, err := s.load(ctx, filters.OrganizationID, filters.ResourceType)
	if err != nil {
		return nil, err
	}

	matches := map[string]struct{}{}
	for i, kind := range filters.MatchKinds {
		if i >= len(filters.MatchValues) {
			break
		}
		value := CanonicalizeMatchValue(kind, filters.MatchValues[i])
		matches[ruleMatchKey(kind, value)] = struct{}{}
	}
	if len(matches) == 0 {
		return nil, nil
	}

	rows := make([]AccessRule, 0, len(state.Rules))
	for _, rule := range state.Rules {
		if _, ok := matches[ruleMatchKey(rule.MatchKind, rule.MatchValue)]; !ok {
			continue
		}
		if rule.AccessScope == AccessScopeProject && rule.ProjectID != filters.ProjectID {
			continue
		}
		if rule.AccessScope != AccessScopeProject && rule.AccessScope != AccessScopeOrganization {
			continue
		}
		rows = append(rows, rule)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	return rows, nil
}

func (s *RedisStore) load(ctx context.Context, organizationID, resourceType string) (redisState, error) {
	if s.cache == nil {
		return redisState{}, errors.New("cache is not configured")
	}

	var state redisState
	err := s.cache.Get(ctx, stateKey(organizationID, resourceType), &state)
	if err != nil {
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return redisState{Requests: nil, Rules: nil}, nil
		}
		return redisState{}, fmt.Errorf("get access control state: %w", err)
	}
	return state, nil
}

func (s *RedisStore) save(ctx context.Context, organizationID, resourceType string, state redisState) error {
	if s.cache == nil {
		return errors.New("cache is not configured")
	}
	if err := s.cache.Set(ctx, stateKey(organizationID, resourceType), state, s.ttl); err != nil {
		return fmt.Errorf("set access control state: %w", err)
	}
	return nil
}

func stateKey(organizationID, resourceType string) string {
	return fmt.Sprintf("access-control:%s:%s", organizationID, resourceType)
}

func normalizeLimit(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 1000
	}
	return limit
}

func pageByID[T any](rows []T, cursor string, limit int, id func(T) string) ([]T, string) {
	start := 0
	if cursor != "" {
		start = len(rows)
		for i, row := range rows {
			if id(row) == cursor {
				start = i + 1
				break
			}
		}
	}
	if start >= len(rows) {
		return nil, ""
	}

	end := start + limit
	if end >= len(rows) {
		return rows[start:], ""
	}
	return rows[start:end], id(rows[end-1])
}

func sameRuleMatch(a, b AccessRule) bool {
	return a.OrganizationID == b.OrganizationID &&
		a.ResourceType == b.ResourceType &&
		a.AccessScope == b.AccessScope &&
		a.ProjectID == b.ProjectID &&
		a.MatchKind == b.MatchKind &&
		a.MatchValue == b.MatchValue
}

func ruleMatchKey(kind, value string) string {
	return kind + "\x00" + value
}

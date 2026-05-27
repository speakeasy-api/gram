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
	// ErrRequestAlreadyDecided is returned when a requested decision targets a non-requested approval request.
	ErrRequestAlreadyDecided = errors.New("access control approval request already decided")
)

type conflictReasonError struct {
	reason error
}

func (e conflictReasonError) Error() string {
	return ErrConflict.Error() + ": " + e.reason.Error()
}

func (e conflictReasonError) Is(target error) bool {
	return target == ErrConflict || errors.Is(e.reason, target)
}

func requestAlreadyDecidedError() error {
	return conflictReasonError{reason: ErrRequestAlreadyDecided}
}

// RedisStore persists access control state in Redis through the shared cache layer.
type RedisStore struct {
	cache cache.Cache
	ttl   time.Duration
}

type redisState struct {
	Requests []AccessApprovalRequest
	Rules    []AccessRule
}

type mutatingCache interface {
	Mutate(ctx context.Context, key string, value any, ttl time.Duration, fn func(exists bool) error) error
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
	now := time.Now().UTC()
	if request.RequestedAt.IsZero() {
		request.RequestedAt = now
	}
	if request.BlockedCount <= 0 {
		request.BlockedCount = 1
	}
	if request.FirstBlockedAt.IsZero() {
		request.FirstBlockedAt = request.RequestedAt
	}
	if request.LastBlockedAt.IsZero() {
		request.LastBlockedAt = request.RequestedAt
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = request.RequestedAt
	}
	if request.UpdatedAt.IsZero() {
		request.UpdatedAt = request.LastBlockedAt
	}

	var result AccessApprovalRequest
	var created bool
	if err := s.mutate(ctx, request.OrganizationID, request.ResourceType, func(state *redisState) error {
		for i, existing := range state.Requests {
			if request.RequestFingerprint != "" &&
				existing.RequestFingerprint == request.RequestFingerprint &&
				existing.ProjectID == request.ProjectID &&
				existing.RequesterUserID == request.RequesterUserID &&
				existing.Status == RequestStatusRequested {
				if existing.BlockedCount <= 0 {
					existing.BlockedCount = 1
				}
				existing.BlockedCount += request.BlockedCount
				if existing.FirstBlockedAt.IsZero() {
					existing.FirstBlockedAt = request.FirstBlockedAt
				}
				existing.LastBlockedAt = request.LastBlockedAt
				if existing.CreatedAt.IsZero() {
					existing.CreatedAt = existing.RequestedAt
				}
				existing.UpdatedAt = request.UpdatedAt
				if request.RequesterEmail != "" {
					existing.RequesterEmail = request.RequesterEmail
				}
				if request.RequesterDisplayName != "" {
					existing.RequesterDisplayName = request.RequesterDisplayName
				}
				if request.DisplayName != "" {
					existing.DisplayName = request.DisplayName
				}
				existing.ObservedSummary = refreshObservedSummary(existing.ObservedSummary, request.ObservedSummary)
				state.Requests[i] = existing
				result = existing
				created = false
				return nil
			}
		}

		if request.ID == "" {
			request.ID = uuid.NewString()
		}
		if request.Status == "" {
			request.Status = RequestStatusRequested
		}

		state.Requests = append(state.Requests, request)
		result = request
		created = true
		return nil
	}); err != nil {
		return AccessApprovalRequest{}, false, err
	}
	return result, created, nil
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
	var result AccessApprovalRequest
	if err := s.mutate(ctx, organizationID, resourceType, func(state *redisState) error {
		for i := range state.Requests {
			if state.Requests[i].ID != id {
				continue
			}
			if state.Requests[i].Status != RequestStatusRequested {
				return requestAlreadyDecidedError()
			}

			now := time.Now().UTC()
			state.Requests[i].Status = status
			state.Requests[i].DecidedAt = &now
			state.Requests[i].DecidedBy = decidedBy
			state.Requests[i].DecisionNote = note
			state.Requests[i].SourceRuleIDs = slices.Clone(sourceRuleIDs)
			if state.Requests[i].CreatedAt.IsZero() {
				state.Requests[i].CreatedAt = state.Requests[i].RequestedAt
			}
			state.Requests[i].UpdatedAt = now
			result = state.Requests[i]
			return nil
		}
		return ErrNotFound
	}); err != nil {
		return AccessApprovalRequest{}, err
	}
	return result, nil
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
	rules, err := s.CreateRules(ctx, []AccessRule{rule})
	if err != nil {
		return AccessRule{}, err
	}
	return rules[0], nil
}

func (s *RedisStore) CreateRules(ctx context.Context, rules []AccessRule) ([]AccessRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	organizationID := rules[0].OrganizationID
	resourceType := rules[0].ResourceType
	var created []AccessRule
	if err := s.mutate(ctx, organizationID, resourceType, func(state *redisState) error {
		var err error
		created, err = prepareCreateRules(state.Rules, rules, organizationID, resourceType)
		if err != nil {
			return err
		}
		state.Rules = append(state.Rules, created...)
		return nil
	}); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *RedisStore) GetOrCreateRules(ctx context.Context, rules []AccessRule) ([]RuleUpsertResult, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	organizationID := rules[0].OrganizationID
	resourceType := rules[0].ResourceType
	var results []RuleUpsertResult
	if err := s.mutate(ctx, organizationID, resourceType, func(state *redisState) error {
		var err error
		results, *state, err = getOrCreateRulesInState(*state, rules, organizationID, resourceType)
		return err
	}); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *RedisStore) DecideRequestWithRules(ctx context.Context, organizationID, resourceType, id, status, decidedBy, note string, sourceRules []AccessRule) (AccessApprovalRequest, []RuleUpsertResult, error) {
	var request AccessApprovalRequest
	var results []RuleUpsertResult
	if err := s.mutate(ctx, organizationID, resourceType, func(state *redisState) error {
		requestIdx := -1
		for i := range state.Requests {
			if state.Requests[i].ID == id {
				requestIdx = i
				break
			}
		}
		if requestIdx == -1 {
			return ErrNotFound
		}
		if state.Requests[requestIdx].Status != RequestStatusRequested {
			return requestAlreadyDecidedError()
		}

		var err error
		results, *state, err = getOrCreateRulesInState(*state, sourceRules, organizationID, resourceType)
		if err != nil {
			return err
		}

		sourceRuleIDs := make([]string, 0, len(results))
		for _, result := range results {
			sourceRuleIDs = append(sourceRuleIDs, result.Rule.ID)
		}

		now := time.Now().UTC()
		state.Requests[requestIdx].Status = status
		state.Requests[requestIdx].DecidedAt = &now
		state.Requests[requestIdx].DecidedBy = decidedBy
		state.Requests[requestIdx].DecisionNote = note
		state.Requests[requestIdx].SourceRuleIDs = sourceRuleIDs
		if state.Requests[requestIdx].CreatedAt.IsZero() {
			state.Requests[requestIdx].CreatedAt = state.Requests[requestIdx].RequestedAt
		}
		state.Requests[requestIdx].UpdatedAt = now
		request = state.Requests[requestIdx]
		return nil
	}); err != nil {
		return AccessApprovalRequest{}, nil, err
	}
	return request, results, nil
}

func (s *RedisStore) UpdateRule(ctx context.Context, rule AccessRule) (AccessRule, error) {
	rule.MatchValue = CanonicalizeMatchValue(rule.MatchKind, rule.MatchValue)
	var result AccessRule
	if err := s.mutate(ctx, rule.OrganizationID, rule.ResourceType, func(state *redisState) error {
		for _, existing := range state.Rules {
			if existing.ID == rule.ID {
				continue
			}
			if sameRuleMatch(existing, rule) {
				return ErrConflict
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
			result = rule
			return nil
		}
		return ErrNotFound
	}); err != nil {
		return AccessRule{}, err
	}
	return result, nil
}

func (s *RedisStore) DeleteRule(ctx context.Context, organizationID, resourceType, id string) (AccessRule, error) {
	var result AccessRule
	if err := s.mutate(ctx, organizationID, resourceType, func(state *redisState) error {
		for i, rule := range state.Rules {
			if rule.ID != id {
				continue
			}
			state.Rules = slices.Delete(state.Rules, i, i+1)
			result = rule
			return nil
		}
		return ErrNotFound
	}); err != nil {
		return AccessRule{}, err
	}
	return result, nil
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

func (s *RedisStore) mutate(ctx context.Context, organizationID, resourceType string, fn func(*redisState) error) error {
	if s.cache == nil {
		return errors.New("cache is not configured")
	}

	key := stateKey(organizationID, resourceType)
	if cacheImpl, ok := s.cache.(mutatingCache); ok {
		var state redisState
		return cacheImpl.Mutate(ctx, key, &state, s.ttl, func(_ bool) error {
			return fn(&state)
		})
	}

	return errors.New("access control cache does not support atomic mutation")
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

type ruleMatchIdentity struct {
	OrganizationID string
	ResourceType   string
	AccessScope    string
	ProjectID      string
	MatchKind      string
	MatchValue     string
}

func prepareCreateRules(existingRules []AccessRule, rules []AccessRule, organizationID, resourceType string) ([]AccessRule, error) {
	now := time.Now().UTC()
	created := make([]AccessRule, 0, len(rules))
	seen := map[ruleMatchIdentity]struct{}{}
	for _, rule := range rules {
		rule = prepareRule(rule, now)
		if rule.OrganizationID != organizationID || rule.ResourceType != resourceType {
			return nil, ErrConflict
		}
		key := sameRuleMatchKey(rule)
		if _, ok := seen[key]; ok {
			return nil, ErrConflict
		}
		seen[key] = struct{}{}
		for _, existing := range existingRules {
			if sameRuleMatch(existing, rule) {
				return nil, ErrConflict
			}
		}
		created = append(created, rule)
	}
	return created, nil
}

func getOrCreateRulesInState(state redisState, rules []AccessRule, organizationID, resourceType string) ([]RuleUpsertResult, redisState, error) {
	now := time.Now().UTC()
	results := make([]RuleUpsertResult, 0, len(rules))
	seen := map[ruleMatchIdentity]struct{}{}
	for _, rule := range rules {
		rule = prepareRule(rule, now)
		if rule.OrganizationID != organizationID || rule.ResourceType != resourceType {
			return nil, redisState{}, ErrConflict
		}
		key := sameRuleMatchKey(rule)
		if _, ok := seen[key]; ok {
			return nil, redisState{}, ErrConflict
		}
		seen[key] = struct{}{}

		existingRule, found := findRuleByMatch(state.Rules, rule)
		if found {
			if existingRule.Disposition != rule.Disposition {
				return nil, redisState{}, ErrConflict
			}
			results = append(results, RuleUpsertResult{Rule: existingRule, Created: false})
			continue
		}

		state.Rules = append(state.Rules, rule)
		results = append(results, RuleUpsertResult{Rule: rule, Created: true})
	}
	return results, state, nil
}

func prepareRule(rule AccessRule, now time.Time) AccessRule {
	rule.MatchValue = CanonicalizeMatchValue(rule.MatchKind, rule.MatchValue)
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	if rule.UpdatedAt.IsZero() {
		rule.UpdatedAt = rule.CreatedAt
	}
	return rule
}

func findRuleByMatch(rules []AccessRule, rule AccessRule) (AccessRule, bool) {
	for _, existing := range rules {
		if sameRuleMatch(existing, rule) {
			return existing, true
		}
	}
	return AccessRule{}, false
}

func sameRuleMatchKey(rule AccessRule) ruleMatchIdentity {
	return ruleMatchIdentity{
		OrganizationID: rule.OrganizationID,
		ResourceType:   rule.ResourceType,
		AccessScope:    rule.AccessScope,
		ProjectID:      rule.ProjectID,
		MatchKind:      rule.MatchKind,
		MatchValue:     rule.MatchValue,
	}
}

func refreshObservedSummary(existing, next ObservedSummary) ObservedSummary {
	if observedSummaryCoreNonEmpty(next) {
		existing.Name = next.Name
		existing.FullURL = next.FullURL
		existing.URLHost = next.URLHost
		existing.ServerIdentity = next.ServerIdentity
		existing.ToolName = next.ToolName
		existing.ToolCall = next.ToolCall
		existing.BlockReason = next.BlockReason
	}
	if nonEmptyStringPtr(next.RiskPolicyID) {
		existing.RiskPolicyID = next.RiskPolicyID
	}
	if nonEmptyStringPtr(next.RiskResultID) {
		existing.RiskResultID = next.RiskResultID
	}
	return existing
}

func observedSummaryCoreNonEmpty(summary ObservedSummary) bool {
	return nonEmptyStringPtr(summary.Name) ||
		nonEmptyStringPtr(summary.FullURL) ||
		nonEmptyStringPtr(summary.URLHost) ||
		nonEmptyStringPtr(summary.ServerIdentity) ||
		nonEmptyStringPtr(summary.ToolName) ||
		nonEmptyStringPtr(summary.ToolCall) ||
		nonEmptyStringPtr(summary.BlockReason)
}

func nonEmptyStringPtr(value *string) bool {
	return value != nil && *value != ""
}

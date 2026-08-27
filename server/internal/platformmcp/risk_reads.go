package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

const riskReadPageSize = 50

var (
	ErrRiskReadInvalid  = errors.New("invalid platform mcp risk read")
	ErrRiskReadNotFound = errors.New("platform mcp risk resource not found")
)

type riskPolicyReader interface {
	ListPage(ctx context.Context, organizationID string, projectID uuid.UUID, cursor *policycore.PageCursor, limit int32) ([]policycore.Policy, error)
}

type riskExclusionReader interface {
	ListPage(ctx context.Context, projectID uuid.UUID, policyID uuid.NullUUID, cursor *exclusioncore.PageCursor, limit int32) ([]exclusioncore.Exclusion, error)
}

type riskProjectResolver interface {
	Resolve(ctx context.Context, organizationID, projectID, projectSlug string) (ResolvedProject, error)
}

type postgresRiskProjectResolver struct {
	queries *platformrepo.Queries
}

func (r postgresRiskProjectResolver) Resolve(ctx context.Context, organizationID, projectID, projectSlug string) (ResolvedProject, error) {
	if r.queries == nil || organizationID == "" || (projectID != "" && projectSlug != "") {
		return ResolvedProject{}, ErrRiskReadInvalid
	}
	if projectID != "" {
		id, err := uuid.Parse(projectID)
		if err != nil {
			return ResolvedProject{}, ErrRiskReadInvalid
		}
		row, err := r.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: organizationID, ProjectID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedProject{}, ErrRiskReadNotFound
		}
		if err != nil {
			return ResolvedProject{}, fmt.Errorf("resolve platform risk project by id: %w", err)
		}
		return ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}, nil
	}
	if projectSlug == "" {
		projectSlug = "default"
	}
	row, err := r.queries.ResolvePlatformMCPProjectBySlug(ctx, platformrepo.ResolvePlatformMCPProjectBySlugParams{OrganizationID: organizationID, Slug: projectSlug})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, ErrRiskReadNotFound
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve platform risk project by slug: %w", err)
	}
	return ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}, nil
}

type RiskReadService struct {
	projects           riskProjectResolver
	policies           riskPolicyReader
	exclusions         riskExclusionReader
	cursor             *riskCursorCodec
	catalog            policycatalog.Catalog
	catalogFingerprint string
	redactionKey       []byte
	versions           *riskVersionCodec
	loadPolicyDetail   func(context.Context, uuid.UUID, uuid.UUID) (policycore.Policy, string, error)
}

func newRiskReadService(db *pgxpool.Pool, keyMaterial string) (*RiskReadService, error) {
	if db == nil {
		return nil, ErrUnavailable
	}
	cursor, err := newRiskCursorCodec(keyMaterial)
	if err != nil {
		return nil, err
	}
	catalog, err := policycatalog.Build()
	if err != nil {
		return nil, fmt.Errorf("build risk policy catalog: %w", err)
	}
	fingerprint, err := policycatalog.Fingerprint(catalog)
	if err != nil {
		return nil, fmt.Errorf("fingerprint risk policy catalog: %w", err)
	}
	versions, err := newRiskVersionCodec(keyMaterial)
	if err != nil {
		return nil, err
	}
	redactionKey := sha256.Sum256([]byte("platform-mcp-risk-value:" + keyMaterial))
	return &RiskReadService{
		projects:           postgresRiskProjectResolver{queries: platformrepo.New(db)},
		policies:           policycore.New(db),
		exclusions:         exclusioncore.New(db),
		cursor:             cursor,
		catalog:            catalog,
		catalogFingerprint: fingerprint,
		redactionKey:       redactionKey[:],
		versions:           versions,
		loadPolicyDetail: func(ctx context.Context, projectID, policyID uuid.UUID) (policycore.Policy, string, error) {
			tx, err := db.BeginTx(ctx, pgx.TxOptions{
				IsoLevel:       pgx.RepeatableRead,
				AccessMode:     pgx.ReadOnly,
				DeferrableMode: "",
				BeginQuery:     "",
				CommitQuery:    "",
			})
			if err != nil {
				return policycore.Policy{}, "", fmt.Errorf("begin risk policy detail snapshot: %w", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			policy, err := policycore.New(tx).Get(ctx, projectID, policyID)
			if err != nil {
				return policycore.Policy{}, "", fmt.Errorf("load risk policy detail snapshot: %w", err)
			}
			state, err := riskPolicyVersionState(ctx, tx, policy, false)
			if err != nil {
				return policycore.Policy{}, "", err
			}
			version, err := versions.PolicyVersion(state)
			if err != nil {
				return policycore.Policy{}, "", err
			}
			if err := tx.Commit(ctx); err != nil {
				return policycore.Policy{}, "", fmt.Errorf("commit risk policy detail snapshot: %w", err)
			}
			return policy, version, nil
		},
	}, nil
}

type RiskProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type RiskCompatibility struct {
	State             string   `json:"state"`
	UnsupportedFields []string `json:"unsupported_fields"`
}

type RiskPolicySummary struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	PolicyType    string            `json:"policy_type"`
	Enabled       bool              `json:"enabled"`
	Action        string            `json:"action,omitempty"`
	Sources       []string          `json:"sources"`
	Score         float64           `json:"score"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
	Compatibility RiskCompatibility `json:"compatibility"`
}

type RiskDetectionScope struct {
	Category     string   `json:"category"`
	MessageTypes []string `json:"message_types"`
}

type RiskPolicyDetail struct {
	RiskPolicySummary
	Version                string               `json:"version"`
	PresidioEntities       []string             `json:"presidio_entities"`
	PresidioScoreThreshold *float64             `json:"presidio_score_threshold,omitempty"`
	ApprovedEmailDomains   []string             `json:"approved_email_domains"`
	DisabledRules          []string             `json:"disabled_rules"`
	MessageTypes           []string             `json:"message_types"`
	DetectionScopes        []RiskDetectionScope `json:"detection_scopes"`
	UserMessage            *string              `json:"user_message,omitempty"`
	Prompt                 *string              `json:"prompt,omitempty"`
	PendingMessages        *int64               `json:"pending_messages,omitempty"`
	TotalMessages          *int64               `json:"total_messages,omitempty"`
}

type ListRiskPoliciesInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type ListRiskPoliciesOutput struct {
	Project            RiskProject         `json:"project"`
	CatalogVersion     string              `json:"catalog_version"`
	CatalogFingerprint string              `json:"catalog_fingerprint"`
	Policies           []RiskPolicySummary `json:"policies"`
	NextCursor         string              `json:"next_cursor,omitempty"`
}

type GetRiskPolicyInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	PolicyID    string `json:"policy_id"`
}

type GetRiskPolicyOutput struct {
	Project            RiskProject      `json:"project"`
	CatalogVersion     string           `json:"catalog_version"`
	CatalogFingerprint string           `json:"catalog_fingerprint"`
	Policy             RiskPolicyDetail `json:"policy"`
}

type RiskExclusionSummary struct {
	ID               string            `json:"id"`
	PolicyID         *string           `json:"policy_id,omitempty"`
	MatchType        string            `json:"match_type"`
	MatchValue       string            `json:"match_value,omitempty"`
	MatchFingerprint string            `json:"match_fingerprint,omitempty"`
	MatchLength      int               `json:"match_length,omitempty"`
	RuleIDFilter     string            `json:"rule_id_filter,omitempty"`
	SourceFilter     string            `json:"source_filter,omitempty"`
	Enabled          bool              `json:"enabled"`
	Version          string            `json:"version"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	Compatibility    RiskCompatibility `json:"compatibility"`
}

type ListRiskExclusionsInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	PolicyID    string `json:"policy_id,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type ListRiskExclusionsOutput struct {
	Project            RiskProject            `json:"project"`
	CatalogVersion     string                 `json:"catalog_version"`
	CatalogFingerprint string                 `json:"catalog_fingerprint"`
	Exclusions         []RiskExclusionSummary `json:"exclusions"`
	NextCursor         string                 `json:"next_cursor,omitempty"`
}

func (s *RiskReadService) ListPolicies(ctx context.Context, principal Principal, input ListRiskPoliciesInput) (ListRiskPoliciesOutput, error) {
	if !s.valid() {
		return ListRiskPoliciesOutput{}, ErrUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return ListRiskPoliciesOutput{}, fmt.Errorf("resolve risk policy list project: %w", err)
	}
	limit, err := riskPageLimit(input.Limit)
	if err != nil {
		return ListRiskPoliciesOutput{}, err
	}
	var pageCursor *policycore.PageCursor
	if input.Cursor != "" {
		decoded, err := s.cursor.Decode(input.Cursor, principal, "policies", project.ID, uuid.Nil)
		if err != nil {
			return ListRiskPoliciesOutput{}, err
		}
		pageCursor = &policycore.PageCursor{CreatedAt: decoded.CreatedAt, ID: decoded.ID}
	}
	policies, err := s.policies.ListPage(ctx, principal.OrganizationID, project.ID, pageCursor, int32(limit)) // #nosec G115 -- riskPageLimit caps at 50.
	if err != nil {
		return ListRiskPoliciesOutput{}, fmt.Errorf("list risk policy page: %w", err)
	}
	output := ListRiskPoliciesOutput{Project: riskProject(project), CatalogVersion: s.catalog.Schema, CatalogFingerprint: s.catalogFingerprint, Policies: make([]RiskPolicySummary, 0, min(len(policies), limit)), NextCursor: ""}
	for _, policy := range policies[:min(len(policies), limit)] {
		output.Policies = append(output.Policies, s.policySummary(policy))
	}
	if len(policies) > limit {
		last := policies[limit-1]
		output.NextCursor, err = s.cursor.Encode(riskCursor{Kind: "policies", OrganizationID: principal.OrganizationID, Binding: principalCursorBinding(principal), ProjectID: project.ID, PolicyID: uuid.Nil, CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return ListRiskPoliciesOutput{}, err
		}
	}
	return output, nil
}

func (s *RiskReadService) GetPolicy(ctx context.Context, principal Principal, input GetRiskPolicyInput) (GetRiskPolicyOutput, error) {
	if !s.valid() {
		return GetRiskPolicyOutput{}, ErrUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return GetRiskPolicyOutput{}, fmt.Errorf("resolve risk policy project: %w", err)
	}
	policyID, err := uuid.Parse(input.PolicyID)
	if err != nil {
		return GetRiskPolicyOutput{}, ErrRiskReadInvalid
	}
	policy, version, err := s.loadPolicyDetail(ctx, project.ID, policyID)
	if errors.Is(err, policycore.ErrLoadPolicy) {
		return GetRiskPolicyOutput{}, ErrRiskReadNotFound
	}
	if err != nil {
		return GetRiskPolicyOutput{}, fmt.Errorf("get risk policy detail: %w", err)
	}
	detail := s.policyDetail(policy)
	detail.Version = version
	return GetRiskPolicyOutput{Project: riskProject(project), CatalogVersion: s.catalog.Schema, CatalogFingerprint: s.catalogFingerprint, Policy: detail}, nil
}

func (s *RiskReadService) ListExclusions(ctx context.Context, principal Principal, input ListRiskExclusionsInput) (ListRiskExclusionsOutput, error) {
	if !s.valid() {
		return ListRiskExclusionsOutput{}, ErrUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return ListRiskExclusionsOutput{}, fmt.Errorf("resolve risk exclusion list project: %w", err)
	}
	limit, err := riskPageLimit(input.Limit)
	if err != nil {
		return ListRiskExclusionsOutput{}, err
	}
	policyID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if input.PolicyID != "" {
		parsed, err := uuid.Parse(input.PolicyID)
		if err != nil {
			return ListRiskExclusionsOutput{}, ErrRiskReadInvalid
		}
		policyID = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	var pageCursor *exclusioncore.PageCursor
	if input.Cursor != "" {
		decoded, err := s.cursor.Decode(input.Cursor, principal, "exclusions", project.ID, policyID.UUID)
		if err != nil {
			return ListRiskExclusionsOutput{}, err
		}
		pageCursor = &exclusioncore.PageCursor{CreatedAt: decoded.CreatedAt, ID: decoded.ID}
	}
	exclusions, err := s.exclusions.ListPage(ctx, project.ID, policyID, pageCursor, int32(limit)) // #nosec G115 -- riskPageLimit caps at 50.
	if err != nil {
		return ListRiskExclusionsOutput{}, fmt.Errorf("list risk exclusion page: %w", err)
	}
	output := ListRiskExclusionsOutput{Project: riskProject(project), CatalogVersion: s.catalog.Schema, CatalogFingerprint: s.catalogFingerprint, Exclusions: make([]RiskExclusionSummary, 0, min(len(exclusions), limit)), NextCursor: ""}
	for _, exclusion := range exclusions[:min(len(exclusions), limit)] {
		summary, err := s.exclusionSummary(exclusion)
		if err != nil {
			return ListRiskExclusionsOutput{}, fmt.Errorf("issue risk exclusion version: %w", err)
		}
		output.Exclusions = append(output.Exclusions, summary)
	}
	if len(exclusions) > limit {
		last := exclusions[limit-1]
		output.NextCursor, err = s.cursor.Encode(riskCursor{Kind: "exclusions", OrganizationID: principal.OrganizationID, Binding: principalCursorBinding(principal), ProjectID: project.ID, PolicyID: policyID.UUID, CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return ListRiskExclusionsOutput{}, err
		}
	}
	return output, nil
}

func (s *RiskReadService) valid() bool {
	return s != nil && s.projects != nil && s.policies != nil && s.exclusions != nil && s.cursor != nil && len(s.redactionKey) > 0 && s.versions != nil && s.catalog.Schema != "" && s.catalogFingerprint != "" && s.loadPolicyDetail != nil
}

func riskPageLimit(value int) (int, error) {
	if value < 0 || value > riskReadPageSize {
		return 0, ErrRiskReadInvalid
	}
	if value == 0 {
		return riskReadPageSize, nil
	}
	return value, nil
}

func riskProject(project ResolvedProject) RiskProject {
	return RiskProject{ID: project.ID.String(), Name: project.Name, Slug: project.Slug}
}

func (s *RiskReadService) policySummary(policy policycore.Policy) RiskPolicySummary {
	unsupported := s.policyUnsupported(policy)
	action := policy.Action
	if !s.policyActionSupported(policy) {
		action = ""
	}
	return RiskPolicySummary{
		ID: policy.ID.String(), Name: policy.Name, PolicyType: policy.PolicyType,
		Enabled: policy.Enabled, Action: action, Sources: allowlisted(policy.Sources, s.catalog.Sources),
		Score: policy.Score, CreatedAt: policy.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: policy.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Compatibility: compatibility(unsupported),
	}
}

func (s *RiskReadService) policyDetail(policy policycore.Policy) RiskPolicyDetail {
	detectionScopes, _ := s.projectDetectionScopes(policy)
	detail := RiskPolicyDetail{
		RiskPolicySummary:      s.policySummary(policy),
		Version:                "",
		PresidioEntities:       allowlisted(policy.PresidioEntities, s.catalog.PresidioEntities),
		PresidioScoreThreshold: policy.PresidioScoreThreshold,
		ApprovedEmailDomains:   append([]string{}, policy.ApprovedEmailDomains...),
		DisabledRules:          allowlisted(policy.DisabledRules, s.catalog.DisabledRules),
		MessageTypes:           allowlisted(policy.MessageTypes, s.catalog.PolicyMessageTypes),
		DetectionScopes:        detectionScopes,
		UserMessage:            policy.UserMessage,
		Prompt:                 nil,
		PendingMessages:        policy.PendingMessages,
		TotalMessages:          policy.TotalMessages,
	}
	if policy.PolicyType == "prompt_based" && policy.Prompt != nil && utf8.RuneCountInString(*policy.Prompt) <= 4000 {
		detail.Prompt = policy.Prompt
	}
	return detail
}

func (s *RiskReadService) policyUnsupported(policy policycore.Policy) []string {
	unsupported := make([]string, 0, 8)
	if !s.policyActionSupported(policy) {
		unsupported = append(unsupported, "unsupported_action")
	}
	if policy.AudienceType != "everyone" {
		unsupported = append(unsupported, "targeted_audience")
	}
	if len(policy.CustomRuleIDs) > 0 {
		unsupported = append(unsupported, "custom_rules")
	}
	if policy.ModelConfig != nil {
		unsupported = append(unsupported, "model_config")
	}
	if policy.ShadowMCPDisposition != nil {
		unsupported = append(unsupported, "shadow_mcp_url_decisions")
	}
	_, scopesSupported := s.projectDetectionScopes(policy)
	if policy.ScopeInclude != nil || policy.ScopeExempt != nil || !scopesSupported {
		unsupported = append(unsupported, "raw_scope")
	}
	if policy.Prompt != nil && utf8.RuneCountInString(*policy.Prompt) > 4000 {
		unsupported = append(unsupported, "prompt_too_long")
	}
	if hasUnknown(policy.Sources, s.catalog.Sources) || hasUnknown(policy.PresidioEntities, s.catalog.PresidioEntities) || hasUnknown(policy.DisabledRules, s.catalog.DisabledRules) || hasUnknown(policy.MessageTypes, s.catalog.PolicyMessageTypes) || len(policy.PromptInjectionRules) > 0 {
		unsupported = append(unsupported, "unknown_detector_value")
	}
	slices.Sort(unsupported)
	return slices.Compact(unsupported)
}

func (s *RiskReadService) policyActionSupported(policy policycore.Policy) bool {
	if !slices.Contains(s.catalog.Actions, policy.Action) {
		return false
	}
	if policy.Action == "flag" {
		return true
	}
	return !slices.ContainsFunc(policy.Sources, func(source string) bool {
		return slices.Contains(s.catalog.FlagOnlySources, source)
	})
}

func (s *RiskReadService) projectDetectionScopes(policy policycore.Policy) ([]RiskDetectionScope, bool) {
	if len(policy.DetectionScopes) == 0 {
		return []RiskDetectionScope{}, true
	}
	result := make([]RiskDetectionScope, 0, len(policy.DetectionScopes))
	seen := make(map[string]bool, len(policy.DetectionScopes))
	for _, scope := range policy.DetectionScopes {
		if seen[scope.Category] || !slices.Contains(s.catalog.DetectionScopeCategories, scope.Category) {
			return []RiskDetectionScope{}, false
		}
		messageTypes, ok := policycatalog.DecodeDetectionScope(valueOrEmpty(scope.ScopeInclude), valueOrEmpty(scope.ScopeExempt), s.catalog)
		if !ok {
			return []RiskDetectionScope{}, false
		}
		seen[scope.Category] = true
		result = append(result, RiskDetectionScope{Category: scope.Category, MessageTypes: messageTypes})
	}
	slices.SortFunc(result, func(a, b RiskDetectionScope) int { return strings.Compare(a.Category, b.Category) })
	return result, true
}

func (s *RiskReadService) exclusionSummary(exclusion exclusioncore.Exclusion) (RiskExclusionSummary, error) {
	unsupported := make([]string, 0, 3)
	result := RiskExclusionSummary{
		ID: exclusion.ID.String(), PolicyID: nil, MatchType: exclusion.MatchType,
		MatchValue: "", MatchFingerprint: "", MatchLength: 0,
		RuleIDFilter: "", SourceFilter: "", Enabled: exclusion.Enabled, Version: "",
		CreatedAt: exclusion.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: exclusion.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Compatibility: RiskCompatibility{State: "fully_supported", UnsupportedFields: []string{}},
	}
	if exclusion.RiskPolicyID.Valid {
		value := exclusion.RiskPolicyID.UUID.String()
		result.PolicyID = &value
	}
	switch exclusion.MatchType {
	case "exact", "regex":
		result.MatchFingerprint = s.fingerprintValue(exclusion.MatchValue)
		result.MatchLength = utf8.RuneCountInString(exclusion.MatchValue)
		if exclusion.MatchType == "regex" {
			unsupported = append(unsupported, "legacy_regex")
		}
	case "rule_id":
		if slices.Contains(s.catalog.DisabledRules, exclusion.MatchValue) {
			result.MatchValue = exclusion.MatchValue
		} else {
			unsupported = append(unsupported, "unknown_detector_value")
		}
	case "source":
		if slices.Contains(s.catalog.Sources, exclusion.MatchValue) {
			result.MatchValue = exclusion.MatchValue
		} else {
			unsupported = append(unsupported, "unknown_detector_value")
		}
	case "entity_type":
		if slices.Contains(s.catalog.PresidioEntities, exclusion.MatchValue) {
			result.MatchValue = exclusion.MatchValue
		} else {
			unsupported = append(unsupported, "unknown_detector_value")
		}
	default:
		unsupported = append(unsupported, "unknown_detector_value")
	}
	if exclusion.RuleIDFilter != "" {
		if slices.Contains(s.catalog.DisabledRules, exclusion.RuleIDFilter) {
			result.RuleIDFilter = exclusion.RuleIDFilter
		} else {
			unsupported = append(unsupported, "unknown_detector_value")
		}
	}
	if exclusion.SourceFilter != "" {
		switch {
		case !slices.Contains(s.catalog.Sources, exclusion.SourceFilter):
			unsupported = append(unsupported, "unknown_detector_value")
		case exclusion.MatchType == "entity_type" && exclusion.SourceFilter != policycatalog.PresidioSource:
			unsupported = append(unsupported, "unsupported_source_filter")
		default:
			result.SourceFilter = exclusion.SourceFilter
		}
	}
	slices.Sort(unsupported)
	result.Compatibility = compatibility(slices.Compact(unsupported))
	version, err := s.versions.ExclusionVersion(exclusion)
	if err != nil {
		return RiskExclusionSummary{}, err
	}
	result.Version = version
	return result, nil
}

func (s *RiskReadService) fingerprintValue(value string) string {
	mac := hmac.New(sha256.New, s.redactionKey)
	_, _ = mac.Write([]byte(value))
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)[:16])
}

func compatibility(unsupported []string) RiskCompatibility {
	state := "fully_supported"
	if len(unsupported) > 0 {
		state = "partially_supported"
	}
	if unsupported == nil {
		unsupported = []string{}
	}
	return RiskCompatibility{State: state, UnsupportedFields: unsupported}
}

func allowlisted(values, allowed []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if slices.Contains(allowed, value) {
			result = append(result, value)
		}
	}
	if result == nil {
		return []string{}
	}
	return result
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func hasUnknown(values, allowed []string) bool {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return true
		}
	}
	return false
}

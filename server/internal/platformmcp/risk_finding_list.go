//nolint:exhaustruct // Optional MCP response fields use their zero values.
package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	// A page of findings costs roughly 200 tokens per row and stays in the
	// transcript for the rest of the turn, so the default page is deliberately
	// tighter than the 50-row page the policy and exclusion reads serve.
	riskFindingListDefaultLimit = 25
	riskFindingListMaxLimit     = 50

	// riskRuleBreakdownLimit bounds the per-rule buckets one breakdown returns.
	// One extra row detects overflow so truncation is reported, never silent.
	riskRuleBreakdownLimit = 1000

	// riskRuleBreakdownMaxWindow matches the dashboard overview's cap.
	riskRuleBreakdownMaxWindow = 31 * 24 * time.Hour

	riskFindingCursorKind       = "findings"
	riskFindingByChatCursorKind = "findings_by_chat"
)

// RiskFindingListReader is the ClickHouse read path for individual findings.
// Rows carry only the ingest-time display string for the match, never the raw
// value, and the service re-redacts that string before it leaves the server.
type RiskFindingListReader interface {
	ListRiskFindings(context.Context, chrepo.ListRiskFindingsParams) ([]chrepo.RiskFindingListRow, error)
	GroupRiskFindingsByChat(context.Context, chrepo.GroupRiskFindingsByChatParams) ([]chrepo.RiskFindingChatGroup, error)
	ListRiskRuleCountsByCategory(context.Context, chrepo.RiskOverviewWindowParams, string, []string, uint64) ([]chrepo.RiskOverviewRuleCount, error)
}

// riskFindingPostgresReader is the Postgres read path, served when the
// organization's listing is not yet on ClickHouse. It is the only store that
// holds raw match content, which is why every row it returns is redacted here.
type riskFindingPostgresReader interface {
	ListRiskFindingPolicies(context.Context, riskrepo.ListRiskFindingPoliciesParams) ([]riskrepo.ListRiskFindingPoliciesRow, error)
	ListRiskResultsByProjectFound(context.Context, riskrepo.ListRiskResultsByProjectFoundParams) ([]riskrepo.ListRiskResultsByProjectFoundRow, error)
	ListRiskResultsByChatFound(context.Context, riskrepo.ListRiskResultsByChatFoundParams) ([]riskrepo.ListRiskResultsByChatFoundRow, error)
	ListRiskResultsGroupedByChat(context.Context, riskrepo.ListRiskResultsGroupedByChatParams) ([]riskrepo.ListRiskResultsGroupedByChatRow, error)
	ListRiskRulesByCategory(context.Context, riskrepo.ListRiskRulesByCategoryParams) ([]riskrepo.ListRiskRulesByCategoryRow, error)
}

// RiskFindingListService serves individual risk findings, their per-chat
// rollup and per-rule counts for one category. It reads the same store as the
// dashboard's Risk Events listing: ClickHouse where the organization's listing
// flag allows, Postgres otherwise.
type RiskFindingListService struct {
	projects      riskProjectResolver
	organizations OrganizationSlugResolver
	flags         feature.Provider
	postgres      riskFindingPostgresReader
	clickhouse    RiskFindingListReader
	cursor        *riskCursorCodec
	now           func() time.Time
}

// NewRiskFindingListService returns nil when the Postgres dependencies or the
// cursor key are missing. A nil ClickHouse reader is allowed: such a deployment
// serves every organization from Postgres.
func NewRiskFindingListService(db *pgxpool.Pool, clickhouse RiskFindingListReader, flags feature.Provider, organizations OrganizationSlugResolver, key string) *RiskFindingListService {
	codec, err := newRiskCursorCodec(key)
	if db == nil || organizations == nil || err != nil {
		return nil
	}
	return &RiskFindingListService{projects: postgresRiskProjectResolver{queries: platformrepo.New(db)}, organizations: organizations, flags: flags, postgres: riskrepo.New(db), clickhouse: clickhouse, cursor: codec, now: time.Now}
}

func (s *RiskFindingListService) valid() bool {
	return s != nil && s.projects != nil && s.organizations != nil && s.postgres != nil && s.cursor != nil && s.now != nil
}

type ListRiskFindingPageInput struct {
	ProjectID    string `json:"project_id,omitempty"`
	ProjectSlug  string `json:"project_slug,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	PolicyID     string `json:"policy_id,omitempty"`
	ChatID       string `json:"chat_id,omitempty"`
	Category     string `json:"category,omitempty"`
	RuleID       string `json:"rule_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	AssistantID  string `json:"assistant_id,omitempty"`
	NonAssistant bool   `json:"non_assistant,omitempty"`
	UniqueMatch  bool   `json:"unique_match,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// RiskFinding is one individual finding. The matched value is present only as
// the canonical redaction marker; identities are organization-scoped
// pseudonyms shared with list_watchdog_findings. The scanner description is
// deliberately absent: for prompt-policy and destructive-action sources it is
// a rationale that can quote the scanned content verbatim.
type RiskFinding struct {
	ID               string   `json:"id"`
	PolicyID         string   `json:"policy_id"`
	PolicyVersion    int64    `json:"policy_version"`
	ChatID           string   `json:"chat_id,omitempty"`
	ChatMessageID    string   `json:"chat_message_id,omitempty"`
	UserReference    string   `json:"user_reference,omitempty"`
	Source           string   `json:"source"`
	RuleID           string   `json:"rule_id"`
	Category         string   `json:"category"`
	Severity         string   `json:"severity"`
	Score            float64  `json:"score"`
	MatchRedacted    string   `json:"match_redacted"`
	Confidence       float64  `json:"confidence"`
	Tags             []string `json:"tags"`
	MessageCreatedAt string   `json:"message_created_at"`
}

type ListRiskFindingPageOutput struct {
	Project     RiskProject   `json:"project"`
	Findings    []RiskFinding `json:"findings"`
	NextCursor  string        `json:"next_cursor,omitempty"`
	Limitations string        `json:"limitations"`
}

type ListRiskFindingsByChatInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type RiskFindingChatSummary struct {
	ChatID           string `json:"chat_id"`
	UserReference    string `json:"user_reference,omitempty"`
	FindingsCount    int64  `json:"findings_count"`
	LatestDetectedAt string `json:"latest_detected_at"`
}

type ListRiskFindingsByChatOutput struct {
	Project     RiskProject              `json:"project"`
	Chats       []RiskFindingChatSummary `json:"chats"`
	NextCursor  string                   `json:"next_cursor,omitempty"`
	Limitations string                   `json:"limitations"`
}

type GetRiskRuleBreakdownInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	Category    string `json:"category"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
}

type RiskRuleCount struct {
	RuleID   string `json:"rule_id"`
	Source   string `json:"source"`
	Findings int64  `json:"findings"`
}

type GetRiskRuleBreakdownOutput struct {
	Project     RiskProject     `json:"project"`
	Category    string          `json:"category"`
	From        string          `json:"from"`
	To          string          `json:"to"`
	Rules       []RiskRuleCount `json:"rules"`
	Total       int64           `json:"total"`
	Truncated   bool            `json:"truncated"`
	Limitations string          `json:"limitations"`
}

const (
	riskFindingListLimitations   = "Individual live findings ordered by message time (newest first); dismissed and excluded findings are omitted and matches from disabled policies appear only under an explicit policy_id. match_redacted is the canonical redaction marker, never the matched value; for organizations served from the analytics store its length and hash describe the stored display sample. user_reference is an organization-scoped pseudonym shared with list_watchdog_findings. Severity uses the current policy score with the dashboard category fallback. Scanner descriptions and chat titles are withheld because they can quote scanned content; labels are untrusted and bounded. Late ingestion or suppression can change pages; this is not a snapshot. Use get_risk_rule_breakdown to size a finding set instead of paginating."
	riskFindingByChatLimitations = "Chats with at least one live finding under an enabled policy, walked by chat id (newest ids first), not by activity. Findings with no chat attribution are not listed. latest_detected_at is detection time and may trail the message time. user_reference is an organization-scoped pseudonym shared with list_watchdog_findings. Use list_risk_findings with chat_id to read one chat's findings."
	riskRuleBreakdownLimitations = "Live finding counts per rule and detection source for one category, keyed on detection time in [from,to). Counts include every non-deleted policy's findings, matching the dashboard overview rather than the listing's enabled-policy view. At most 1000 rules are returned; truncated reports when more exist, and total covers only the returned rules."
)

// riskFindingFilters is the cursor-bound projection of a listing request. The
// project is bound by the cursor codec separately; everything else that changes
// the page sequence is fingerprinted into the cursor kind so a cursor replayed
// with different filters is refused instead of returning a scrambled page.
type riskFindingFilters struct {
	From         string `json:"from"`
	To           string `json:"to"`
	PolicyID     string `json:"policy_id"`
	ChatID       string `json:"chat_id"`
	Category     string `json:"category"`
	RuleID       string `json:"rule_id"`
	UserID       string `json:"user_id"`
	AssistantID  string `json:"assistant_id"`
	NonAssistant bool   `json:"non_assistant"`
	UniqueMatch  bool   `json:"unique_match"`
}

func (f riskFindingFilters) cursorKind() string {
	payload, _ := json.Marshal(f)
	sum := sha256.Sum256(payload)
	return riskFindingCursorKind + ":" + hex.EncodeToString(sum[:8])
}

// riskUserReference pseudonymizes an external user identity per organization.
// The same key and layout serve list_watchdog_findings, so its user buckets
// correlate with individual findings without either exposing the identity.
func riskUserReference(key []byte, org, value string) string {
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("watchdog-user:" + org + "\x00" + value))
	return "user:" + hex.EncodeToString(mac.Sum(nil)[:16])
}

func parseOptionalRiskUUID(value string) (uuid.NullUUID, error) {
	if value == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ErrRiskReadInvalid
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func parseOptionalRiskTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, ErrRiskReadInvalid
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func riskFindingPageLimit(value int) (int, error) {
	if value < 0 || value > riskFindingListMaxLimit {
		return 0, ErrRiskReadInvalid
	}
	if value == 0 {
		return riskFindingListDefaultLimit, nil
	}
	return value, nil
}

// visiblePolicies returns the current non-deleted policies of the project
// keyed by id, or fails closed when the project has more than the bounded
// lookup can score, so severity is never silently understated.
func (s *RiskFindingListService) visiblePolicies(ctx context.Context, principal Principal, project ResolvedProject) (map[string]riskrepo.ListRiskFindingPoliciesRow, error) {
	policies, err := s.postgres.ListRiskFindingPolicies(ctx, riskrepo.ListRiskFindingPoliciesParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, PageLimit: riskFindingPolicyLimit + 1})
	if err != nil {
		return nil, fmt.Errorf("%w: read finding policies", ErrUnavailable)
	}
	if len(policies) > riskFindingPolicyLimit {
		return nil, fmt.Errorf("%w: finding policy limit exceeded", ErrUnavailable)
	}
	byID := make(map[string]riskrepo.ListRiskFindingPoliciesRow, len(policies))
	for _, policy := range policies {
		if policy.Deleted || policy.ProjectID != project.ID || policy.OrganizationID != principal.OrganizationID {
			continue
		}
		byID[policy.ID.String()] = policy
	}
	return byID, nil
}

// pushdownPolicyIDs mirrors the dashboard listing's visible-policy rule for
// the ClickHouse store, which cannot join risk_policies: enabled policies by
// default, or exactly the requested policy including a disabled one. The
// result is sorted so the pushdown is deterministic in tests and query logs.
func pushdownPolicyIDs(policies map[string]riskrepo.ListRiskFindingPoliciesRow, policyID uuid.NullUUID) []string {
	if policyID.Valid {
		if _, ok := policies[policyID.UUID.String()]; ok {
			return []string{policyID.UUID.String()}
		}
		return nil
	}
	ids := make([]string, 0, len(policies))
	for id, policy := range policies {
		if policy.Enabled {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// useClickHouse decides which store answers, per organization and project.
// A flag evaluation failure fails closed rather than silently switching store
// mid-pagination.
func (s *RiskFindingListService) useClickHouse(ctx context.Context, principal Principal, project ResolvedProject) (bool, error) {
	if s.clickhouse == nil {
		return false, nil
	}
	orgSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || orgSlug == "" {
		return false, ErrUnavailable
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagRiskListFromClickHouse, principal.OrganizationID, feature.OrgProjectGroups(orgSlug, project.Slug))
	if err != nil {
		return false, fmt.Errorf("%w: evaluate finding listing store", ErrUnavailable)
	}
	return evaluation == feature.EvaluationEnabled, nil
}

func (s *RiskFindingListService) List(ctx context.Context, principal Principal, input ListRiskFindingPageInput) (ListRiskFindingPageOutput, error) {
	var zero ListRiskFindingPageOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if principal.OrganizationID == "" || (input.ProjectID != "" && input.ProjectSlug != "") {
		return zero, ErrRiskReadInvalid
	}
	limit, err := riskFindingPageLimit(input.Limit)
	if err != nil {
		return zero, err
	}
	from, err := parseOptionalRiskTime(input.From)
	if err != nil {
		return zero, err
	}
	to, err := parseOptionalRiskTime(input.To)
	if err != nil {
		return zero, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return zero, ErrRiskReadInvalid
	}
	policyID, err := parseOptionalRiskUUID(input.PolicyID)
	if err != nil {
		return zero, err
	}
	chatID, err := parseOptionalRiskUUID(input.ChatID)
	if err != nil {
		return zero, err
	}
	assistantID, err := parseOptionalRiskUUID(input.AssistantID)
	if err != nil {
		return zero, err
	}
	if input.Category != "" && !validRiskCategory(input.Category) {
		return zero, ErrRiskReadInvalid
	}
	if len(input.RuleID) > 128 || len(input.UserID) > 256 || (assistantID.Valid && input.NonAssistant) {
		return zero, ErrRiskReadInvalid
	}
	filters := riskFindingFilters{From: input.From, To: input.To, PolicyID: input.PolicyID, ChatID: input.ChatID, Category: input.Category, RuleID: input.RuleID, UserID: input.UserID, AssistantID: input.AssistantID, NonAssistant: input.NonAssistant, UniqueMatch: input.UniqueMatch}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, fmt.Errorf("resolve risk finding list project: %w", err)
	}
	var cursor *riskCursor
	if input.Cursor != "" {
		decoded, err := s.cursor.Decode(input.Cursor, principal, filters.cursorKind(), project.ID, uuid.Nil)
		if err != nil {
			return zero, err
		}
		cursor = &decoded
	}
	clickhouse, err := s.useClickHouse(ctx, principal, project)
	if err != nil {
		return zero, err
	}
	policies, err := s.visiblePolicies(ctx, principal, project)
	if err != nil {
		return zero, err
	}

	var findings []RiskFinding
	if clickhouse {
		findings, err = s.listFromClickHouse(ctx, principal, project, policies, filters, policyID, from, to, cursor, limit)
	} else {
		findings, err = s.listFromPostgres(ctx, principal, project, policies, filters, policyID, chatID, assistantID, from, to, cursor, limit)
	}
	if err != nil {
		return zero, err
	}

	output := ListRiskFindingPageOutput{Project: riskProject(project), Findings: findings, Limitations: riskFindingListLimitations}
	if len(findings) > limit {
		output.Findings = findings[:limit]
		last := output.Findings[limit-1]
		lastID, parseErr := uuid.Parse(last.ID)
		lastAt, timeErr := time.Parse(time.RFC3339Nano, last.MessageCreatedAt)
		if parseErr != nil || timeErr != nil {
			return zero, fmt.Errorf("%w: finding page cursor keys", ErrUnavailable)
		}
		output.NextCursor, err = s.cursor.Encode(riskCursor{Kind: filters.cursorKind(), OrganizationID: principal.OrganizationID, Binding: principalCursorBinding(principal), ProjectID: project.ID, PolicyID: uuid.Nil, CreatedAt: lastAt, ID: lastID})
		if err != nil {
			return zero, err
		}
	}
	return output, nil
}

func (s *RiskFindingListService) listFromClickHouse(ctx context.Context, principal Principal, project ResolvedProject, policies map[string]riskrepo.ListRiskFindingPoliciesRow, filters riskFindingFilters, policyID uuid.NullUUID, from, to *time.Time, cursor *riskCursor, limit int) ([]RiskFinding, error) {
	policyIDs := pushdownPolicyIDs(policies, policyID)
	if len(policyIDs) == 0 {
		return []RiskFinding{}, nil
	}
	params := chrepo.ListRiskFindingsParams{
		OrganizationID:  principal.OrganizationID,
		ProjectID:       project.ID.String(),
		PolicyIDs:       policyIDs,
		MCPServerID:     "",
		ChatID:          filters.ChatID,
		From:            from,
		To:              to,
		Category:        filters.Category,
		RuleIDSubstr:    filters.RuleID,
		UserIDSubstr:    filters.UserID,
		ExternalUserIDs: nil,
		AssistantID:     filters.AssistantID,
		NonAssistant:    filters.NonAssistant,
		UniqueMatch:     filters.UniqueMatch,
		CursorTime:      nil,
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Limit:           uint64(limit) + 1, // #nosec G115 -- riskFindingPageLimit caps at 50.
	}
	if cursor != nil {
		cursorTime := cursor.CreatedAt
		params.CursorTime = &cursorTime
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	rows, err := s.clickhouse.ListRiskFindings(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%w: list findings", ErrUnavailable)
	}
	findings := make([]RiskFinding, 0, len(rows))
	for _, row := range rows {
		finding := s.finding(policies, row.RiskPolicyID, row.Source, row.RuleID, string(categories.Classify(row.Source, row.RuleID)), row.Tags, row.Confidence, row.MessageCreatedAt)
		finding.ID = row.ID.String()
		finding.PolicyVersion = row.RiskPolicyVersion
		finding.ChatID = row.ChatID
		finding.ChatMessageID = row.ChatMessageID
		finding.UserReference = riskUserReference(s.cursor.key, principal.OrganizationID, row.ExternalUserID)
		// The store holds a partial-mask display string with real boundary
		// characters; only the canonical marker passes through verbatim.
		finding.MatchRedacted = findingEvidence(row.MatchRedacted, principal.OrganizationID)
		findings = append(findings, finding)
	}
	return findings, nil
}

func (s *RiskFindingListService) listFromPostgres(ctx context.Context, principal Principal, project ResolvedProject, policies map[string]riskrepo.ListRiskFindingPoliciesRow, filters riskFindingFilters, policyID, chatID, assistantID uuid.NullUUID, from, to *time.Time, cursor *riskCursor, limit int) ([]RiskFinding, error) {
	cursorAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if cursor != nil {
		cursorAt = pgtype.Timestamptz{Time: cursor.CreatedAt, InfinityModifier: pgtype.Finite, Valid: true}
		cursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	pageLimit := conv.SafeInt32(limit + 1)
	findings := make([]RiskFinding, 0, limit+1)
	if chatID.Valid {
		// The chat-scoped Postgres listing takes no other filter. Refusing the
		// combination is safer than serving a page that silently ignores it.
		narrowed := filters
		narrowed.ChatID = ""
		if narrowed != (riskFindingFilters{}) {
			return nil, ErrRiskReadInvalid
		}
		rows, err := s.postgres.ListRiskResultsByChatFound(ctx, riskrepo.ListRiskResultsByChatFoundParams{ChatID: chatID.UUID, ProjectID: project.ID, CursorMessageCreatedAt: cursorAt, CursorID: cursorID, PageLimit: pageLimit})
		if err != nil {
			return nil, fmt.Errorf("%w: list chat findings", ErrUnavailable)
		}
		for _, row := range rows {
			finding := s.postgresFinding(principal.OrganizationID, policies, row.RiskPolicyID, row.Source, row.RuleID, row.Tags, row.Confidence, row.MessageCreatedAt, row.Match)
			finding.ID = row.ID.String()
			finding.PolicyVersion = row.RiskPolicyVersion
			finding.ChatID = row.ChatID.String()
			finding.ChatMessageID = nullUUIDString(row.ChatMessageID)
			finding.UserReference = riskUserReference(s.cursor.key, principal.OrganizationID, conv.FromPGTextOrEmpty[string](row.ChatUserID))
			findings = append(findings, finding)
		}
		return findings, nil
	}
	rows, err := s.postgres.ListRiskResultsByProjectFound(ctx, riskrepo.ListRiskResultsByProjectFoundParams{
		UniqueMatch:            filters.UniqueMatch,
		PolicyID:               policyID,
		ProjectID:              project.ID,
		FromTime:               pgOptionalTime(from),
		ToTime:                 pgOptionalTime(to),
		RuleID:                 filters.RuleID,
		UserID:                 filters.UserID,
		ExternalUserIds:        nil,
		NonAssistant:           filters.NonAssistant,
		AssistantID:            assistantID,
		Category:               filters.Category,
		CursorMessageCreatedAt: cursorAt,
		CursorID:               cursorID,
		PageLimit:              pageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list project findings", ErrUnavailable)
	}
	for _, row := range rows {
		finding := s.postgresFinding(principal.OrganizationID, policies, row.RiskPolicyID, row.Source, row.RuleID, row.Tags, row.Confidence, row.MessageCreatedAt, row.Match)
		finding.ID = row.ID.String()
		finding.PolicyVersion = row.RiskPolicyVersion
		finding.ChatID = row.ChatID.String()
		finding.ChatMessageID = nullUUIDString(row.ChatMessageID)
		finding.UserReference = riskUserReference(s.cursor.key, principal.OrganizationID, conv.FromPGTextOrEmpty[string](row.ChatUserID))
		findings = append(findings, finding)
	}
	return findings, nil
}

// postgresFinding maps the columns shared by both Postgres listings. Postgres
// rows carry the raw match, so the redaction happens here and nowhere later.
func (s *RiskFindingListService) postgresFinding(org string, policies map[string]riskrepo.ListRiskFindingPoliciesRow, policyID uuid.UUID, source string, ruleID pgtype.Text, tags []string, confidence pgtype.Float8, messageCreatedAt pgtype.Timestamptz, match pgtype.Text) RiskFinding {
	rule := conv.FromPGTextOrEmpty[string](ruleID)
	finding := s.finding(policies, policyID.String(), source, rule, string(categories.Classify(source, rule)), tags, confidence.Float64, messageCreatedAt.Time)
	finding.MatchRedacted = risk.RedactMatchAll(conv.FromPGTextOrEmpty[string](match), org)
	return finding
}

// finding builds the store-independent part of a row. Every label is untrusted
// content bounded by findingLabel; the match is set by the caller because the
// two stores redact from different inputs.
func (s *RiskFindingListService) finding(policies map[string]riskrepo.ListRiskFindingPoliciesRow, policyID, source, ruleID, category string, tags []string, confidence float64, messageCreatedAt time.Time) RiskFinding {
	score := risk.SignalScore(policies[policyID].Score, category)
	bounded := make([]string, 0, len(tags))
	for _, tag := range tags {
		bounded = append(bounded, findingLabel(tag))
	}
	return RiskFinding{
		PolicyID:         policyID,
		Source:           findingLabel(source),
		RuleID:           findingLabel(ruleID),
		Category:         findingLabel(category),
		Severity:         findingSeverity(score),
		Score:            score,
		Confidence:       confidence,
		Tags:             bounded,
		MessageCreatedAt: messageCreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *RiskFindingListService) ListByChat(ctx context.Context, principal Principal, input ListRiskFindingsByChatInput) (ListRiskFindingsByChatOutput, error) {
	var zero ListRiskFindingsByChatOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if principal.OrganizationID == "" || (input.ProjectID != "" && input.ProjectSlug != "") {
		return zero, ErrRiskReadInvalid
	}
	limit, err := riskFindingPageLimit(input.Limit)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, fmt.Errorf("resolve risk findings by chat project: %w", err)
	}
	cursorChat := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if input.Cursor != "" {
		decoded, err := s.cursor.Decode(input.Cursor, principal, riskFindingByChatCursorKind, project.ID, uuid.Nil)
		if err != nil {
			return zero, err
		}
		cursorChat = uuid.NullUUID{UUID: decoded.ID, Valid: true}
	}
	clickhouse, err := s.useClickHouse(ctx, principal, project)
	if err != nil {
		return zero, err
	}

	// Both stores walk chat ids downward from an inclusive cursor and return
	// one extra row, whose chat id seeds the next cursor.
	chats := make([]RiskFindingChatSummary, 0, limit+1)
	if clickhouse {
		policies, err := s.visiblePolicies(ctx, principal, project)
		if err != nil {
			return zero, err
		}
		policyIDs := pushdownPolicyIDs(policies, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
		if len(policyIDs) > 0 {
			params := chrepo.GroupRiskFindingsByChatParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), PolicyIDs: policyIDs, CursorChatID: "", Limit: uint64(limit) + 1} // #nosec G115 -- riskFindingPageLimit caps at 50.
			if cursorChat.Valid {
				params.CursorChatID = cursorChat.UUID.String()
			}
			rows, err := s.clickhouse.GroupRiskFindingsByChat(ctx, params)
			if err != nil {
				return zero, fmt.Errorf("%w: group findings by chat", ErrUnavailable)
			}
			for _, row := range rows {
				chats = append(chats, RiskFindingChatSummary{ChatID: row.ChatID, UserReference: riskUserReference(s.cursor.key, principal.OrganizationID, row.ExternalUserID), FindingsCount: safeFindingCount(row.FindingsCount), LatestDetectedAt: row.LatestDetected.UTC().Format(time.RFC3339Nano)})
			}
		}
	} else {
		rows, err := s.postgres.ListRiskResultsGroupedByChat(ctx, riskrepo.ListRiskResultsGroupedByChatParams{ProjectID: project.ID, Cursor: cursorChat, PageLimit: conv.SafeInt32(limit + 1)})
		if err != nil {
			return zero, fmt.Errorf("%w: group findings by chat", ErrUnavailable)
		}
		for _, row := range rows {
			chats = append(chats, RiskFindingChatSummary{ChatID: row.ChatID.String(), UserReference: riskUserReference(s.cursor.key, principal.OrganizationID, conv.FromPGTextOrEmpty[string](row.ChatUserID)), FindingsCount: row.FindingsCount, LatestDetectedAt: row.LatestDetected.Time.UTC().Format(time.RFC3339Nano)})
		}
	}

	output := ListRiskFindingsByChatOutput{Project: riskProject(project), Chats: chats, Limitations: riskFindingByChatLimitations}
	if len(chats) > limit {
		next := chats[limit]
		output.Chats = chats[:limit]
		nextID, parseErr := uuid.Parse(next.ChatID)
		nextAt, timeErr := time.Parse(time.RFC3339Nano, next.LatestDetectedAt)
		if parseErr != nil || timeErr != nil {
			return zero, fmt.Errorf("%w: chat page cursor keys", ErrUnavailable)
		}
		output.NextCursor, err = s.cursor.Encode(riskCursor{Kind: riskFindingByChatCursorKind, OrganizationID: principal.OrganizationID, Binding: principalCursorBinding(principal), ProjectID: project.ID, PolicyID: uuid.Nil, CreatedAt: nextAt, ID: nextID})
		if err != nil {
			return zero, err
		}
	}
	return output, nil
}

// ruleBreakdownWindow applies the dashboard overview's defaults: to is now,
// from is the start (UTC) of the day six days before to, and the span is at
// most 31 days.
func ruleBreakdownWindow(input GetRiskRuleBreakdownInput, now time.Time) (time.Time, time.Time, error) {
	to := now.UTC()
	if input.To != "" {
		parsed, err := time.Parse(time.RFC3339Nano, input.To)
		if err != nil {
			return time.Time{}, time.Time{}, ErrRiskReadInvalid
		}
		to = parsed.UTC()
	}
	year, month, day := to.Date()
	from := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
	if input.From != "" {
		parsed, err := time.Parse(time.RFC3339Nano, input.From)
		if err != nil {
			return time.Time{}, time.Time{}, ErrRiskReadInvalid
		}
		from = parsed.UTC()
	}
	if !from.Before(to) || to.Sub(from) > riskRuleBreakdownMaxWindow {
		return time.Time{}, time.Time{}, ErrRiskReadInvalid
	}
	return from, to, nil
}

func (s *RiskFindingListService) RuleBreakdown(ctx context.Context, principal Principal, input GetRiskRuleBreakdownInput) (GetRiskRuleBreakdownOutput, error) {
	var zero GetRiskRuleBreakdownOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if principal.OrganizationID == "" || (input.ProjectID != "" && input.ProjectSlug != "") || !validRiskCategory(input.Category) {
		return zero, ErrRiskReadInvalid
	}
	from, to, err := ruleBreakdownWindow(input, s.now())
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, fmt.Errorf("resolve risk rule breakdown project: %w", err)
	}
	clickhouse, err := s.useClickHouse(ctx, principal, project)
	if err != nil {
		return zero, err
	}

	// Both stores read one row past the bound so truncation is detected
	// without an unbounded fetch.
	rules := make([]RiskRuleCount, 0, riskRuleBreakdownLimit+1)
	if clickhouse {
		// Deleted policies' rows linger in ClickHouse until TTL, so the
		// non-deleted set is pushed down where Postgres joins risk_policies.
		policies, err := s.visiblePolicies(ctx, principal, project)
		if err != nil {
			return zero, err
		}
		policyIDs := make([]string, 0, len(policies))
		for id := range policies {
			policyIDs = append(policyIDs, id)
		}
		slices.Sort(policyIDs)
		var rows []chrepo.RiskOverviewRuleCount
		if len(policyIDs) > 0 {
			rows, err = s.clickhouse.ListRiskRuleCountsByCategory(ctx, chrepo.RiskOverviewWindowParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), From: from, To: to}, input.Category, policyIDs, riskRuleBreakdownLimit+1)
			if err != nil {
				return zero, fmt.Errorf("%w: count findings by rule", ErrUnavailable)
			}
		}
		for _, row := range rows {
			rules = append(rules, RiskRuleCount{RuleID: findingLabel(row.RuleID), Source: findingLabel(row.Source), Findings: safeFindingCount(row.Findings)})
		}
	} else {
		rows, err := s.postgres.ListRiskRulesByCategory(ctx, riskrepo.ListRiskRulesByCategoryParams{Category: input.Category, ProjectID: project.ID, FromTime: pgOptionalTime(&from), ToTime: pgOptionalTime(&to), PageLimit: pgtype.Int4{Int32: riskRuleBreakdownLimit + 1, Valid: true}})
		if err != nil {
			return zero, fmt.Errorf("%w: count findings by rule", ErrUnavailable)
		}
		for _, row := range rows {
			rules = append(rules, RiskRuleCount{RuleID: findingLabel(row.RuleID), Source: findingLabel(row.Source), Findings: row.Findings})
		}
	}

	output := GetRiskRuleBreakdownOutput{Project: riskProject(project), Category: input.Category, From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano), Rules: rules, Limitations: riskRuleBreakdownLimitations}
	if len(rules) > riskRuleBreakdownLimit {
		output.Truncated = true
		output.Rules = rules[:riskRuleBreakdownLimit]
	}
	for _, rule := range output.Rules {
		output.Total += rule.Findings
	}
	return output, nil
}

func validRiskCategory(value string) bool {
	return slices.ContainsFunc(categories.All(), func(def categories.Definition) bool { return string(def.Category) == value })
}

// riskCategoryKeys enumerates the category registry into the input schemas so
// the model picks a real key instead of guessing one and reading an empty page
// as "no findings".
func riskCategoryKeys() []string {
	defs := categories.All()
	keys := make([]string, 0, len(defs))
	for _, def := range defs {
		keys = append(keys, string(def.Category))
	}
	return keys
}

func pgOptionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}
	return pgtype.Timestamptz{Time: *value, InfinityModifier: pgtype.Finite, Valid: true}
}

func nullUUIDString(value uuid.NullUUID) string {
	if !value.Valid {
		return ""
	}
	return value.UUID.String()
}

// safeFindingCount clamps a ClickHouse count into the signed range the wire
// type serializes; a page can never legitimately approach it.
func safeFindingCount(value uint64) int64 {
	if value > uint64(1<<62) {
		return 1 << 62
	}
	return int64(value) // #nosec G115 -- bounded above.
}

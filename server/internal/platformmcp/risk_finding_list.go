//nolint:exhaustruct // Optional MCP response fields use their zero values.
package platformmcp

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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

// RiskFindingListService serves individual risk findings, their per-chat
// rollup and per-rule counts for one category from ClickHouse, the same store
// as the dashboard's Risk Events listing. Postgres supplies project and policy
// lookups, since ClickHouse cannot join risk_policies.
type RiskFindingListService struct {
	projects   riskProjectResolver
	policies   findingPolicyReader
	clickhouse RiskFindingListReader
	cursor     *riskCursorCodec
	now        func() time.Time
}

// NewRiskFindingListService panics if the cursor key is missing.
func NewRiskFindingListService(db *pgxpool.Pool, clickhouse RiskFindingListReader, key string) *RiskFindingListService {
	codec := newRiskCursorCodec(key)
	return &RiskFindingListService{projects: postgresRiskProjectResolver{queries: platformrepo.New(db)}, policies: riskrepo.New(db), clickhouse: clickhouse, cursor: codec, now: time.Now}
}

func (s *RiskFindingListService) valid() bool {
	return s != nil
}

type ListRiskFindingPageInput struct {
	ProjectID    string `json:"project_id,omitempty"`
	ProjectSlug  string `json:"project_slug,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	PolicyID     string `json:"policy_id,omitempty"`
	ChatID       string `json:"chat_id,omitempty"`
	MCPServerID  string `json:"mcp_server_id,omitempty"`
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
	ID                 string   `json:"id"`
	PolicyID           string   `json:"policy_id"`
	PolicyVersion      int64    `json:"policy_version"`
	ExecutionID        string   `json:"execution_id,omitempty"`
	MCPServerID        string   `json:"mcp_server_id,omitempty"`
	MetaMCPServerID    string   `json:"meta_mcp_server_id,omitempty"`
	ToolsetID          string   `json:"toolset_id,omitempty"`
	ToolName           string   `json:"tool_name,omitempty"`
	Phase              string   `json:"phase,omitempty"`
	MediationSurface   string   `json:"mediation_surface,omitempty"`
	MCPMethod          string   `json:"mcp_method,omitempty"`
	PrincipalKind      string   `json:"principal_kind,omitempty"`
	IdentityStamped    bool     `json:"identity_stamped,omitempty"`
	EnforcementOutcome string   `json:"enforcement_outcome,omitempty"`
	ChatID             string   `json:"chat_id,omitempty"`
	ChatMessageID      string   `json:"chat_message_id,omitempty"`
	UserReference      string   `json:"user_reference,omitempty"`
	Source             string   `json:"source"`
	RuleID             string   `json:"rule_id"`
	Category           string   `json:"category"`
	Severity           string   `json:"severity"`
	Score              float64  `json:"score"`
	MatchRedacted      string   `json:"match_redacted"`
	Confidence         float64  `json:"confidence"`
	Tags               []string `json:"tags"`
	MessageCreatedAt   string   `json:"message_created_at"`
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
	riskFindingListLimitations   = "Individual live findings ordered by message time (newest first); dismissed and excluded findings and matches from deleted policies are omitted, while disabled policies' historical matches are included. MCP findings include execution attribution and can be filtered by concrete mcp_server_id even when they have no chat. match_redacted is the canonical redaction marker, never the matched value; its length and hash describe the stored display sample. user_reference is an organization-scoped pseudonym shared with list_watchdog_findings. Severity uses the current policy score with the dashboard category fallback. Scanner descriptions and chat titles are withheld because they can quote scanned content; labels are untrusted and bounded. Late ingestion or suppression can change pages; this is not a snapshot. Use get_risk_rule_breakdown to size a finding cluster before paging."
	riskFindingByChatLimitations = "Chats with at least one live finding under a non-deleted policy, including disabled ones, walked by chat id (newest ids first), not by activity. Findings with no chat attribution are not listed. latest_detected_at is detection time and may trail the message time. user_reference is an organization-scoped pseudonym shared with list_watchdog_findings. Use list_risk_findings with chat_id to read one chat's findings."
	riskRuleBreakdownLimitations = "Live finding counts per rule and detection source for one category, keyed on detection time in [from,to). Counts include every non-deleted policy's findings, disabled ones included. At most 1000 rules are returned; truncated reports when more exist, and total covers only the returned rules."
)

// riskFindingFilters is the cursor-bound projection of a listing request. The
// project is bound by the cursor codec separately; everything else that changes
// the page sequence is fingerprinted into the cursor kind so a cursor replayed
// with different filters is refused instead of returning a scrambled page.
type riskFindingFilters struct {
	From     string `json:"from"`
	To       string `json:"to"`
	PolicyID string `json:"policy_id"`
	ChatID   string `json:"chat_id"`
	// omitempty keeps the cursor kind of unfiltered listings unchanged, so
	// cursors issued before this filter existed stay valid.
	MCPServerID  string `json:"mcp_server_id,omitempty"`
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
	policies, err := s.policies.ListRiskFindingPolicies(ctx, riskrepo.ListRiskFindingPoliciesParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, PageLimit: riskFindingPolicyLimit + 1})
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

// pushdownPolicyIDs is the policy set ClickHouse filters on: every visible
// (non-deleted) policy by default, enabled or not, or exactly the requested
// one. Deleted policies' rows linger in ClickHouse until TTL, so they must be
// excluded here. Sorted so the pushdown is deterministic.
func pushdownPolicyIDs(policies map[string]riskrepo.ListRiskFindingPoliciesRow, policyID uuid.NullUUID) []string {
	if policyID.Valid {
		if _, ok := policies[policyID.UUID.String()]; ok {
			return []string{policyID.UUID.String()}
		}
		return nil
	}
	ids := make([]string, 0, len(policies))
	for id := range policies {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
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
	if _, err := parseOptionalRiskUUID(input.ChatID); err != nil {
		return zero, err
	}
	assistantID, err := parseOptionalRiskUUID(input.AssistantID)
	if err != nil {
		return zero, err
	}
	mcpServerID, err := parseOptionalRiskUUID(input.MCPServerID)
	if err != nil {
		return zero, err
	}
	if mcpServerID.Valid {
		// Findings store server ids in canonical lowercase form, and the
		// analytics filter compares strings, so an uppercase spelling that
		// parses would otherwise match nothing.
		input.MCPServerID = mcpServerID.UUID.String()
	}
	if input.Category != "" && !validRiskCategory(input.Category) {
		return zero, ErrRiskReadInvalid
	}
	if len(input.RuleID) > 128 || len(input.UserID) > 256 || (assistantID.Valid && input.NonAssistant) {
		return zero, ErrRiskReadInvalid
	}
	filters := riskFindingFilters{From: input.From, To: input.To, PolicyID: input.PolicyID, ChatID: input.ChatID, MCPServerID: input.MCPServerID, Category: input.Category, RuleID: input.RuleID, UserID: input.UserID, AssistantID: input.AssistantID, NonAssistant: input.NonAssistant, UniqueMatch: input.UniqueMatch}

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
	policies, err := s.visiblePolicies(ctx, principal, project)
	if err != nil {
		return zero, err
	}
	findings, err := s.listFindings(ctx, principal, project, policies, filters, policyID, from, to, cursor, limit)
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

func (s *RiskFindingListService) listFindings(ctx context.Context, principal Principal, project ResolvedProject, policies map[string]riskrepo.ListRiskFindingPoliciesRow, filters riskFindingFilters, policyID uuid.NullUUID, from, to *time.Time, cursor *riskCursor, limit int) ([]RiskFinding, error) {
	policyIDs := pushdownPolicyIDs(policies, policyID)
	if len(policyIDs) == 0 {
		return []RiskFinding{}, nil
	}
	params := chrepo.ListRiskFindingsParams{
		OrganizationID:  principal.OrganizationID,
		ProjectID:       project.ID.String(),
		PolicyIDs:       policyIDs,
		MCPServerID:     filters.MCPServerID,
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
		finding.ExecutionID = row.ExecutionID
		finding.MCPServerID = row.MCPServerID
		finding.MetaMCPServerID = row.MetaMCPServerID
		finding.ToolsetID = row.ToolsetID
		finding.ToolName = findingLabel(row.ToolName)
		finding.Phase = findingLabel(row.Phase)
		finding.MediationSurface = findingLabel(row.MediationSurface)
		finding.MCPMethod = findingLabel(row.MCPMethod)
		finding.PrincipalKind = findingLabel(row.PrincipalKind)
		finding.IdentityStamped = row.IdentityStamped
		finding.EnforcementOutcome = findingLabel(row.EnforcementOutcome)
		finding.ChatID = row.ChatID
		finding.ChatMessageID = row.ChatMessageID
		finding.UserReference = riskUserReference(s.cursor.key, principal.OrganizationID, cmp.Or(row.ExternalUserID, row.UserID))
		// The store holds a partial-mask display string with real boundary
		// characters; only the canonical marker passes through verbatim.
		finding.MatchRedacted = findingEvidence(row.MatchRedacted, principal.OrganizationID)
		findings = append(findings, finding)
	}
	return findings, nil
}

// finding builds the row fields shared with other finding reads. Every label
// is untrusted content bounded by findingLabel.
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
	policies, err := s.visiblePolicies(ctx, principal, project)
	if err != nil {
		return zero, err
	}

	// Walks chat ids downward from an inclusive cursor and reads one extra
	// row, whose chat id seeds the next cursor.
	chats := make([]RiskFindingChatSummary, 0, limit+1)
	if policyIDs := pushdownPolicyIDs(policies, uuid.NullUUID{UUID: uuid.Nil, Valid: false}); len(policyIDs) > 0 {
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
	policies, err := s.visiblePolicies(ctx, principal, project)
	if err != nil {
		return zero, err
	}

	// Reads one row past the bound so truncation is detected without an
	// unbounded fetch.
	rules := make([]RiskRuleCount, 0, riskRuleBreakdownLimit+1)
	if policyIDs := pushdownPolicyIDs(policies, uuid.NullUUID{UUID: uuid.Nil, Valid: false}); len(policyIDs) > 0 {
		rows, err := s.clickhouse.ListRiskRuleCountsByCategory(ctx, chrepo.RiskOverviewWindowParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), From: from, To: to}, input.Category, policyIDs, riskRuleBreakdownLimit+1)
		if err != nil {
			return zero, fmt.Errorf("%w: count findings by rule", ErrUnavailable)
		}
		for _, row := range rows {
			rules = append(rules, RiskRuleCount{RuleID: findingLabel(row.RuleID), Source: findingLabel(row.Source), Findings: safeFindingCount(row.Findings)})
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

// safeFindingCount clamps a ClickHouse count into the signed range the wire
// type serializes; a page can never legitimately approach it.
func safeFindingCount(value uint64) int64 {
	if value > uint64(1<<62) {
		return 1 << 62
	}
	return int64(value) // #nosec G115 -- bounded above.
}

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
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// RiskFindingsReader reads only safe finding metadata, never matched content.
type RiskFindingsReader interface {
	ListWatchdogFindings(context.Context, chrepo.ListRiskFindingsParams) ([]chrepo.WatchdogFinding, error)
	GroupWatchdogFindings(context.Context, chrepo.ListRiskFindingsParams, string) ([]chrepo.WatchdogGroup, error)
	CountWatchdogFindings(context.Context, chrepo.ListRiskFindingsParams) (uint64, error)
}

type findingPolicyReader interface {
	ListRiskPolicies(context.Context, uuid.UUID) ([]riskrepo.RiskPolicy, error)
}

type RiskFindingsService struct {
	projects      riskProjectResolver
	organizations OrganizationSlugResolver
	flags         feature.Provider
	policies      findingPolicyReader
	findings      RiskFindingsReader
	cursor        *riskCursorCodec
	now           func() time.Time
}

func NewRiskFindingsService(db *pgxpool.Pool, findings RiskFindingsReader, flags feature.Provider, organizations OrganizationSlugResolver, key string) *RiskFindingsService {
	codec, err := newRiskCursorCodec(key)
	if db == nil || findings == nil || organizations == nil || err != nil {
		return nil
	}
	return &RiskFindingsService{projects: postgresRiskProjectResolver{queries: platformrepo.New(db)}, organizations: organizations, flags: flags, policies: riskrepo.New(db), findings: findings, cursor: codec, now: time.Now}
}

func (s *RiskFindingsService) valid() bool {
	return s != nil && s.projects != nil && s.organizations != nil && s.policies != nil && s.findings != nil && s.cursor != nil && s.now != nil
}

type ListRiskFindingsInput struct {
	ProjectID   string   `json:"project_id,omitempty"`
	ProjectSlug string   `json:"project_slug,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	Severity    string   `json:"severity,omitempty"`
	GroupBy     []string `json:"group_by,omitempty"`
	Cursor      string   `json:"cursor,omitempty"`
}

type SafeRiskFinding struct {
	ID               string `json:"id"`
	MessageCreatedAt string `json:"message_created_at"`
	PolicyID         string `json:"policy_id"`
	RuleID           string `json:"rule_id"`
	Severity         string `json:"severity"`
	DataType         string `json:"data_type"`
	Team             string `json:"team"`
	App              string `json:"app"`
	User             string `json:"user"`
}

type RiskFindingGroup struct {
	Value string `json:"value"`
	Count uint64 `json:"count"`
}
type RiskFindingGroups struct {
	Dimension string             `json:"dimension"`
	Groups    []RiskFindingGroup `json:"groups"`
	Truncated bool               `json:"truncated"`
}
type ListRiskFindingsOutput struct {
	Project     RiskProject         `json:"project"`
	From        string              `json:"from"`
	To          string              `json:"to"`
	Severity    string              `json:"severity"`
	Findings    []SafeRiskFinding   `json:"findings"`
	TotalCount  uint64              `json:"total_count"`
	Groups      []RiskFindingGroups `json:"groups"`
	NextCursor  string              `json:"next_cursor,omitempty"`
	Limitations string              `json:"limitations"`
}

// Severity follows the dashboard's CVSS bands, using the current policy score.
// Unlike a rule-level signal, each finding has exactly one policy.
func findingSeverity(score float64) string {
	switch {
	case score >= 9:
		return "critical"
	case score >= 7:
		return "high"
	case score >= 4:
		return "medium"
	default:
		return "low"
	}
}

func findingWindow(input ListRiskFindingsInput, now time.Time) (time.Time, time.Time, error) {
	to := now.UTC()
	var err error
	if input.To != "" {
		to, err = time.Parse(time.RFC3339Nano, input.To)
		if err != nil {
			return time.Time{}, time.Time{}, ErrRiskReadInvalid
		}
	}
	from := to.Add(-24 * time.Hour)
	if input.From != "" {
		from, err = time.Parse(time.RFC3339Nano, input.From)
		if err != nil {
			return time.Time{}, time.Time{}, ErrRiskReadInvalid
		}
	}
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour || to.After(now) || from.Before(now.Add(-90*24*time.Hour)) {
		return time.Time{}, time.Time{}, ErrRiskReadInvalid
	}
	if input.Cursor != "" && (input.From == "" || input.To == "") {
		return time.Time{}, time.Time{}, ErrRiskReadInvalid
	}
	return from.UTC(), to.UTC(), nil
}

// Bound untrusted metadata labels without returning control characters. A suffix
// keeps distinct long or normalized labels distinguishable in group counts.
func findingLabel(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	runes := []rune(cleaned)
	if cleaned == value && len(runes) <= 128 {
		return value
	}
	if len(runes) > 128 {
		runes = runes[:128]
	}
	hash := sha256.Sum256([]byte(value))
	return string(runes) + "…#" + hex.EncodeToString(hash[:8])
}

func (s *RiskFindingsService) userReference(org, value string) string {
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.cursor.key)
	_, _ = mac.Write([]byte("watchdog-user:" + org + "\x00" + value))
	return "user:" + hex.EncodeToString(mac.Sum(nil)[:16])
}

func (s *RiskFindingsService) List(ctx context.Context, principal Principal, input ListRiskFindingsInput) (ListRiskFindingsOutput, error) {
	var zero ListRiskFindingsOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if principal.OrganizationID == "" || len(input.Cursor) > 4096 || (input.ProjectID != "" && input.ProjectSlug != "") {
		return zero, ErrRiskReadInvalid
	}
	from, to, err := findingWindow(input, s.now())
	if err != nil {
		return zero, err
	}
	severity := input.Severity
	if severity == "" {
		severity = "critical"
	}
	if !slices.Contains([]string{"critical", "high", "medium", "low", "all"}, severity) {
		return zero, ErrRiskReadInvalid
	}
	dimensions := slices.Clone(input.GroupBy)
	if len(dimensions) == 0 {
		dimensions = []string{"severity"}
	}
	if len(dimensions) > 5 {
		return zero, ErrRiskReadInvalid
	}
	slices.Sort(dimensions)
	for i, dimension := range dimensions {
		if !slices.Contains([]string{"severity", "data_type", "team", "app", "user"}, dimension) || (i > 0 && dimension == dimensions[i-1]) {
			return zero, ErrRiskReadInvalid
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	orgSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || orgSlug == "" {
		return zero, ErrUnavailable
	}
	for _, flag := range []feature.Flag{feature.FlagRiskWatchdog, feature.FlagRiskListFromClickHouse} {
		evaluation, err := feature.EvaluateFlag(ctx, s.flags, flag, principal.OrganizationID, feature.OrgProjectGroups(orgSlug, project.Slug))
		if err != nil {
			return zero, fmt.Errorf("%w: evaluate findings capability", ErrUnavailable)
		}
		if evaluation != feature.EvaluationEnabled {
			return zero, ErrRiskFeatureNotEnabled
		}
	}
	// Bind pagination to the caller, exact project, fixed window and filters.
	binding, _ := json.Marshal([]any{from, to, severity, dimensions})
	hash := sha256.Sum256(binding)
	kind := "findings:" + hex.EncodeToString(hash[:])
	params := chrepo.ListRiskFindingsParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), From: &from, To: &to, Limit: riskReadPageSize + 1}
	if input.Cursor != "" {
		cursor, err := s.cursor.Decode(input.Cursor, principal, kind, project.ID, uuid.Nil)
		if err != nil {
			return zero, err
		}
		params.CursorTime = &cursor.CreatedAt
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	policies, err := s.policies.ListRiskPolicies(ctx, project.ID)
	if err != nil {
		return zero, fmt.Errorf("%w: read finding policies", ErrUnavailable)
	}
	severities := map[string]string{}
	bySeverity := map[string][]string{}
	for _, policy := range policies {
		if !policy.Enabled || policy.Deleted || policy.ProjectID != project.ID || policy.OrganizationID != principal.OrganizationID {
			continue
		}
		band := findingSeverity(policy.Score)
		if severity != "all" && severity != band {
			continue
		}
		id := policy.ID.String()
		params.PolicyIDs = append(params.PolicyIDs, id)
		severities[id] = band
		bySeverity[band] = append(bySeverity[band], id)
	}
	output := ListRiskFindingsOutput{Project: riskProject(project), From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano), Severity: severity, Findings: []SafeRiskFinding{}, Groups: []RiskFindingGroups{}, Limitations: "Live findings from currently enabled, non-deleted policies only; severity uses current policy scores (critical >=9, high >=7, medium >=4, low <4), not confidence or rule-level signal scores. Time bounds apply to message event time [from,to), not detection time. data_type is the stored category, app is captured chat_source, and team is captured team attribution; empty values mean unknown. Users are organization-scoped pseudonyms, not emails. No matched content is returned. Metadata labels are untrusted; long labels are shortened with a hash suffix. Group counts cover the full filtered window, independently per dimension (top 200 buckets). Late ingestion or suppression may change counts between calls; this is not a snapshot. Repeat returned from/to and filters with next_cursor."}
	if len(params.PolicyIDs) == 0 {
		for _, dimension := range dimensions {
			output.Groups = append(output.Groups, RiskFindingGroups{Dimension: dimension, Groups: []RiskFindingGroup{}})
		}
		return output, nil
	}
	output.TotalCount, err = s.findings.CountWatchdogFindings(ctx, params)
	if err != nil {
		return zero, fmt.Errorf("%w: count findings", ErrUnavailable)
	}
	rows, err := s.findings.ListWatchdogFindings(ctx, params)
	if err != nil {
		return zero, fmt.Errorf("%w: list findings", ErrUnavailable)
	}
	if len(rows) > riskReadPageSize {
		rows = rows[:riskReadPageSize]
		last := rows[len(rows)-1]
		output.NextCursor, err = s.cursor.Encode(riskCursor{Kind: kind, OrganizationID: principal.OrganizationID, Binding: principalCursorBinding(principal), ProjectID: project.ID, CreatedAt: last.MessageCreatedAt, ID: last.ID})
		if err != nil {
			return zero, err
		}
	}
	for _, row := range rows {
		output.Findings = append(output.Findings, SafeRiskFinding{ID: row.ID.String(), MessageCreatedAt: row.MessageCreatedAt.UTC().Format(time.RFC3339Nano), PolicyID: row.PolicyID, RuleID: findingLabel(row.RuleID), Severity: severities[row.PolicyID], DataType: findingLabel(row.Category), Team: findingLabel(row.Team), App: findingLabel(row.App), User: s.userReference(principal.OrganizationID, row.User)})
	}
	for _, dimension := range dimensions {
		group := RiskFindingGroups{Dimension: dimension, Groups: []RiskFindingGroup{}}
		if dimension == "severity" {
			for _, band := range []string{"critical", "high", "medium", "low"} {
				if len(bySeverity[band]) == 0 {
					continue
				}
				p := params
				p.PolicyIDs = bySeverity[band]
				count := output.TotalCount
				if len(bySeverity) > 1 {
					count, err = s.findings.CountWatchdogFindings(ctx, p)
					if err != nil {
						return zero, fmt.Errorf("%w: group findings", ErrUnavailable)
					}
				}
				if count > 0 {
					group.Groups = append(group.Groups, RiskFindingGroup{Value: band, Count: count})
				}
			}
		} else {
			buckets, err := s.findings.GroupWatchdogFindings(ctx, params, dimension)
			if err != nil {
				return zero, fmt.Errorf("%w: group findings", ErrUnavailable)
			}
			if len(buckets) > 200 {
				group.Truncated = true
				buckets = buckets[:200]
			}
			for _, bucket := range buckets {
				value := findingLabel(bucket.Value)
				if dimension == "user" {
					value = s.userReference(principal.OrganizationID, bucket.Value)
				}
				group.Groups = append(group.Groups, RiskFindingGroup{Value: value, Count: bucket.Count})
			}
		}
		sort.Slice(group.Groups, func(i, j int) bool {
			if group.Groups[i].Count == group.Groups[j].Count {
				return group.Groups[i].Value < group.Groups[j].Value
			}
			return group.Groups[i].Count > group.Groups[j].Count
		})
		output.Groups = append(output.Groups, group)
	}
	return output, nil
}

//nolint:exhaustruct // Optional MCP response fields use their zero values.
package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// RiskFindingsReader reads grouped metadata and stored display samples, never raw matches.
type RiskFindingsReader interface {
	ListWatchdogAlerts(context.Context, chrepo.RiskSignalWindowParams, uint64) ([]chrepo.WatchdogAlert, error)
	GroupWatchdogAlerts(context.Context, chrepo.RiskSignalWindowParams, []string, string) ([]chrepo.WatchdogAlertGroup, error)
}

// Bound the current policy metadata lookup. One extra row detects overflow;
// incomplete policy score sets must never silently understate severity.
const riskFindingPolicyLimit = 1000

type findingPolicyReader interface {
	ListRiskFindingPolicies(context.Context, riskrepo.ListRiskFindingPoliciesParams) ([]riskrepo.ListRiskFindingPoliciesRow, error)
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
}

// WatchdogAlert is one rule-level signal, not an individual matched finding.
type WatchdogAlert struct {
	RuleID          string              `json:"rule_id"`
	DataType        string              `json:"data_type"`
	Severity        string              `json:"severity"`
	Score           float64             `json:"score"`
	Count           uint64              `json:"count"`
	UsersAffected   uint64              `json:"users_affected"`
	ClientsAffected uint64              `json:"clients_affected"`
	Clients         []string            `json:"clients"`
	FirstSeen       string              `json:"first_seen"`
	LastSeen        string              `json:"last_seen"`
	Evidence        string              `json:"evidence"`
	GroupBy         []RiskFindingGroups `json:"group_by,omitempty"`
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
	Project     RiskProject     `json:"project"`
	From        string          `json:"from"`
	To          string          `json:"to"`
	Severity    string          `json:"severity"`
	Groups      []WatchdogAlert `json:"groups"`
	TotalCount  uint64          `json:"total_count"`
	TotalAlerts int             `json:"total_alerts"`
	Truncated   bool            `json:"truncated"`
	Limitations string          `json:"limitations"`
}

var canonicalFindingEvidence = regexp.MustCompile(`\A<redacted len=[1-9][0-9]* sha=[0-9a-f]{8}>\z`)

// Stored display samples can contain identifiers or partial secrets. Only the
// canonical full-redaction marker is safe to preserve verbatim.
func findingEvidence(sample, org string) string {
	if sample == "<redacted len=0>" || (len(sample) <= 128 && canonicalFindingEvidence.MatchString(sample)) {
		return sample
	}
	return risk.RedactMatchAll(sample, org)
}

// Severity follows the dashboard's CVSS bands, using the current policy score.
// Scores are shared with the dashboard signal implementation.
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
	return from.UTC(), to.UTC(), nil
}

// Bound untrusted metadata labels without returning control characters. A suffix
// keeps distinct long or normalized labels distinguishable in group counts.
func findingLabel(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
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
	if principal.OrganizationID == "" || (input.ProjectID != "" && input.ProjectSlug != "") {
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
		return zero, fmt.Errorf("resolve risk findings project: %w", err)
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
	params := chrepo.RiskSignalWindowParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID.String(),
		MCPServerID:    "",
		WideFrom:       time.Time{},
		From:           from,
		To:             to,
	}
	policies, err := s.policies.ListRiskFindingPolicies(ctx, riskrepo.ListRiskFindingPoliciesParams{
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
		PageLimit:      riskFindingPolicyLimit + 1,
	})
	if err != nil {
		return zero, fmt.Errorf("%w: read finding policies", ErrUnavailable)
	}
	if len(policies) > riskFindingPolicyLimit {
		return zero, fmt.Errorf("%w: finding policy limit exceeded", ErrUnavailable)
	}
	scores := map[string]float64{}
	for _, policy := range policies {
		if policy.Deleted || policy.ProjectID != project.ID || policy.OrganizationID != principal.OrganizationID {
			continue
		}
		scores[policy.ID.String()] = policy.Score
	}
	rows, err := s.findings.ListWatchdogAlerts(ctx, params, chrepo.WatchdogAlertLimit)
	if err != nil {
		return zero, fmt.Errorf("%w: list alerts", ErrUnavailable)
	}
	if len(rows) >= chrepo.WatchdogAlertLimit {
		return zero, fmt.Errorf("%w: alert limit exceeded; narrow the time window", ErrUnavailable)
	}
	output := ListRiskFindingsOutput{Project: riskProject(project), From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano), Severity: severity, Groups: []WatchdogAlert{}, Limitations: "One alert per rule across all live findings, including disabled policy matches. Detection time is created_at in [from,to). Severity uses the maximum current nondeleted contributing policy score, with the dashboard category fallback. first_seen and last_seen are message timestamps, which may fall outside the detection window. The len=0 evidence marker means no stored sample. Clients are distinct observed chat_source surfaces, not devices or OAuth clients; empty attribution is unknown and excluded from affected counts. Evidence is one fully redacted stored display sample, not original raw matched content; its length may describe that display sample. Optional group_by contains independent per-alert finding histograms (top 200 buckets), not dashboard sections. User buckets are organization-scoped pseudonyms. Labels are untrusted. At most 100 alerts are returned; total_count and total_alerts cover every severity-matching alert in the window. More than 1000 rules fails closed: narrow the window. Late ingestion or suppression can change counts; this is not a snapshot."}
	for _, row := range rows {
		var score float64
		for _, id := range row.PolicyIDs {
			score = max(score, scores[id])
		}
		score = risk.SignalScore(score, row.Category)
		band := findingSeverity(score)
		if severity != "all" && severity != band {
			continue
		}
		output.TotalCount += row.FindingCount
		output.TotalAlerts++
		output.Groups = append(output.Groups, WatchdogAlert{RuleID: row.RuleID, DataType: findingLabel(row.Category), Severity: band, Score: score, Count: row.FindingCount, UsersAffected: row.UsersAffected, ClientsAffected: uint64(len(row.Clients)), Evidence: findingEvidence(row.SampleEvidence, principal.OrganizationID), Clients: safeFindingClients(row.Clients), FirstSeen: row.FirstSeen.UTC().Format(time.RFC3339Nano), LastSeen: row.LastSeen.UTC().Format(time.RFC3339Nano)})
	}
	sort.Slice(output.Groups, func(i, j int) bool {
		a, b := output.Groups[i], output.Groups[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.RuleID < b.RuleID
	})
	if len(output.Groups) > 100 {
		output.Truncated = true
		output.Groups = output.Groups[:100]
	}
	// Fetch each requested attribution dimension once for the selected rules.
	// The repository caps each rule's buckets at 201 (one overflow sentinel).
	ruleIDs := make([]string, 0, len(output.Groups))
	for _, alert := range output.Groups {
		ruleIDs = append(ruleIDs, alert.RuleID)
	}
	for _, dimension := range dimensions {
		byRule := map[string][]RiskFindingGroup{}
		if dimension != "severity" && len(ruleIDs) > 0 {
			buckets, err := s.findings.GroupWatchdogAlerts(ctx, params, ruleIDs, dimension)
			if err != nil {
				return zero, fmt.Errorf("%w: group alerts", ErrUnavailable)
			}
			for _, bucket := range buckets {
				value := findingLabel(bucket.Value)
				if dimension == "user" {
					value = s.userReference(principal.OrganizationID, bucket.Value)
				}
				byRule[bucket.RuleID] = append(byRule[bucket.RuleID], RiskFindingGroup{Value: value, Count: bucket.Count})
			}
		}
		for i := range output.Groups {
			alert := &output.Groups[i]
			group := RiskFindingGroups{Dimension: dimension, Groups: []RiskFindingGroup{}}
			switch dimension {
			case "severity":
				group.Groups = append(group.Groups, RiskFindingGroup{Value: alert.Severity, Count: alert.Count})
			default:
				if buckets := byRule[alert.RuleID]; len(buckets) > 0 {
					group.Groups = buckets
				}
				if len(group.Groups) > 200 {
					group.Truncated = true
					group.Groups = group.Groups[:200]
				}
			}
			alert.GroupBy = append(alert.GroupBy, group)
		}
	}
	for i := range output.Groups {
		output.Groups[i].RuleID = findingLabel(output.Groups[i].RuleID)
	}
	return output, nil
}

func safeFindingClients(clients []string) []string {
	out := make([]string, 0, len(clients))
	for _, client := range clients {
		out = append(out, findingLabel(client))
	}
	return out
}

package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type findingProjects struct {
	project ResolvedProject
	calls   []riskProjectCall
	err     error
}

func (s *findingProjects) Resolve(_ context.Context, org, id, slug string) (ResolvedProject, error) {
	s.calls = append(s.calls, riskProjectCall{org, id, slug})
	return s.project, s.err
}

type findingPolicies struct {
	rows   []riskrepo.ListRiskFindingPoliciesRow
	calls  int
	params []riskrepo.ListRiskFindingPoliciesParams
}

func (s *findingPolicies) ListRiskFindingPolicies(_ context.Context, p riskrepo.ListRiskFindingPoliciesParams) ([]riskrepo.ListRiskFindingPoliciesRow, error) {
	s.calls++
	s.params = append(s.params, p)
	return s.rows, nil
}

type findingReader struct {
	rows       []chrepo.WatchdogAlert
	buckets    []chrepo.WatchdogAlertGroup
	params     []chrepo.RiskSignalWindowParams
	rules      [][]string
	dimensions []string
	limits     []uint64
	err        error
}

func (s *findingReader) ListWatchdogAlerts(_ context.Context, p chrepo.RiskSignalWindowParams, limit uint64) ([]chrepo.WatchdogAlert, error) {
	s.params = append(s.params, p)
	s.limits = append(s.limits, limit)
	return s.rows, s.err
}
func (s *findingReader) GroupWatchdogAlerts(_ context.Context, p chrepo.RiskSignalWindowParams, rules []string, dimension string) ([]chrepo.WatchdogAlertGroup, error) {
	s.params = append(s.params, p)
	s.rules = append(s.rules, rules)
	s.dimensions = append(s.dimensions, dimension)
	return s.buckets, s.err
}

func findingsFixture(t *testing.T) (*RiskFindingsService, *findingReader, *findingPolicies) {
	t.Helper()
	project := ResolvedProject{ID: uuid.New(), Slug: "default", Name: "Project"}
	policies := &findingPolicies{rows: []riskrepo.ListRiskFindingPoliciesRow{
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: true, Score: 9.5},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: true, Score: 7},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: false, Score: 10},
		{ID: uuid.New(), ProjectID: uuid.New(), OrganizationID: "other", Enabled: true, Score: 10},
	}}
	reader := &findingReader{rows: []chrepo.WatchdogAlert{{RuleID: "test-rule", Category: "pii", PolicyIDs: []string{policies.rows[0].ID.String()}, FindingCount: 12, UsersAffected: 2, Clients: []string{"browser"}, SampleEvidence: "sensitive@example.invalid", FirstSeen: riskAnalysisTestNow.Add(-48 * time.Hour), LastSeen: riskAnalysisTestNow.Add(-time.Hour)}}, buckets: []chrepo.WatchdogAlertGroup{{RuleID: "test-rule", Value: "", Count: 4}, {RuleID: "test-rule", Value: "test-user", Count: 8}}}
	codec, err := newRiskCursorCodec("test-key")
	require.NoError(t, err)
	return &RiskFindingsService{projects: &findingProjects{project: project}, organizations: riskMutationOrganizationResolver{slug: "org"}, flags: &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}, policies: policies, findings: reader, cursor: codec, now: func() time.Time { return riskAnalysisTestNow }}, reader, policies
}

func TestRiskFindingsDefaultsAndGroups(t *testing.T) {
	t.Parallel()
	s, r, p := findingsFixture(t)
	out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{GroupBy: []string{"severity", "data_type", "team", "app", "user"}})
	require.NoError(t, err)
	require.Equal(t, "critical", out.Severity)
	require.Equal(t, riskAnalysisTestNow.Add(-24*time.Hour).Format(time.RFC3339), out.From)
	require.EqualValues(t, 12, out.TotalCount)
	require.Equal(t, 1, out.TotalAlerts)
	require.Len(t, out.Groups, 1)
	alert := out.Groups[0]
	require.Len(t, alert.GroupBy, 5)
	require.EqualValues(t, 2, alert.UsersAffected)
	require.EqualValues(t, 1, alert.ClientsAffected)
	require.Equal(t, []string{"browser"}, alert.Clients)
	require.Len(t, r.params, 5, "one alert query and four attribution queries")
	require.Equal(t, "critical", alert.Severity)
	require.NotContains(t, alert.Evidence, "sensitive")
	require.Equal(t, "test-rule", alert.RuleID)
	require.Equal(t, riskAnalysisTestNow.Add(-48*time.Hour).Format(time.RFC3339Nano), alert.FirstSeen)
	require.Equal(t, riskAnalysisTestNow.Add(-time.Hour).Format(time.RFC3339Nano), alert.LastSeen)
	for _, params := range r.params {
		require.Equal(t, "<ORG_ID>", params.OrganizationID)
		require.Equal(t, p.rows[0].ProjectID.String(), params.ProjectID)
		require.Equal(t, riskAnalysisTestNow.Add(-24*time.Hour), params.From)
	}
	require.NotEqual(t, s.userReference("other", "test-user"), s.userReference("<ORG_ID>", "test-user"))
}

func TestRiskFindingsSignalScores(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		ids            []int
		category, band string
		score          float64
	}{
		{"max contributing policy", []int{0, 1}, "pii", "critical", 9.5},
		{"disabled policy contributes", []int{2}, "pii", "critical", 10},
		{"foreign policy ignored", []int{3}, "pii", "medium", 5.5},
		{"category fallback", nil, "secrets", "high", 8.5},
		{"unknown fallback", nil, "future", "medium", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, r, p := findingsFixture(t)
			r.rows[0].Category = tc.category
			r.rows[0].PolicyIDs = nil
			for _, i := range tc.ids {
				r.rows[0].PolicyIDs = append(r.rows[0].PolicyIDs, p.rows[i].ID.String())
			}
			out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{Severity: "all"})
			require.NoError(t, err)
			require.InDelta(t, tc.score, out.Groups[0].Score, 0.001)
			require.Equal(t, tc.band, out.Groups[0].Severity)
			out, err = s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{Severity: "low"})
			require.NoError(t, err)
			require.Empty(t, out.Groups)
			require.Zero(t, out.TotalCount)
		})
	}
	s, r, p := findingsFixture(t)
	p.rows[0].Deleted = true
	out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{Severity: "all"})
	require.NoError(t, err)
	require.InDelta(t, 5.5, out.Groups[0].Score, 0.001)
	require.Len(t, r.params, 1)
	p.rows = nil
	out, err = s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{Severity: "all"})
	require.NoError(t, err)
	require.Len(t, out.Groups, 1)
}

func TestRiskFindingsEvidence(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"https://private.example.invalid/token", "a***@private.example.invalid", "<redacted len=12 sha=0123abcd>suffix", "<redacted len=12 sha=ABCDEF12>", "<redacted len=0 sha=0123abcd>", "<redacted len=01 sha=0123abcd>", "<redacted len=12 sha=0123abcd>\n", "<redacted len=" + strings.Repeat("1", 200) + " sha=0123abcd>"} {
		got := findingEvidence(v, "<ORG_ID>")
		require.Equal(t, risk.RedactMatchAll(v, "<ORG_ID>"), got)
		require.NotEqual(t, v, got)
	}
	for _, v := range []string{"<redacted len=0>", "<redacted len=12 sha=0123abcd>"} {
		require.Equal(t, v, findingEvidence(v, "<ORG_ID>"))
	}
	require.Equal(t, "<redacted len=0>", findingEvidence("", "<ORG_ID>"))
}

func TestRiskFindingsPolicyLookupBounds(t *testing.T) {
	t.Parallel()

	for _, count := range []int{1000, 1001} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()

			s, reader, policies := findingsFixture(t)
			policy := policies.rows[0]
			policies.rows = []riskrepo.ListRiskFindingPoliciesRow{policy}
			for len(policies.rows) < count {
				row := policy
				row.ID = uuid.New()
				policies.rows = append(policies.rows, row)
			}
			out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{GroupBy: []string{"team"}})
			require.Equal(t, []riskrepo.ListRiskFindingPoliciesParams{{
				OrganizationID: "<ORG_ID>",
				ProjectID:      policy.ProjectID,
				PageLimit:      1001,
			}}, policies.params)
			if count == 1001 {
				require.ErrorIs(t, err, ErrUnavailable)
				require.Empty(t, reader.params)
			} else {
				require.NoError(t, err)
				require.Len(t, out.Groups, 1)
				require.NotEmpty(t, reader.params)
				for _, params := range reader.params {
					require.False(t, params.From.IsZero())
				}
			}
		})
	}
}

func TestRiskFindingsProjectResolverError(t *testing.T) {
	t.Parallel()

	s, reader, policies := findingsFixture(t)
	projects, ok := s.projects.(*findingProjects)
	require.True(t, ok)
	sentinel := errors.New("project resolver failed")
	projects.err = sentinel
	_, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{})
	require.ErrorIs(t, err, sentinel)
	require.NotSame(t, sentinel, err, "resolver errors must be wrapped with service context")
	require.Contains(t, err.Error(), "resolve risk findings project")
	require.Zero(t, policies.calls)
	require.Empty(t, reader.params)
}

func TestRiskFindingsValidationAndGates(t *testing.T) {
	t.Parallel()

	for _, input := range []ListRiskFindingsInput{{From: "invalid"}, {From: riskAnalysisTestNow.Format(time.RFC3339)}, {To: riskAnalysisTestNow.Add(time.Hour).Format(time.RFC3339)}, {From: riskAnalysisTestNow.Add(-32 * 24 * time.Hour).Format(time.RFC3339)}, {Severity: "urgent"}, {GroupBy: []string{"email"}}, {GroupBy: []string{"team", "team"}}, {ProjectID: uuid.NewString(), ProjectSlug: "default"}} {
		s, r, p := findingsFixture(t)
		_, err := s.List(t.Context(), testRiskPrincipal("user"), input)
		require.ErrorIs(t, err, ErrRiskReadInvalid)
		require.Empty(t, r.params)
		require.Zero(t, p.calls)
	}
	for _, evaluation := range []feature.Evaluation{feature.EvaluationDisabled, feature.EvaluationIndeterminate} {
		s, r, p := findingsFixture(t)
		s.flags = &riskMutationFlagProvider{evaluation: evaluation}
		_, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{})
		require.ErrorIs(t, err, ErrRiskFeatureNotEnabled)
		require.Empty(t, r.params)
		require.Zero(t, p.calls)
	}
	s, r, p := findingsFixture(t)
	projects, ok := s.projects.(*findingProjects)
	require.True(t, ok)
	projects.err = ErrRiskReadNotFound
	_, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{ProjectID: uuid.NewString()})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	require.Zero(t, p.calls)
	require.Empty(t, r.params)
	s, r, _ = findingsFixture(t)
	r.err = errors.New("private database detail")
	_, err = s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotContains(t, err.Error(), "private database detail")
}

func TestRiskFindingsGroupBounds(t *testing.T) {
	t.Parallel()
	s, r, _ := findingsFixture(t)
	row := r.rows[0]
	r.rows = nil
	for i := range 1000 {
		v := row
		v.RuleID = strconv.Itoa(i)
		r.rows = append(r.rows, v)
	}
	r.buckets = nil
	for i := range 201 {
		r.buckets = append(r.buckets, chrepo.WatchdogAlertGroup{RuleID: "0", Value: strconv.Itoa(i), Count: 1})
	}
	out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{GroupBy: []string{"team"}})
	require.NoError(t, err)
	require.Len(t, out.Groups, 100)
	require.True(t, out.Truncated)
	require.Equal(t, 1000, out.TotalAlerts)
	require.EqualValues(t, 12000, out.TotalCount)
	require.Len(t, r.params, 2, "one aggregate query and one batched breakdown query")
	require.True(t, out.Groups[0].GroupBy[0].Truncated)
	require.Len(t, out.Groups[0].GroupBy[0].Groups, 200)
	r.rows = append(r.rows, row)
	out, err = s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Empty(t, out)
}

func TestRiskFindingsMCPInProcess(t *testing.T) {
	t.Parallel()

	s, reader, _ := findingsFixture(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "findings-test", Version: "1"}, nil)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerRiskFindingsTool(reg, s)
	descriptor := descriptorByName(t, reg, "list_watchdog_findings")
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
	require.Equal(t, ProjectScopeDefaultable, descriptor.Meta.ProjectScope)
	require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
	require.True(t, descriptor.Annotations.ReadOnlyHint)
	require.True(t, validRiskTelemetryTool(descriptor.Name))
	require.True(t, validOptionalFeedbackTool(descriptor.Name))
	require.NotContains(t, string(descriptor.InputSchema), `"cursor"`)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(ContextWithPrincipal(ctx, testRiskPrincipal("user")), method, req)
		}
	})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	require.NoError(t, err)
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "digest-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), ct, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_watchdog_findings", Arguments: map[string]any{"group_by": []string{"severity", "team"}}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var out ListRiskFindingsOutput
	require.NoError(t, json.Unmarshal(encoded, &out))
	require.EqualValues(t, 12, out.TotalCount)
	require.Len(t, out.Groups, 1)
	require.Len(t, out.Groups[0].GroupBy, 2)
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_watchdog_findings", Arguments: map[string]any{"cursor": "obsolete"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_watchdog_findings", Arguments: map[string]any{"organization_id": "other-org"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	calls := len(reader.params)
	reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_watchdog_findings", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, reader.params, calls, "authorization denial must not reach storage")
}

type findingFlagGate struct {
	riskMutationFlagProvider
	disabled feature.Flag
}

func (f *findingFlagGate) EvaluateFlag(_ context.Context, flag feature.Flag, _ string, _ map[string]string) (feature.Evaluation, error) {
	if flag == f.disabled {
		return feature.EvaluationDisabled, nil
	}
	return feature.EvaluationEnabled, nil
}

func TestRiskFindingsIndependentFeatureGates(t *testing.T) {
	t.Parallel()

	for _, flag := range []feature.Flag{feature.FlagRiskWatchdog, feature.FlagRiskListFromClickHouse} {
		s, r, p := findingsFixture(t)
		s.flags = &findingFlagGate{disabled: flag}
		_, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{})
		require.ErrorIs(t, err, ErrRiskFeatureNotEnabled)
		require.Empty(t, r.params)
		require.Zero(t, p.calls)
	}
	s, r, _ := findingsFixture(t)
	s.flags = &riskMutationFlagProvider{err: errors.New("flag unavailable")}
	_, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Empty(t, r.params)
}

func TestRiskFindingsWindowAndLabels(t *testing.T) {
	t.Parallel()

	end := riskAnalysisTestNow.Add(-time.Hour)
	from, to, err := findingWindow(ListRiskFindingsInput{To: end.Format(time.RFC3339)}, riskAnalysisTestNow)
	require.NoError(t, err)
	require.Equal(t, end, to)
	require.Equal(t, end.Add(-24*time.Hour), from)
	from, to, err = findingWindow(ListRiskFindingsInput{From: end.Add(-time.Hour).Format(time.RFC3339), To: end.Format(time.RFC3339)}, riskAnalysisTestNow)
	require.NoError(t, err)
	require.Equal(t, time.Hour, to.Sub(from))
	require.Empty(t, findingLabel(""))
	require.Equal(t, "engineering", findingLabel("engineering"))
	a := findingLabel(strings.Repeat("a", 1000))
	b := findingLabel(strings.Repeat("a", 1001))
	require.Less(t, len(a), 160)
	require.NotEqual(t, a, b)
	require.NotContains(t, findingLabel("unsafe\nlabel"), "\n")
}

func TestRiskFindingsStub(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "findings-stub", Version: "1"}, nil)
	reg := newRegistrar(server)
	registerRiskFindingsTool(reg, nil)
	d := descriptorByName(t, reg, "list_watchdog_findings")
	require.Contains(t, d.Description, "unavailable in this deployment")
	_, err := d.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
}

func TestRiskFindingsBreakdownsUseSelectedRawRuleIDs(t *testing.T) {
	t.Parallel()
	s, r, p := findingsFixture(t)
	r.rows[0].RuleID = "rule\nwith-control"
	high := r.rows[0]
	high.RuleID = "high-rule"
	high.PolicyIDs = []string{p.rows[1].ID.String()}
	r.rows = append(r.rows, high)
	r.buckets = []chrepo.WatchdogAlertGroup{{RuleID: r.rows[0].RuleID, Value: "pii", Count: 8}, {RuleID: r.rows[0].RuleID, Value: "secrets", Count: 4}}
	out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{GroupBy: []string{"data_type"}})
	require.NoError(t, err)
	require.Len(t, out.Groups, 1)
	require.EqualValues(t, 12, out.TotalCount)
	require.Equal(t, [][]string{{"rule\nwith-control"}}, r.rules)
	require.Equal(t, []string{"data_type"}, r.dimensions)
	require.Equal(t, []uint64{chrepo.WatchdogAlertLimit}, r.limits)
	require.Equal(t, []RiskFindingGroup{{Value: "pii", Count: 8}, {Value: "secrets", Count: 4}}, out.Groups[0].GroupBy[0].Groups)
	require.NotContains(t, out.Groups[0].RuleID, "\n")
}

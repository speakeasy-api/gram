//nolint:exhaustruct // Focused fixtures intentionally omit unused fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
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
	rows  []riskrepo.RiskPolicy
	calls int
}

func (s *findingPolicies) ListRiskPolicies(_ context.Context, _ uuid.UUID) ([]riskrepo.RiskPolicy, error) {
	s.calls++
	return s.rows, nil
}

type findingReader struct {
	rows    []chrepo.WatchdogFinding
	buckets []chrepo.WatchdogGroup
	params  []chrepo.ListRiskFindingsParams
	err     error
}

func (s *findingReader) ListWatchdogFindings(_ context.Context, p chrepo.ListRiskFindingsParams) ([]chrepo.WatchdogFinding, error) {
	s.params = append(s.params, p)
	return s.rows, s.err
}
func (s *findingReader) CountWatchdogFindings(_ context.Context, p chrepo.ListRiskFindingsParams) (uint64, error) {
	s.params = append(s.params, p)
	return 12, s.err
}
func (s *findingReader) GroupWatchdogFindings(_ context.Context, p chrepo.ListRiskFindingsParams, _ string) ([]chrepo.WatchdogGroup, error) {
	s.params = append(s.params, p)
	return s.buckets, s.err
}

func findingsFixture(t *testing.T) (*RiskFindingsService, *findingReader, *findingPolicies) {
	t.Helper()
	project := ResolvedProject{ID: uuid.New(), Slug: "default", Name: "Project"}
	policies := &findingPolicies{rows: []riskrepo.RiskPolicy{
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: true, Score: 9.5},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: true, Score: 7},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: false, Score: 10},
		{ID: uuid.New(), ProjectID: uuid.New(), OrganizationID: "other", Enabled: true, Score: 10},
	}}
	reader := &findingReader{rows: []chrepo.WatchdogFinding{{ID: uuid.New(), MessageCreatedAt: riskAnalysisTestNow.Add(-time.Hour), PolicyID: policies.rows[0].ID.String(), RuleID: "test-rule", Category: "pii", Team: "engineering", App: "browser", User: "test-user"}}, buckets: []chrepo.WatchdogGroup{{Value: "", Count: 4}, {Value: "test-user", Count: 8}}}
	codec, err := newRiskCursorCodec("test-key")
	require.NoError(t, err)
	return &RiskFindingsService{projects: &findingProjects{project: project}, organizations: riskMutationOrganizationResolver{slug: "org"}, flags: &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}, policies: policies, findings: reader, cursor: codec, now: func() time.Time { return riskAnalysisTestNow }}, reader, policies
}

func TestRiskFindingsDefaultsAndGroups(t *testing.T) {
	s, r, p := findingsFixture(t)
	out, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{GroupBy: []string{"severity", "data_type", "team", "app", "user"}})
	require.NoError(t, err)
	require.Equal(t, "critical", out.Severity)
	require.Equal(t, riskAnalysisTestNow.Add(-24*time.Hour).Format(time.RFC3339), out.From)
	require.EqualValues(t, 12, out.TotalCount)
	require.Len(t, out.Findings, 1)
	require.Len(t, out.Groups, 5)
	require.Equal(t, "critical", out.Findings[0].Severity)
	require.Equal(t, "pii", out.Findings[0].DataType)
	require.NotEqual(t, "test-user", out.Findings[0].User)
	require.Equal(t, s.userReference("<ORG_ID>", "test-user"), out.Findings[0].User)
	require.NotEqual(t, s.userReference("other", "test-user"), out.Findings[0].User)
	for _, params := range r.params {
		require.Equal(t, "<ORG_ID>", params.OrganizationID)
		require.Equal(t, p.rows[0].ProjectID.String(), params.ProjectID)
		require.Equal(t, []string{p.rows[0].ID.String()}, params.PolicyIDs)
	}
	require.Equal(t, []riskProjectCall{{organizationID: "<ORG_ID>"}}, s.projects.(*findingProjects).calls)
	userGroups := out.Groups[4]
	require.Equal(t, "user", userGroups.Dimension)
	require.Equal(t, out.Findings[0].User, userGroups.Groups[0].Value)
	require.Empty(t, userGroups.Groups[1].Value)
}

func TestRiskFindingsSeverityFilters(t *testing.T) {
	for _, tc := range []struct {
		severity string
		ids      int
	}{{"critical", 1}, {"high", 1}, {"medium", 0}, {"low", 0}, {"all", 2}} {
		t.Run(tc.severity, func(t *testing.T) {
			s, r, _ := findingsFixture(t)
			_, err := s.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingsInput{Severity: tc.severity})
			require.NoError(t, err)
			if tc.ids == 0 {
				require.Empty(t, r.params)
			} else {
				require.Len(t, r.params[0].PolicyIDs, tc.ids)
			}
		})
	}
	for _, tc := range []struct {
		score float64
		band  string
	}{{3.9, "low"}, {4, "medium"}, {6.9, "medium"}, {7, "high"}, {8.9, "high"}, {9, "critical"}} {
		require.Equal(t, tc.band, findingSeverity(tc.score))
	}
}

func TestRiskFindingsValidationAndGates(t *testing.T) {
	for _, input := range []ListRiskFindingsInput{{From: "invalid"}, {From: riskAnalysisTestNow.Format(time.RFC3339)}, {To: riskAnalysisTestNow.Add(time.Hour).Format(time.RFC3339)}, {From: riskAnalysisTestNow.Add(-32 * 24 * time.Hour).Format(time.RFC3339)}, {Severity: "urgent"}, {GroupBy: []string{"email"}}, {GroupBy: []string{"team", "team"}}, {Cursor: "cursor"}, {ProjectID: uuid.NewString(), ProjectSlug: "default"}} {
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
	s.projects.(*findingProjects).err = ErrRiskReadNotFound
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

func TestRiskFindingsCursorAndGroupBounds(t *testing.T) {
	principal := testRiskPrincipal("user")
	s, r, _ := findingsFixture(t)
	for len(r.rows) < 51 {
		row := r.rows[0]
		row.ID = uuid.New()
		r.rows = append(r.rows, row)
	}
	for len(r.buckets) < 201 {
		r.buckets = append(r.buckets, chrepo.WatchdogGroup{Value: "bucket", Count: 1})
	}
	out, err := s.List(t.Context(), principal, ListRiskFindingsInput{GroupBy: []string{"team"}})
	require.NoError(t, err)
	require.Len(t, out.Findings, 50)
	require.NotEmpty(t, out.NextCursor)
	require.Len(t, out.Groups[0].Groups, 200)
	require.True(t, out.Groups[0].Truncated)
	input := ListRiskFindingsInput{From: out.From, To: out.To, GroupBy: []string{"team"}, Cursor: out.NextCursor}
	r.rows = nil
	r.params = nil
	_, err = s.List(t.Context(), principal, input)
	require.NoError(t, err)
	require.Equal(t, out.Findings[49].ID, r.params[0].CursorID.UUID.String())
	for _, principal := range []Principal{testRiskPrincipal("user"), testRiskPrincipal("other-user"), {OrganizationID: "other-org", UserID: "user", ConnectionID: "connection", Generation: "generation"}} {
		_, err = s.List(t.Context(), principal, input)
		require.ErrorIs(t, err, ErrRiskCursorInvalid)
	}
	input.Severity = "all"
	_, err = s.List(t.Context(), principal, input)
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	input.Severity = "critical"
	input.From = riskAnalysisTestNow.Add(-23 * time.Hour).Format(time.RFC3339)
	_, err = s.List(t.Context(), principal, input)
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	input.From = out.From
	s.projects.(*findingProjects).project.ID = uuid.New()
	_, err = s.List(t.Context(), principal, input)
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
}

func TestRiskFindingsMCPInProcess(t *testing.T) {
	s, reader, _ := findingsFixture(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "findings-test", Version: "1"}, nil)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerRiskFindingsTool(reg, s)
	descriptor := descriptorByName(t, reg, "list_risk_findings")
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
	require.Equal(t, ProjectScopeDefaultable, descriptor.Meta.ProjectScope)
	require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
	require.True(t, descriptor.Annotations.ReadOnlyHint)
	require.True(t, validRiskTelemetryTool(descriptor.Name))
	require.True(t, validOptionalFeedbackTool(descriptor.Name))
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
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_risk_findings", Arguments: map[string]any{"group_by": []string{"severity", "team"}}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var out ListRiskFindingsOutput
	require.NoError(t, json.Unmarshal(encoded, &out))
	require.EqualValues(t, 12, out.TotalCount)
	require.Len(t, out.Findings, 1)
	require.Len(t, out.Groups, 2)
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_risk_findings", Arguments: map[string]any{"organization_id": "other-org"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	calls := len(reader.params)
	reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_risk_findings", Arguments: map[string]any{}})
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
	server := mcp.NewServer(&mcp.Implementation{Name: "findings-stub", Version: "1"}, nil)
	reg := newRegistrar(server)
	registerRiskFindingsTool(reg, nil)
	d := descriptorByName(t, reg, "list_risk_findings")
	require.Contains(t, d.Description, "unavailable in this deployment")
	_, err := d.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
}

package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/analysisstatus"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubAnalysisDescriber struct {
	status    analysisstatus.Status
	err       error
	projectID uuid.UUID
	calls     int
}

func (s *stubAnalysisDescriber) Describe(_ context.Context, projectID uuid.UUID) (analysisstatus.Status, error) {
	s.calls++
	s.projectID = projectID
	if s.err != nil {
		return analysisstatus.Status{}, s.err
	}
	return s.status, nil
}

var riskAnalysisTestNow = time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)

func testRiskAnalysisStatusService(t *testing.T, project ResolvedProject, describer *stubAnalysisDescriber, evaluation feature.Evaluation) (*RiskAnalysisStatusService, *riskMutationFlagProvider) {
	t.Helper()
	flags := &riskMutationFlagProvider{evaluation: evaluation, err: nil, flag: "", groups: nil}
	return &RiskAnalysisStatusService{
		logger:        testenv.NewLogger(t),
		describer:     describer,
		flags:         flags,
		organizations: riskMutationOrganizationResolver{slug: "org", err: nil},
		projects:      &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectID: "", projectSlug: "project"}}, calls: nil, err: nil},
		now:           func() time.Time { return riskAnalysisTestNow },
	}, flags
}

func TestRiskAnalysisStatusNeverRun(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{State: analysisstatus.StateNever, RunningSince: nil, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, err: nil, projectID: uuid.Nil, calls: 0}
	service, flags := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)

	output, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.NoError(t, err)
	require.Equal(t, project.ID, describer.projectID)
	require.Equal(t, feature.FlagRiskWatchdog, flags.flag)
	require.Equal(t, map[string]string{"organization": "org", "slug": "org/project"}, flags.groups)
	require.Equal(t, GetRiskAnalysisStatusOutput{
		Project:          RiskProject{ID: project.ID.String(), Name: "Project", Slug: "project"},
		State:            "never",
		RunningSince:     "",
		LastRunStartedAt: "",
		LastRunAt:        "",
		LastRunOutcome:   "",
		Explanation:      "No analysis has run for this project recently. It starts automatically when chat traffic is captured; check that logging is enabled.",
	}, output)
}

func TestRiskAnalysisStatusIdleCompleted(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	startedAt := riskAnalysisTestNow.Add(-6 * time.Minute)
	finishedAt := riskAnalysisTestNow.Add(-5 * time.Minute)
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{State: analysisstatus.StateIdle, RunningSince: nil, LastRunStartedAt: &startedAt, LastRunAt: &finishedAt, LastRunOutcome: "completed"}, err: nil, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)

	output, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.NoError(t, err)
	require.Equal(t, "idle", output.State)
	require.Empty(t, output.RunningSince)
	require.Equal(t, "2026-09-15T11:54:00Z", output.LastRunStartedAt)
	require.Equal(t, "2026-09-15T11:55:00Z", output.LastRunAt)
	require.Equal(t, "completed", output.LastRunOutcome)
	require.Equal(t, "Analysis last finished 5 minutes ago. It runs within about 30 seconds of new chat traffic, not on a timer.", output.Explanation)
}

func TestRiskAnalysisStatusIdleWithFailedOutcome(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	startedAt := riskAnalysisTestNow.Add(-3 * time.Hour)
	finishedAt := riskAnalysisTestNow.Add(-2 * time.Hour)
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{State: analysisstatus.StateIdle, RunningSince: nil, LastRunStartedAt: &startedAt, LastRunAt: &finishedAt, LastRunOutcome: "timed_out"}, err: nil, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)

	output, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.NoError(t, err)
	require.Equal(t, "idle", output.State)
	require.Equal(t, "timed_out", output.LastRunOutcome)
	require.Equal(t, "The last analysis ended with outcome timed out 2 hours ago. Messages it did not analyze are retried automatically on the next run, which starts within about 30 seconds of new chat traffic.", output.Explanation)
}

func TestRiskAnalysisStatusRunning(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	runningSince := riskAnalysisTestNow.Add(-12 * time.Second)
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{State: analysisstatus.StateRunning, RunningSince: &runningSince, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, err: nil, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)

	output, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.NoError(t, err)
	require.Equal(t, "running", output.State)
	require.Equal(t, "2026-09-15T11:59:48Z", output.RunningSince)
	require.Empty(t, output.LastRunAt)
	require.Empty(t, output.LastRunOutcome)
	require.Equal(t, "Analysis is in progress now. It started 12 seconds ago.", output.Explanation)
}

func TestRiskAnalysisStatusDescriberErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	describeErr := errors.New("describe failed")
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{}, err: describeErr, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)

	_, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, err, describeErr)
}

func TestRiskAnalysisStatusFlagOffFailsClosedBeforeDescribing(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{}, err: nil, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationDisabled)

	_, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.ErrorIs(t, err, ErrRiskFeatureNotEnabled)
	require.Zero(t, describer.calls)

	// An indeterminate evaluation is not an enablement either.
	service, _ = testRiskAnalysisStatusService(t, project, describer, feature.EvaluationIndeterminate)
	_, err = service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "project"})
	require.ErrorIs(t, err, ErrRiskFeatureNotEnabled)
	require.Zero(t, describer.calls)
}

func TestRiskAnalysisStatusProjectNotFound(t *testing.T) {
	t.Parallel()

	describer := &stubAnalysisDescriber{status: analysisstatus.Status{}, err: nil, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, ResolvedProject{}, describer, feature.EvaluationEnabled)
	service.projects = &stubRiskProjects{project: ResolvedProject{}, expected: nil, calls: nil, err: ErrRiskReadNotFound}

	_, err := service.Get(t.Context(), testRiskPrincipal("user"), GetRiskAnalysisStatusInput{ProjectID: "", ProjectSlug: "missing"})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	require.Zero(t, describer.calls)
}

func TestRiskAnalysisStatusToolServesLiveAndRefusals(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	finishedAt := riskAnalysisTestNow.Add(-90 * time.Second)
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{State: analysisstatus.StateIdle, RunningSince: nil, LastRunStartedAt: &finishedAt, LastRunAt: &finishedAt, LastRunOutcome: "completed"}, err: nil, projectID: uuid.Nil, calls: 0}
	service, flags := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)
	// The live call and the flag-off call each resolve the project; the
	// schema violation between them is rejected before resolution.
	resolution := riskProjectCall{organizationID: "<ORG_ID>", projectID: "", projectSlug: "project"}
	service.projects = &stubRiskProjects{project: project, expected: []riskProjectCall{resolution, resolution}, calls: nil, err: nil}

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-analysis-status-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	registerRiskAnalysisStatusTool(reg, service)
	descriptor := descriptorByName(t, reg, "get_risk_analysis_status")
	require.True(t, descriptor.Annotations.ReadOnlyHint)
	require.Equal(t, ProjectScopeDefaultable, descriptor.Meta.ProjectScope)
	require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
	require.NotContains(t, descriptor.Description, "Temporal")
	require.NotContains(t, descriptor.Description, "workflow")

	raw, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project"}`))
	require.NoError(t, err)
	output, ok := raw.(GetRiskAnalysisStatusOutput)
	require.True(t, ok)
	require.Equal(t, "idle", output.State)
	require.Equal(t, "Analysis last finished 1 minute ago. It runs within about 30 seconds of new chat traffic, not on a timer.", output.Explanation)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "running_since")
	require.Contains(t, string(encoded), `"last_run_outcome":"completed"`)

	// Both selectors at once violate the shared project-selector schema.
	_, err = descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","project_id":"`+project.ID.String()+`"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")

	flags.evaluation = feature.EvaluationDisabled
	_, err = descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
	require.Contains(t, refusal.Payload, "not switched on")
}

func TestRiskAnalysisStatusToolStubWithoutDescriber(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-analysis-status-stub-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	registerRiskAnalysisStatusTool(reg, nil)
	descriptor := descriptorByName(t, reg, "get_risk_analysis_status")
	require.Contains(t, descriptor.Description, "unavailable in this deployment")
	require.NotEmpty(t, descriptor.InputSchema)

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)

	require.Nil(t, NewRiskAnalysisStatusService(testenv.NewLogger(t), nil, &stubAnalysisDescriber{status: analysisstatus.Status{}, err: nil, projectID: uuid.Nil, calls: 0}, nil, riskMutationOrganizationResolver{slug: "org", err: nil}))
}

func TestHumanDurationUnits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		in   time.Duration
		want string
	}{
		{in: -time.Second, want: "a few seconds"},
		{in: 400 * time.Millisecond, want: "a few seconds"},
		{in: time.Second, want: "1 second"},
		{in: 45 * time.Second, want: "45 seconds"},
		{in: time.Minute, want: "1 minute"},
		{in: 59*time.Minute + 59*time.Second, want: "59 minutes"},
		{in: time.Hour, want: "1 hour"},
		{in: 23 * time.Hour, want: "23 hours"},
		{in: 24 * time.Hour, want: "1 day"},
		{in: 72 * time.Hour, want: "3 days"},
	} {
		require.Equal(t, test.want, humanDuration(test.in), test.in.String())
	}
}

// The status read does not depend on the policy reader: when policy reads are
// unavailable the status tool is still served live, and the policy stubs are
// registered exactly once alongside it.
func TestRiskAnalysisStatusToolLiveWhenPolicyReadsUnavailable(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	startedAt := riskAnalysisTestNow.Add(-12 * time.Second)
	describer := &stubAnalysisDescriber{status: analysisstatus.Status{State: analysisstatus.StateRunning, RunningSince: &startedAt, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, err: nil, projectID: uuid.Nil, calls: 0}
	service, _ := testRiskAnalysisStatusService(t, project, describer, feature.EvaluationEnabled)

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-analysis-status-independent-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	require.NotPanics(t, func() { registerRiskToolsWithMutations(reg, nil, service, nil) })

	descriptor := descriptorByName(t, reg, "get_risk_analysis_status")
	require.NotContains(t, descriptor.Description, "unavailable in this deployment")
	raw, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project"}`))
	require.NoError(t, err)
	output, ok := raw.(GetRiskAnalysisStatusOutput)
	require.True(t, ok)
	require.Equal(t, "running", output.State)

	policies := descriptorByName(t, reg, "list_risk_policies")
	require.Contains(t, policies.Description, "unavailable in this deployment")
	names := map[string]int{}
	for _, d := range reg.Descriptors() {
		names[d.Name]++
	}
	for _, name := range []string{"get_risk_analysis_status", "list_risk_policies", "get_risk_policy", "list_risk_exclusions"} {
		require.Equal(t, 1, names[name], name)
	}
}

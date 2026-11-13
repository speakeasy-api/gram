package background

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporallog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

type publishOutcomeCounts struct {
	mu     sync.Mutex
	values map[string]int64
}

type publishOutcomeMetrics struct {
	client.MetricsHandler
	counts  *publishOutcomeCounts
	outcome string
}

func newPublishOutcomeMetrics() *publishOutcomeMetrics {
	return &publishOutcomeMetrics{MetricsHandler: client.MetricsNopHandler, counts: &publishOutcomeCounts{values: map[string]int64{}}}
}

func (m *publishOutcomeMetrics) WithTags(tags map[string]string) client.MetricsHandler {
	tagged := *m
	if outcome, ok := tags["outcome"]; ok {
		tagged.outcome = outcome
	}
	return &tagged
}

func (m *publishOutcomeMetrics) Counter(name string) client.MetricsCounter {
	if name != "plugin_project_publish_outcomes" {
		return client.MetricsNopHandler.Counter(name)
	}
	return m
}

func (m *publishOutcomeMetrics) Inc(n int64) {
	m.counts.mu.Lock()
	defer m.counts.mu.Unlock()
	m.counts.values[m.outcome] += n
}

func TestPluginPublishWorkflow_OutcomeCountsLogicalPublishNotAttempts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		failures      int
		err           error
		skipped       bool
		outcome       string
		attempts      int
		workflowFails bool
	}{
		{name: "retry exhaustion", failures: 3, err: errors.New("transient failure"), outcome: "failed", attempts: 3, workflowFails: true},
		{name: "retry recovery", failures: 2, err: errors.New("transient failure"), outcome: "published", attempts: 3},
		{name: "unchanged", skipped: true, outcome: "skipped", attempts: 1},
		{name: "permanent rejection", failures: 1, err: temporal.NewNonRetryableApplicationError("no member", activities.ErrTypePluginActorNotMember, nil), outcome: "rejected", attempts: 1},
		{name: "permanent conflict", failures: 1, err: temporal.NewNonRetryableApplicationError("repo conflict", activities.ErrTypeGitHubRepoConflict, nil), outcome: "failed", attempts: 1, workflowFails: true},
		{name: "canceled", failures: 1, err: temporal.NewCanceledError(), outcome: "canceled", attempts: 1, workflowFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			var output bytes.Buffer
			suite.SetLogger(temporallog.NewStructuredLogger(slog.New(slog.NewJSONHandler(&output, nil))))
			metrics := newPublishOutcomeMetrics()
			suite.SetMetricsHandler(metrics)
			env := suite.NewTestWorkflowEnvironment()
			attempts := 0
			env.RegisterActivityWithOptions(func(context.Context, plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
				attempts++
				if attempts <= tc.failures {
					return nil, tc.err
				}
				return &plugins.PublishProjectResult{Skipped: tc.skipped}, nil
			}, activity.RegisterOptions{Name: "PublishPluginProject"})
			env.ExecuteWorkflow(PluginPublishWorkflow, PluginPublishParams{ProjectID: uuid.New()})
			require.True(t, env.IsWorkflowCompleted())
			require.Equal(t, tc.workflowFails, env.GetWorkflowError() != nil)
			require.Equal(t, tc.attempts, attempts)
			require.Equal(t, map[string]int64{tc.outcome: 1}, metrics.counts.values)
			if tc.outcome == "skipped" {
				require.NotContains(t, output.String(), "plugin project publish outcome")
			} else {
				require.Contains(t, output.String(), "plugin project publish outcome")
			}
		})
	}
}

func TestPluginGeneratorRolloutWorkflow_OutcomeCountsAfterRetries(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	metrics := newPublishOutcomeMetrics()
	suite.SetMetricsHandler(metrics)
	env := suite.NewTestWorkflowEnvironment()
	registerPluginRolloutRepairNoop(env)
	failedID, recoveredID := uuid.New(), uuid.New()
	env.RegisterActivityWithOptions(func(context.Context, activities.ListPluginPublishCandidatesInput) (*activities.ListPluginPublishCandidatesResult, error) {
		return &activities.ListPluginPublishCandidatesResult{Candidates: []activities.PluginPublishCandidate{
			{ProjectID: failedID, CreatedByUserID: "user_1"},
			{ProjectID: recoveredID, CreatedByUserID: "user_2"},
		}}, nil
	}, activity.RegisterOptions{Name: "ListPluginPublishCandidates"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
		if input.ProjectID == failedID || activity.GetInfo(ctx).Attempt < 3 {
			return nil, errors.New("transient failure")
		}
		return &plugins.PublishProjectResult{}, nil
	}, activity.RegisterOptions{Name: "PublishPluginProject"})
	env.ExecuteWorkflow(PluginGeneratorRolloutWorkflow, PluginGeneratorRolloutInput{BatchSize: 10})
	require.NoError(t, env.GetWorkflowError(), "the sweep can succeed despite a project publish failure")
	var result PluginGeneratorRolloutResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 1, result.Failed)
	require.Equal(t, 1, result.Published)
	require.Equal(t, map[string]int64{"failed": 1, "published": 1}, metrics.counts.values)
}

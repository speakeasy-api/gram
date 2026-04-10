package background_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish"
)

func TestAutoPublishWorkflow_PublishesEligibleDrafts(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"
	draftID1 := uuid.New()
	draftID2 := uuid.New()

	cfg := autopublish.Config{
		Enabled:          true,
		IntervalMinutes:  10,
		MinUpvotes:       0,
		AuthorTypeFilter: nil,
		LabelFilter:      nil,
		MinAgeHours:      0,
	}

	var a *background.AutoPublishActivities
	env.OnActivity(a.GetAutoPublishConfig, mock.Anything, projectID).Return(cfg, nil)
	env.OnActivity(a.QueryEligibleDrafts, mock.Anything, projectID, cfg).Return([]uuid.UUID{draftID1, draftID2}, nil)
	env.OnActivity(a.BatchPublishDrafts, mock.Anything, projectID, orgID, []uuid.UUID{draftID1, draftID2}).Return("abc123", nil)

	env.ExecuteWorkflow(background.AutoPublishWorkflow, background.AutoPublishWorkflowParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result background.AutoPublishWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.Published)
	require.Equal(t, "abc123", result.CommitSHA)

	env.AssertExpectations(t)
}

func TestAutoPublishWorkflow_NothingEligible(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"

	cfg := autopublish.Config{
		Enabled:          true,
		IntervalMinutes:  10,
		MinUpvotes:       5,
		AuthorTypeFilter: nil,
		LabelFilter:      nil,
		MinAgeHours:      0,
	}

	var a *background.AutoPublishActivities
	env.OnActivity(a.GetAutoPublishConfig, mock.Anything, projectID).Return(cfg, nil)
	env.OnActivity(a.QueryEligibleDrafts, mock.Anything, projectID, cfg).Return([]uuid.UUID{}, nil)

	env.ExecuteWorkflow(background.AutoPublishWorkflow, background.AutoPublishWorkflowParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result background.AutoPublishWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 0, result.Published)
	require.Empty(t, result.CommitSHA)

	env.AssertExpectations(t)
}

func TestAutoPublishWorkflow_Disabled(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"

	cfg := autopublish.Config{
		Enabled:          false,
		IntervalMinutes:  10,
		MinUpvotes:       0,
		AuthorTypeFilter: nil,
		LabelFilter:      nil,
		MinAgeHours:      0,
	}

	var a *background.AutoPublishActivities
	env.OnActivity(a.GetAutoPublishConfig, mock.Anything, projectID).Return(cfg, nil)

	env.ExecuteWorkflow(background.AutoPublishWorkflow, background.AutoPublishWorkflowParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result background.AutoPublishWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 0, result.Published)
	require.Empty(t, result.CommitSHA)

	env.AssertExpectations(t)
}

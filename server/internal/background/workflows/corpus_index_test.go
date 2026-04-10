package workflows

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// activityStub is used to register activity method references in the test env.
var activityStub = &CorpusIndexActivities{}

func TestCorpusIndexWorkflow_NewCommit(t *testing.T) {
	t.Parallel()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"
	commitSHA := "abc123"
	eventID := uuid.New()

	env.OnActivity(activityStub.GetIndexState, mock.Anything, GetIndexStateParams{
		ProjectID: projectID,
	}).Return((*IndexState)(nil), nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexing",
	}).Return(nil)

	env.OnActivity(activityStub.DiffCommit, mock.Anything, DiffCommitParams{
		ProjectID: projectID,
		OldSHA:    "",
		NewSHA:    commitSHA,
	}).Return(&DiffResult{
		Added:    []string{"docs/intro.md", "docs/setup.md"},
		Modified: nil,
		Deleted:  nil,
	}, nil)

	chunks := []ChunkResult{
		{
			ChunkID:            "docs/intro.md#overview",
			FilePath:           "docs/intro.md",
			HeadingPath:        "overview",
			Breadcrumb:         "Overview",
			Content:            "# Overview\nHello",
			ContentText:        "Overview Hello",
			Strategy:           "h2",
			MetadataJSON:       "{}",
			ContentFingerprint: "fp1",
		},
		{
			ChunkID:            "docs/setup.md#install",
			FilePath:           "docs/setup.md",
			HeadingPath:        "install",
			Breadcrumb:         "Install",
			Content:            "# Install\nRun it",
			ContentText:        "Install Run it",
			Strategy:           "h2",
			MetadataJSON:       "{}",
			ContentFingerprint: "fp2",
		},
	}

	env.OnActivity(activityStub.ChunkFiles, mock.Anything, ChunkFilesParams{
		ProjectID: projectID,
		CommitSHA: commitSHA,
		FilePaths: []string{"docs/intro.md", "docs/setup.md"},
	}).Return(chunks, nil)

	embeddings := []EmbedResult{
		{ChunkID: "docs/intro.md#overview", Embedding: make([]float32, 3072), Skipped: false},
		{ChunkID: "docs/setup.md#install", Embedding: make([]float32, 3072), Skipped: false},
	}

	env.OnActivity(activityStub.EmbedChunks, mock.Anything, EmbedChunksParams{
		ProjectID: projectID,
		Chunks:    chunks,
	}).Return(embeddings, nil)

	env.OnActivity(activityStub.UpsertChunks, mock.Anything, UpsertChunksParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		Chunks:         chunks,
		Embeddings:     embeddings,
	}).Return(nil)

	env.OnActivity(activityStub.UpdateIndexState, mock.Anything, UpdateIndexStateParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EmbeddingModel: "text-embedding-3-large",
	}).Return(nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexed",
	}).Return(nil)

	env.ExecuteWorkflow(CorpusIndexWorkflow, CorpusIndexParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EventID:        eventID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestCorpusIndexWorkflow_DeletedFile(t *testing.T) {
	t.Parallel()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"
	commitSHA := "def456"
	eventID := uuid.New()
	prevSHA := "abc123"

	env.OnActivity(activityStub.GetIndexState, mock.Anything, GetIndexStateParams{
		ProjectID: projectID,
	}).Return(&IndexState{
		LastIndexedSHA: prevSHA,
		EmbeddingModel: "text-embedding-3-large",
	}, nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexing",
	}).Return(nil)

	env.OnActivity(activityStub.DiffCommit, mock.Anything, DiffCommitParams{
		ProjectID: projectID,
		OldSHA:    prevSHA,
		NewSHA:    commitSHA,
	}).Return(&DiffResult{
		Added:    nil,
		Modified: nil,
		Deleted:  []string{"docs/removed.md"},
	}, nil)

	env.OnActivity(activityStub.DeleteChunks, mock.Anything, DeleteChunksParams{
		ProjectID: projectID,
		FilePaths: []string{"docs/removed.md"},
	}).Return(nil)

	env.OnActivity(activityStub.UpdateIndexState, mock.Anything, UpdateIndexStateParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EmbeddingModel: "text-embedding-3-large",
	}).Return(nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexed",
	}).Return(nil)

	env.ExecuteWorkflow(CorpusIndexWorkflow, CorpusIndexParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EventID:        eventID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestCorpusIndexWorkflow_ConfigChange(t *testing.T) {
	t.Parallel()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"
	commitSHA := "ghi789"
	eventID := uuid.New()
	prevSHA := "def456"

	env.OnActivity(activityStub.GetIndexState, mock.Anything, GetIndexStateParams{
		ProjectID: projectID,
	}).Return(&IndexState{
		LastIndexedSHA: prevSHA,
		EmbeddingModel: "text-embedding-3-large",
	}, nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexing",
	}).Return(nil)

	env.OnActivity(activityStub.DiffCommit, mock.Anything, DiffCommitParams{
		ProjectID: projectID,
		OldSHA:    prevSHA,
		NewSHA:    commitSHA,
	}).Return(&DiffResult{
		Added:    nil,
		Modified: []string{".docs-mcp.json"},
		Deleted:  nil,
	}, nil)

	chunks := []ChunkResult{
		{
			ChunkID:            "docs/intro.md#overview",
			FilePath:           "docs/intro.md",
			HeadingPath:        "overview",
			Breadcrumb:         "Overview",
			Content:            "# Overview\nHello",
			ContentText:        "Overview Hello",
			Strategy:           "h3",
			MetadataJSON:       "{}",
			ContentFingerprint: "fp-new",
		},
	}

	// Config change: ChunkFiles called with nil FilePaths to re-chunk all
	env.OnActivity(activityStub.ChunkFiles, mock.Anything, ChunkFilesParams{
		ProjectID: projectID,
		CommitSHA: commitSHA,
		FilePaths: nil,
	}).Return(chunks, nil)

	embeddings := []EmbedResult{
		{ChunkID: "docs/intro.md#overview", Embedding: make([]float32, 3072), Skipped: false},
	}

	env.OnActivity(activityStub.EmbedChunks, mock.Anything, EmbedChunksParams{
		ProjectID: projectID,
		Chunks:    chunks,
	}).Return(embeddings, nil)

	env.OnActivity(activityStub.UpsertChunks, mock.Anything, UpsertChunksParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		Chunks:         chunks,
		Embeddings:     embeddings,
	}).Return(nil)

	env.OnActivity(activityStub.UpdateIndexState, mock.Anything, UpdateIndexStateParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EmbeddingModel: "text-embedding-3-large",
	}).Return(nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexed",
	}).Return(nil)

	env.ExecuteWorkflow(CorpusIndexWorkflow, CorpusIndexParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EventID:        eventID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestCorpusIndexWorkflow_Idempotent(t *testing.T) {
	t.Parallel()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	orgID := "org-1"
	commitSHA := "abc123"
	eventID := uuid.New()

	// Index state already has this commit SHA -- workflow is a no-op
	env.OnActivity(activityStub.GetIndexState, mock.Anything, GetIndexStateParams{
		ProjectID: projectID,
	}).Return(&IndexState{
		LastIndexedSHA: commitSHA,
		EmbeddingModel: "text-embedding-3-large",
	}, nil)

	env.OnActivity(activityStub.MarkEventStatus, mock.Anything, MarkEventStatusParams{
		EventID:   eventID,
		ProjectID: projectID,
		Status:    "indexed",
	}).Return(nil)

	env.ExecuteWorkflow(CorpusIndexWorkflow, CorpusIndexParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSHA:      commitSHA,
		EventID:        eventID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

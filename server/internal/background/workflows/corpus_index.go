package workflows

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const configFileName = ".docs-mcp.json"

// CorpusIndexParams contains the input for the corpus index workflow.
type CorpusIndexParams struct {
	ProjectID      uuid.UUID
	OrganizationID string
	CommitSHA      string
	EventID        uuid.UUID
}

// DiffResult describes files changed between two commits.
type DiffResult struct {
	Added    []string
	Modified []string
	Deleted  []string
}

// ChunkResult describes the chunks produced from processing changed files.
type ChunkResult struct {
	ChunkID             string
	FilePath            string
	HeadingPath         string
	Breadcrumb          string
	Content             string
	ContentText         string
	Strategy            string
	MetadataJSON        string
	ManifestFingerprint string
	ContentFingerprint  string
}

// EmbedResult holds the embedding for a chunk.
type EmbedResult struct {
	ChunkID   string
	Embedding []float32
	Skipped   bool
}

// DiffCommitParams are input for the DiffCommit activity.
type DiffCommitParams struct {
	ProjectID uuid.UUID
	OldSHA    string
	NewSHA    string
}

// ChunkFilesParams are input for the ChunkFiles activity.
// If FilePaths is nil, all files in the commit are chunked.
type ChunkFilesParams struct {
	ProjectID uuid.UUID
	CommitSHA string
	FilePaths []string
}

// EmbedChunksParams are input for the EmbedChunks activity.
type EmbedChunksParams struct {
	ProjectID uuid.UUID
	Chunks    []ChunkResult
}

// UpsertChunksParams are input for the UpsertChunks activity.
type UpsertChunksParams struct {
	ProjectID      uuid.UUID
	OrganizationID string
	Chunks         []ChunkResult
	Embeddings     []EmbedResult
}

// DeleteChunksParams are input for the DeleteChunks activity.
type DeleteChunksParams struct {
	ProjectID uuid.UUID
	FilePaths []string
}

// UpdateIndexStateParams are input for the UpdateIndexState activity.
type UpdateIndexStateParams struct {
	ProjectID      uuid.UUID
	OrganizationID string
	CommitSHA      string
	EmbeddingModel string
}

// GetIndexStateParams are input for the GetIndexState activity.
type GetIndexStateParams struct {
	ProjectID uuid.UUID
}

// IndexState represents the current indexing state for a project.
type IndexState struct {
	LastIndexedSHA string
	EmbeddingModel string
}

// MarkEventStatusParams are input for the MarkEventStatus activity.
type MarkEventStatusParams struct {
	EventID   uuid.UUID
	ProjectID uuid.UUID
	Status    string
}

// CorpusIndexActivities holds the activity implementations for corpus indexing.
type CorpusIndexActivities struct{}

func (a *CorpusIndexActivities) DiffCommit(ctx context.Context, params DiffCommitParams) (*DiffResult, error) {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) ChunkFiles(ctx context.Context, params ChunkFilesParams) ([]ChunkResult, error) {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) EmbedChunks(ctx context.Context, params EmbedChunksParams) ([]EmbedResult, error) {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) UpsertChunks(ctx context.Context, params UpsertChunksParams) error {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) DeleteChunks(ctx context.Context, params DeleteChunksParams) error {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) UpdateIndexState(ctx context.Context, params UpdateIndexStateParams) error {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) GetIndexState(ctx context.Context, params GetIndexStateParams) (*IndexState, error) {
	panic("activity not wired")
}

func (a *CorpusIndexActivities) MarkEventStatus(ctx context.Context, params MarkEventStatusParams) error {
	panic("activity not wired")
}

// CorpusIndexWorkflow is the Temporal workflow that indexes corpus content
// after a publish event. It diffs the commit, chunks changed files, embeds
// new chunks, and upserts them into the database.
func CorpusIndexWorkflow(ctx workflow.Context, params CorpusIndexParams) error {
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	var activities *CorpusIndexActivities

	// 1. Get current index state.
	var indexState *IndexState
	err := workflow.ExecuteActivity(ctx, activities.GetIndexState, GetIndexStateParams{
		ProjectID: params.ProjectID,
	}).Get(ctx, &indexState)
	if err != nil {
		return err
	}

	// 2. Idempotency check: if already indexed this commit, mark done and return.
	if indexState != nil && indexState.LastIndexedSHA == params.CommitSHA {
		return workflow.ExecuteActivity(ctx, activities.MarkEventStatus, MarkEventStatusParams{
			EventID:   params.EventID,
			ProjectID: params.ProjectID,
			Status:    "indexed",
		}).Get(ctx, nil)
	}

	// 3. Mark event as indexing.
	err = workflow.ExecuteActivity(ctx, activities.MarkEventStatus, MarkEventStatusParams{
		EventID:   params.EventID,
		ProjectID: params.ProjectID,
		Status:    "indexing",
	}).Get(ctx, nil)
	if err != nil {
		return err
	}

	// 4. Diff the commit against previous indexed state.
	oldSHA := ""
	if indexState != nil {
		oldSHA = indexState.LastIndexedSHA
	}

	var diff DiffResult
	err = workflow.ExecuteActivity(ctx, activities.DiffCommit, DiffCommitParams{
		ProjectID: params.ProjectID,
		OldSHA:    oldSHA,
		NewSHA:    params.CommitSHA,
	}).Get(ctx, &diff)
	if err != nil {
		return err
	}

	// 5. Check if config changed — if so, re-chunk everything.
	configChanged := hasConfigChange(diff)

	// 6. Handle deleted files.
	if len(diff.Deleted) > 0 {
		err = workflow.ExecuteActivity(ctx, activities.DeleteChunks, DeleteChunksParams{
			ProjectID: params.ProjectID,
			FilePaths: diff.Deleted,
		}).Get(ctx, nil)
		if err != nil {
			return err
		}
	}

	// 7. Determine which files to chunk.
	var filesToChunk []string
	if configChanged {
		// Re-chunk all files when config changes. Passing nil signals "all files".
		filesToChunk = nil
	} else {
		filesToChunk = make([]string, 0, len(diff.Added)+len(diff.Modified))
		filesToChunk = append(filesToChunk, diff.Added...)
		filesToChunk = append(filesToChunk, diff.Modified...)
		// Exclude config files from chunking.
		filesToChunk = filterConfigFiles(filesToChunk)
	}

	// If there are files to chunk (or config changed), run the chunk+embed pipeline.
	if configChanged || len(filesToChunk) > 0 {
		var chunks []ChunkResult
		err = workflow.ExecuteActivity(ctx, activities.ChunkFiles, ChunkFilesParams{
			ProjectID: params.ProjectID,
			CommitSHA: params.CommitSHA,
			FilePaths: filesToChunk,
		}).Get(ctx, &chunks)
		if err != nil {
			return err
		}

		if len(chunks) > 0 {
			var embeddings []EmbedResult
			err = workflow.ExecuteActivity(ctx, activities.EmbedChunks, EmbedChunksParams{
				ProjectID: params.ProjectID,
				Chunks:    chunks,
			}).Get(ctx, &embeddings)
			if err != nil {
				return err
			}

			err = workflow.ExecuteActivity(ctx, activities.UpsertChunks, UpsertChunksParams{
				ProjectID:      params.ProjectID,
				OrganizationID: params.OrganizationID,
				Chunks:         chunks,
				Embeddings:     embeddings,
			}).Get(ctx, nil)
			if err != nil {
				return err
			}
		}
	}

	// 8. Update index state.
	err = workflow.ExecuteActivity(ctx, activities.UpdateIndexState, UpdateIndexStateParams{
		ProjectID:      params.ProjectID,
		OrganizationID: params.OrganizationID,
		CommitSHA:      params.CommitSHA,
		EmbeddingModel: "text-embedding-3-large",
	}).Get(ctx, nil)
	if err != nil {
		return err
	}

	// 9. Mark event as indexed.
	return workflow.ExecuteActivity(ctx, activities.MarkEventStatus, MarkEventStatusParams{
		EventID:   params.EventID,
		ProjectID: params.ProjectID,
		Status:    "indexed",
	}).Get(ctx, nil)
}

// hasConfigChange checks if any of the changed files is a .docs-mcp.json config.
func hasConfigChange(diff DiffResult) bool {
	isConfig := func(f string) bool { return filepath.Base(f) == configFileName }
	return slices.ContainsFunc(diff.Added, isConfig) ||
		slices.ContainsFunc(diff.Modified, isConfig) ||
		slices.ContainsFunc(diff.Deleted, isConfig)
}

// filterConfigFiles removes .docs-mcp.json files from the list.
func filterConfigFiles(paths []string) []string {
	var filtered []string
	for _, p := range paths {
		if filepath.Base(p) != configFileName {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

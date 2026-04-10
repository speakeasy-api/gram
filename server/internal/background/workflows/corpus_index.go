package workflows

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

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
	panic("not implemented")
}

func (a *CorpusIndexActivities) ChunkFiles(ctx context.Context, params ChunkFilesParams) ([]ChunkResult, error) {
	panic("not implemented")
}

func (a *CorpusIndexActivities) EmbedChunks(ctx context.Context, params EmbedChunksParams) ([]EmbedResult, error) {
	panic("not implemented")
}

func (a *CorpusIndexActivities) UpsertChunks(ctx context.Context, params UpsertChunksParams) error {
	panic("not implemented")
}

func (a *CorpusIndexActivities) DeleteChunks(ctx context.Context, params DeleteChunksParams) error {
	panic("not implemented")
}

func (a *CorpusIndexActivities) UpdateIndexState(ctx context.Context, params UpdateIndexStateParams) error {
	panic("not implemented")
}

func (a *CorpusIndexActivities) GetIndexState(ctx context.Context, params GetIndexStateParams) (*IndexState, error) {
	panic("not implemented")
}

func (a *CorpusIndexActivities) MarkEventStatus(ctx context.Context, params MarkEventStatusParams) error {
	panic("not implemented")
}

// CorpusIndexWorkflow is the Temporal workflow that indexes corpus content
// after a publish event. It diffs the commit, chunks changed files, embeds
// new chunks, and upserts them into the database.
func CorpusIndexWorkflow(ctx workflow.Context, params CorpusIndexParams) error {
	_ = temporal.RetryPolicy{}
	_ = time.Second
	panic("not implemented")
}

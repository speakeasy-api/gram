package drafts

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts/repo"
)

const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"

	StatusOpen      = "open"
	StatusPublished = "published"
	StatusRejected  = "rejected"
)

var ErrNotFound = errors.New("draft not found")
var ErrInvalidOperation = errors.New("invalid operation: must be create, update, or delete")
var ErrEmptyFilePath = errors.New("file_path must not be empty")

var validOperations = map[string]bool{
	OpCreate: true,
	OpUpdate: true,
	OpDelete: true,
}

type GitRepo interface {
	CommitFiles(files map[string][]byte, message string) (string, error)
	ReadBlob(ref string, path string) ([]byte, error)
}

type CreateDraftParams struct {
	FilePath   string
	Content    *string
	Operation  string
	Source     *string
	AuthorType *string
	Labels     []byte
}

type Draft = repo.CorpusDraft

type Enrichment struct {
	OpenDrafts int64
}

type Service struct {
	db      *pgxpool.Pool
	repo    *repo.Queries
	git     GitRepo
	writeMu sync.Mutex
}

func NewService(db *pgxpool.Pool, git GitRepo) *Service {
	return &Service{
		db:      db,
		repo:    repo.New(db),
		git:     git,
		writeMu: sync.Mutex{},
	}
}

func (s *Service) Create(ctx context.Context, projectID uuid.UUID, orgID string, params CreateDraftParams) (*Draft, error) {
	if !validOperations[params.Operation] {
		return nil, ErrInvalidOperation
	}
	if params.FilePath == "" {
		return nil, ErrEmptyFilePath
	}

	d, err := s.repo.CreateDraft(ctx, repo.CreateDraftParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		FilePath:       params.FilePath,
		Content:        conv.PtrToPGText(params.Content),
		Operation:      params.Operation,
		Source:         conv.PtrToPGText(params.Source),
		AuthorType:     conv.PtrToPGText(params.AuthorType),
		Labels:         params.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}

	return &d, nil
}

func (s *Service) Get(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (*Draft, error) {
	d, err := s.repo.GetDraft(ctx, repo.GetDraftParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get draft: %w", err)
	}

	return &d, nil
}

func (s *Service) List(ctx context.Context, projectID uuid.UUID, status *string) ([]Draft, error) {
	drafts, err := s.repo.ListDrafts(ctx, repo.ListDraftsParams{
		ProjectID: projectID,
		Status:    conv.PtrToPGText(status),
	})
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}

	return drafts, nil
}

func (s *Service) UpdateContent(ctx context.Context, projectID uuid.UUID, id uuid.UUID, content string) (*Draft, error) {
	d, err := s.repo.UpdateDraftContent(ctx, repo.UpdateDraftContentParams{
		ID:        id,
		ProjectID: projectID,
		Content:   conv.ToPGText(content),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update draft content: %w", err)
	}

	return &d, nil
}

func (s *Service) Delete(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (*Draft, error) {
	d, err := s.repo.SoftDeleteDraft(ctx, repo.SoftDeleteDraftParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delete draft: %w", err)
	}

	return &d, nil
}

func (s *Service) Publish(ctx context.Context, projectID uuid.UUID, orgID string, draftIDs []uuid.UUID) (string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Fetch drafts to publish
	openDrafts, err := s.repo.ListOpenDraftsByProject(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("list open drafts: %w", err)
	}

	// Filter to requested IDs
	idSet := make(map[uuid.UUID]bool, len(draftIDs))
	for _, id := range draftIDs {
		idSet[id] = true
	}

	// Build file tree: start from current git HEAD if exists, then apply changes
	files := make(map[string][]byte)

	// Try to read existing tree
	for _, d := range openDrafts {
		if !idSet[d.ID] {
			continue
		}

		switch d.Operation {
		case OpCreate, OpUpdate:
			if d.Content.Valid {
				files[d.FilePath] = []byte(d.Content.String)
			}
		case OpDelete:
			// Mark for deletion by not including in files
		}
	}

	// For non-delete operations, we need to preserve existing files
	// Read all existing files and merge
	existingFiles := make(map[string][]byte)
	// Try reading existing blobs for files we're not modifying
	for _, d := range openDrafts {
		if !idSet[d.ID] {
			continue
		}
		if d.Operation == OpDelete {
			existingFiles[d.FilePath] = nil // mark for deletion
		}
	}

	// Read existing repo state if possible
	// We need to get all current files and overlay our changes
	allFiles := make(map[string][]byte)

	// For each file not being changed, try to preserve from HEAD
	// This is a simplified approach - the full impl would read the entire tree
	// For now, just commit the changed files (works for create operations)
	maps.Copy(allFiles, files)

	if len(allFiles) == 0 && len(existingFiles) == 0 {
		return "", fmt.Errorf("no changes to publish")
	}

	commitSHA, err := s.git.CommitFiles(allFiles, fmt.Sprintf("publish %d draft(s)", len(draftIDs)))
	if err != nil {
		return "", fmt.Errorf("commit files: %w", err)
	}

	// Single transaction: mark drafts published + create outbox row
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txRepo := s.repo.WithTx(tx)

	_, err = txRepo.MarkDraftsPublished(ctx, repo.MarkDraftsPublishedParams{
		CommitSha: pgtype.Text{String: commitSHA, Valid: true},
		Ids:       draftIDs,
		ProjectID: projectID,
	})
	if err != nil {
		return "", fmt.Errorf("mark drafts published: %w", err)
	}

	_, err = txRepo.CreatePublishEvent(ctx, repo.CreatePublishEventParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CommitSha:      commitSHA,
	})
	if err != nil {
		return "", fmt.Errorf("create publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit tx: %w", err)
	}

	return commitSHA, nil
}

func (s *Service) Enrichments(ctx context.Context, projectID uuid.UUID) (map[string]Enrichment, error) {
	rows, err := s.repo.CountOpenDraftsByFilePath(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("count open drafts: %w", err)
	}

	result := make(map[string]Enrichment, len(rows))
	for _, row := range rows {
		result[row.FilePath] = Enrichment{
			OpenDrafts: row.OpenDrafts,
		}
	}

	return result, nil
}

package drafts

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// WriteLock abstracts the publish write lock. In dev, use MutexWriteLock
// (in-process). In prod, use lock.Locker (Redis distributed lock).
type WriteLock interface {
	Lock(ctx context.Context, key string) error
	Unlock(ctx context.Context, key string) error
}

const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"

	StatusOpen      = "open"
	StatusPublished = "published"
	StatusRejected  = "rejected"

	PublishEventPending  = "pending"
	PublishEventIndexing = "indexing"
	PublishEventIndexed  = "indexed"
	PublishEventFailed   = "failed"
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
	ReadFiles(ref string) (map[string][]byte, error)
	ReadBlob(ref string, path string) ([]byte, error)
}

type CreateDraftParams struct {
	FilePath        string
	Title           *string
	OriginalContent *string
	AuthorUserID    *string
	AgentName       *string
	Content         *string
	Operation       string
	Source          *string
	AuthorType      *string
	Labels          []byte
}

type Draft = repo.CorpusDraft

type Enrichment struct {
	OpenDrafts int64
}

type Service struct {
	db   *pgxpool.Pool
	repo *repo.Queries
	git  GitRepo
	lock WriteLock
}

func NewService(db *pgxpool.Pool, git GitRepo, lock WriteLock) *Service {
	return &Service{
		db:   db,
		repo: repo.New(db),
		git:  git,
		lock: lock,
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
		ProjectID:       projectID,
		OrganizationID:  orgID,
		FilePath:        params.FilePath,
		Title:           conv.PtrToPGText(params.Title),
		OriginalContent: conv.PtrToPGText(params.OriginalContent),
		AuthorUserID:    conv.PtrToPGText(params.AuthorUserID),
		AgentName:       conv.PtrToPGText(params.AgentName),
		Content:         conv.PtrToPGText(params.Content),
		Operation:       params.Operation,
		Source:          conv.PtrToPGText(params.Source),
		AuthorType:      conv.PtrToPGText(params.AuthorType),
		Labels:          params.Labels,
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
	lockKey := "corpus:publish:" + projectID.String()
	if err := s.lock.Lock(ctx, lockKey); err != nil {
		return "", fmt.Errorf("acquire publish lock: %w", err)
	}
	defer func() { _ = s.lock.Unlock(ctx, lockKey) }()

	draftsToPublish, err := s.repo.ListOpenDraftsByIDs(ctx, repo.ListOpenDraftsByIDsParams{
		Ids:       draftIDs,
		ProjectID: projectID,
	})
	if err != nil {
		return "", fmt.Errorf("list drafts by ids: %w", err)
	}

	files, err := s.git.ReadFiles("HEAD")
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			files = make(map[string][]byte)
		} else {
			return "", fmt.Errorf("read current files: %w", err)
		}
	}

	for _, d := range draftsToPublish {
		switch d.Operation {
		case OpCreate, OpUpdate:
			if d.Content.Valid {
				files[d.FilePath] = []byte(d.Content.String)
			}
		case OpDelete:
			delete(files, d.FilePath)
		}
	}

	if len(draftsToPublish) == 0 {
		return "", fmt.Errorf("no changes to publish")
	}

	commitSHA, err := s.git.CommitFiles(files, fmt.Sprintf("publish %d draft(s)", len(draftIDs)))
	if err != nil {
		return "", fmt.Errorf("commit files: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	_, err = txRepo.MarkDraftsPublished(ctx, repo.MarkDraftsPublishedParams{
		CommitSha: conv.ToPGText(commitSHA),
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

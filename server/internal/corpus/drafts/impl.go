package drafts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
	db   *pgxpool.Pool
	repo *repo.Queries
	git  GitRepo
}

func NewService(db *pgxpool.Pool, git GitRepo) *Service {
	return &Service{
		db:   db,
		repo: repo.New(db),
		git:  git,
	}
}

func (s *Service) Create(ctx context.Context, projectID uuid.UUID, orgID string, params CreateDraftParams) (*Draft, error) {
	return nil, errors.New("not implemented")
}

func (s *Service) Get(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (*Draft, error) {
	return nil, errors.New("not implemented")
}

func (s *Service) List(ctx context.Context, projectID uuid.UUID, status *string) ([]Draft, error) {
	return nil, errors.New("not implemented")
}

func (s *Service) UpdateContent(ctx context.Context, projectID uuid.UUID, id uuid.UUID, content string) (*Draft, error) {
	return nil, errors.New("not implemented")
}

func (s *Service) Delete(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (*Draft, error) {
	return nil, errors.New("not implemented")
}

func (s *Service) Publish(ctx context.Context, projectID uuid.UUID, orgID string, draftIDs []uuid.UUID) (string, error) {
	return "", errors.New("not implemented")
}

func (s *Service) Enrichments(ctx context.Context, projectID uuid.UUID) (map[string]Enrichment, error) {
	return nil, errors.New("not implemented")
}

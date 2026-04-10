package drafts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/corpus/drafts/repo"
)

var ErrNotFound = errors.New("draft not found")
var ErrInvalidOperation = errors.New("invalid operation: must be create, update, or delete")
var ErrEmptyFilePath = errors.New("file_path must not be empty")

type GitRepo interface {
	CommitFiles(files map[string][]byte, message string) (string, error)
	ReadBlob(ref string, path string) ([]byte, error)
	ReadTree(ref string) ([]TreeEntry, error)
}

type TreeEntry struct {
	Path string
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

// ReadGitBlob exposes the git repo for testing published content.
func (s *Service) ReadGitBlob(ref string, path string) ([]byte, error) {
	return s.git.ReadBlob(ref, path)
}

// SeedGitFile commits a single file to the git repo (for test setup).
func (s *Service) SeedGitFile(path string, content []byte) (string, error) {
	// Read existing tree to preserve other files
	var files map[string][]byte
	if entries, err := s.git.ReadTree("HEAD"); err == nil {
		files = make(map[string][]byte, len(entries)+1)
		for _, e := range entries {
			blob, err := s.git.ReadBlob("HEAD", e.Path)
			if err != nil {
				continue
			}
			files[e.Path] = blob
		}
	} else {
		files = make(map[string][]byte, 1)
	}
	files[path] = content
	return s.git.CommitFiles(files, "seed: "+path)
}

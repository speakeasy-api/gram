package annotations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/corpus/annotations/repo"
)

var ErrNotFound = errors.New("annotation not found")

type Annotation = repo.CorpusAnnotation

type CreateParams struct {
	FilePath   string
	AuthorID   string
	AuthorType string
	Content    string
	LineStart  *int32
	LineEnd    *int32
}

type Service struct {
	db   *pgxpool.Pool
	repo *repo.Queries
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{
		db:   db,
		repo: repo.New(db),
	}
}

func (s *Service) Create(ctx context.Context, projectID uuid.UUID, orgID string, params CreateParams) (*Annotation, error) {
	var lineStart pgtype.Int4
	if params.LineStart != nil {
		lineStart = pgtype.Int4{Int32: *params.LineStart, Valid: true}
	}

	var lineEnd pgtype.Int4
	if params.LineEnd != nil {
		lineEnd = pgtype.Int4{Int32: *params.LineEnd, Valid: true}
	}

	a, err := s.repo.CreateAnnotation(ctx, repo.CreateAnnotationParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		FilePath:       params.FilePath,
		AuthorID:       params.AuthorID,
		AuthorType:     params.AuthorType,
		Content:        params.Content,
		LineStart:      lineStart,
		LineEnd:        lineEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("create annotation: %w", err)
	}

	return &a, nil
}

func (s *Service) List(ctx context.Context, projectID uuid.UUID, filePath string) ([]Annotation, error) {
	annotations, err := s.repo.ListAnnotationsByFilePath(ctx, repo.ListAnnotationsByFilePathParams{
		ProjectID: projectID,
		FilePath:  filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}

	return annotations, nil
}

func (s *Service) Delete(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (*Annotation, error) {
	a, err := s.repo.DeleteAnnotation(ctx, repo.DeleteAnnotationParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delete annotation: %w", err)
	}

	return &a, nil
}

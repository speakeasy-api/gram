package autopublish

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish/repo"
)

// Config represents the auto-publish configuration for a project.
type Config struct {
	Enabled          bool
	IntervalMinutes  int32
	MinUpvotes       int32
	AuthorTypeFilter *string
	LabelFilter      []byte
	MinAgeHours      int32
}

// DefaultConfig returns a disabled auto-publish configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:          false,
		IntervalMinutes:  10,
		MinUpvotes:       0,
		AuthorTypeFilter: nil,
		LabelFilter:      nil,
		MinAgeHours:      0,
	}
}

// Draft is a type alias for the generated CorpusDraft model.
type Draft = repo.CorpusDraft

// DraftsPublisher is the interface the auto-publish service uses to batch-publish drafts.
type DraftsPublisher interface {
	Publish(ctx context.Context, projectID uuid.UUID, orgID string, draftIDs []uuid.UUID) (string, error)
}

// Service manages auto-publish configuration and eligible draft queries.
type Service struct {
	db   *pgxpool.Pool
	repo *repo.Queries
}

// NewService creates a new auto-publish service.
func NewService(db *pgxpool.Pool) *Service {
	return &Service{
		db:   db,
		repo: repo.New(db),
	}
}

// GetConfig retrieves the auto-publish configuration for a project.
// Returns the default (disabled) config if none is set.
func (s *Service) GetConfig(ctx context.Context, projectID uuid.UUID) (Config, error) {
	row, err := s.repo.GetAutoPublishConfig(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("get auto-publish config: %w", err)
	}

	return Config{
		Enabled:          row.Enabled,
		IntervalMinutes:  row.IntervalMinutes,
		MinUpvotes:       row.MinUpvotes,
		AuthorTypeFilter: conv.FromPGText[string](row.AuthorTypeFilter),
		LabelFilter:      row.LabelFilter,
		MinAgeHours:      row.MinAgeHours,
	}, nil
}

// SetConfig upserts the auto-publish configuration for a project.
func (s *Service) SetConfig(ctx context.Context, projectID uuid.UUID, orgID string, cfg Config) (Config, error) {
	row, err := s.repo.UpsertAutoPublishConfig(ctx, repo.UpsertAutoPublishConfigParams{
		ProjectID:        projectID,
		OrganizationID:   orgID,
		Enabled:          cfg.Enabled,
		IntervalMinutes:  cfg.IntervalMinutes,
		MinUpvotes:       cfg.MinUpvotes,
		AuthorTypeFilter: conv.PtrToPGText(cfg.AuthorTypeFilter),
		LabelFilter:      cfg.LabelFilter,
		MinAgeHours:      cfg.MinAgeHours,
	})
	if err != nil {
		return Config{}, fmt.Errorf("upsert auto-publish config: %w", err)
	}

	return Config{
		Enabled:          row.Enabled,
		IntervalMinutes:  row.IntervalMinutes,
		MinUpvotes:       row.MinUpvotes,
		AuthorTypeFilter: conv.FromPGText[string](row.AuthorTypeFilter),
		LabelFilter:      row.LabelFilter,
		MinAgeHours:      row.MinAgeHours,
	}, nil
}

// QueryEligibleDrafts returns drafts matching the auto-publish filter criteria.
func (s *Service) QueryEligibleDrafts(ctx context.Context, projectID uuid.UUID, cfg Config) ([]Draft, error) {
	drafts, err := s.repo.QueryEligibleDrafts(ctx, repo.QueryEligibleDraftsParams{
		ProjectID:        projectID,
		MinUpvotes:       cfg.MinUpvotes,
		AuthorTypeFilter: conv.PtrToPGText(cfg.AuthorTypeFilter),
		MinAgeHours:      cfg.MinAgeHours,
	})
	if err != nil {
		return nil, fmt.Errorf("query eligible drafts: %w", err)
	}

	return drafts, nil
}

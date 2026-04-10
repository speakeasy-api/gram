package autopublish

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
func (s *Service) GetConfig(_ context.Context, _ uuid.UUID) (Config, error) {
	// TODO: implement
	return Config{}, nil
}

// SetConfig upserts the auto-publish configuration for a project.
func (s *Service) SetConfig(_ context.Context, _ uuid.UUID, _ string, _ Config) (Config, error) {
	// TODO: implement
	return Config{}, nil
}

// QueryEligibleDrafts returns drafts matching the auto-publish filter criteria.
func (s *Service) QueryEligibleDrafts(_ context.Context, _ uuid.UUID, _ Config) ([]Draft, error) {
	// TODO: implement
	return nil, nil
}

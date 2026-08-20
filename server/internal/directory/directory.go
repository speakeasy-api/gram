package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory/repo"
)

// ErrUserNotFound indicates that no active directory profile is linked to the
// requested user in the requested organization.
var ErrUserNotFound = errors.New("directory user not found")

// Service retrieves directory state from Postgres.
type Service struct {
	db database.DBTX
}

// NewService creates a directory service backed by db.
func NewService(db database.DBTX) *Service {
	return &Service{db: db}
}

// GetUserProfile returns the current directory profile linked to a Gram user
// in an organization. When multiple active profiles are linked to the same
// user, the most recently synchronized profile wins as a complete snapshot.
func (s *Service) GetUserProfile(ctx context.Context, organizationID, userID string) (*UserProfile, error) {
	row, err := repo.New(s.db).GetUserProfileByUserID(ctx, repo.GetUserProfileByUserIDParams{
		UserID:         conv.ToPGText(userID),
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get directory user profile: %w", err)
	}

	attributes := make(map[string]any)
	if err := json.Unmarshal(row.Attributes, &attributes); err != nil {
		return nil, fmt.Errorf("decode directory user attributes: %w", err)
	}

	groups := make([]Group, 0)
	if err := json.Unmarshal([]byte(row.GroupsJson), &groups); err != nil {
		return nil, fmt.Errorf("decode directory user groups: %w", err)
	}

	return &UserProfile{
		ID:            row.ID,
		UserID:        conv.FromPGTextOrEmpty[string](row.UserID),
		ExternalID:    row.ExternalID,
		Email:         conv.FromPGTextOrEmpty[string](row.Email),
		RawAttributes: attributes,
		Groups:        groups,
	}, nil
}

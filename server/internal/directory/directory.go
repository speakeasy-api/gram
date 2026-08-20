package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory/repo"
)

// ErrUserNotFound indicates that no active directory profile is linked to the
// requested user in the requested organization.
var ErrUserNotFound = errors.New("directory user not found")

// Group describes an active directory group assigned to a user.
type Group struct {
	// ExternalID is the group identifier assigned by the directory provider.
	ExternalID string `json:"external_id"`

	// Name is the display name supplied by the directory provider.
	Name string `json:"name"`
}

// UserProfile is the current directory state associated with a Gram user.
type UserProfile struct {
	// ID is the internal identifier for the selected directory user row.
	ID uuid.UUID

	// UserID is the Gram user identifier linked to the directory profile.
	UserID string

	// ExternalID is the user identifier assigned by the directory provider.
	ExternalID string

	// Email is the email supplied by the directory provider.
	Email string

	// Attributes contains the directory provider's user attributes.
	Attributes map[string]any

	// Groups contains the active groups assigned to the selected profile.
	Groups []Group
}

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
		ID:         row.ID,
		UserID:     conv.FromPGTextOrEmpty[string](row.UserID),
		ExternalID: row.ExternalID,
		Email:      conv.FromPGTextOrEmpty[string](row.Email),
		Attributes: attributes,
		Groups:     groups,
	}, nil
}

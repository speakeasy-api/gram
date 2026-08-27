package directory

import (
	"bytes"
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
	rawAttributes := bytes.TrimSpace(row.Attributes)
	if len(rawAttributes) > 0 && rawAttributes[0] == '{' {
		if err := json.Unmarshal(rawAttributes, &attributes); err != nil {
			return nil, fmt.Errorf("decode directory user attributes: %w", err)
		}
	} else if !json.Valid(rawAttributes) {
		return nil, errors.New("decode directory user attributes: invalid JSON")
	}

	groups := make([]Group, 0)
	if err := json.Unmarshal([]byte(row.GroupsJson), &groups); err != nil {
		return nil, fmt.Errorf("decode directory user groups: %w", err)
	}

	return &UserProfile{
		ID:             row.ID,
		UserID:         conv.FromPGTextOrEmpty[string](row.UserID),
		ExternalID:     row.ExternalID,
		Email:          conv.FromPGTextOrEmpty[string](row.Email),
		DepartmentName: stringAttribute(attributes, "department_name"),
		JobTitle:       stringAttribute(attributes, "job_title"),
		EmployeeType:   stringAttribute(attributes, "employee_type"),
		DivisionName:   stringAttribute(attributes, "division_name"),
		CostCenterName: stringAttribute(attributes, "cost_center_name"),
		RawAttributes:  attributes,
		Groups:         groups,
	}, nil
}

// AttributeValue identifies one directory attribute assignment.
type AttributeValue struct {
	Key   string
	Value string
}

// UserAssociations contains the active directory groups and attributes
// associated with a user.
type UserAssociations struct {
	GroupIDs   []uuid.UUID
	Attributes []AttributeValue
}

// GroupSummary describes an active directory group and its distinct members.
type GroupSummary struct {
	ID          uuid.UUID
	Name        string
	MemberCount int64
}

// AttributeValueSummary describes an active directory attribute value and its
// distinct members.
type AttributeValueSummary struct {
	AttributeValue
	MemberCount int64
}

// ResolveUserAssociationsByEmails returns the active directory groups and
// non-null attributes associated with each normalized email address.
func (s *Service) ResolveUserAssociationsByEmails(ctx context.Context, organizationID string, emails []string) (map[string]UserAssociations, error) {
	normalized := normalizeEmails(emails)
	if len(normalized) == 0 {
		return map[string]UserAssociations{}, nil
	}

	queries := repo.New(s.db)
	groups, err := queries.ListActiveDirectoryGroupIDsByEmails(ctx, repo.ListActiveDirectoryGroupIDsByEmailsParams{
		OrganizationID: organizationID,
		Emails:         normalized,
	})
	if err != nil {
		return nil, fmt.Errorf("list active directory groups by emails: %w", err)
	}

	attributes, err := queries.ListActiveDirectoryUserAttributesByEmails(ctx, repo.ListActiveDirectoryUserAttributesByEmailsParams{
		OrganizationID: organizationID,
		Emails:         normalized,
	})
	if err != nil {
		return nil, fmt.Errorf("list active directory attributes by emails: %w", err)
	}

	associations := make(map[string]UserAssociations, len(normalized))
	for _, group := range groups {
		association := associations[group.Email]
		association.GroupIDs = append(association.GroupIDs, group.DirectoryGroupID)
		associations[group.Email] = association
	}
	for _, attribute := range attributes {
		association := associations[attribute.Email]
		association.Attributes = append(association.Attributes, AttributeValue{
			Key:   attribute.AttributeKey,
			Value: attribute.AttributeValue,
		})
		associations[attribute.Email] = association
	}

	return associations, nil
}

// ListActiveGroups returns active directory groups and their distinct member
// counts for an organization.
func (s *Service) ListActiveGroups(ctx context.Context, organizationID string) ([]GroupSummary, error) {
	rows, err := repo.New(s.db).ListActiveDirectoryGroups(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list active directory groups: %w", err)
	}

	groups := make([]GroupSummary, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, GroupSummary{
			ID:          row.ID,
			Name:        row.Name,
			MemberCount: row.MemberCount,
		})
	}
	return groups, nil
}

// ListActiveAttributeValues returns active, non-null directory attribute
// values and their distinct member counts for an organization.
func (s *Service) ListActiveAttributeValues(ctx context.Context, organizationID string) ([]AttributeValueSummary, error) {
	rows, err := repo.New(s.db).ListActiveDirectoryAttributeValues(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list active directory attributes: %w", err)
	}

	attributes := make([]AttributeValueSummary, 0, len(rows))
	for _, row := range rows {
		attributes = append(attributes, AttributeValueSummary{
			AttributeValue: AttributeValue{
				Key:   row.AttributeKey,
				Value: row.AttributeValue,
			},
			MemberCount: row.MemberCount,
		})
	}
	return attributes, nil
}

// GroupExists reports whether an active group belongs to the organization.
func (s *Service) GroupExists(ctx context.Context, organizationID string, groupID uuid.UUID) (bool, error) {
	exists, err := repo.New(s.db).DirectoryGroupExists(ctx, repo.DirectoryGroupExistsParams{
		ID:             groupID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return false, fmt.Errorf("check active directory group: %w", err)
	}
	return exists, nil
}

// AttributeValueExists reports whether an active directory user in the
// organization has the given attribute value.
func (s *Service) AttributeValueExists(ctx context.Context, organizationID string, attribute AttributeValue) (bool, error) {
	exists, err := repo.New(s.db).DirectoryAttributeValueExists(ctx, repo.DirectoryAttributeValueExistsParams{
		OrganizationID: organizationID,
		AttributeKey:   []byte(attribute.Key),
		AttributeValue: []byte(attribute.Value),
	})
	if err != nil {
		return false, fmt.Errorf("check active directory attribute: %w", err)
	}
	return exists, nil
}

func normalizeEmails(emails []string) []string {
	normalized := make([]string, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		email = conv.NormalizeEmail(email)
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		normalized = append(normalized, email)
	}
	return normalized
}

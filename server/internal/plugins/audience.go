package plugins

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

const (
	directoryAttributePrincipalPrefix = "directory_attribute:"
	directoryGroupPrincipalPrefix     = "directory_group:"
)

// DirectoryGroupPrincipal returns the plugin-only principal that represents a
// locally stored directory group. It deliberately is not an urn.Principal:
// directory groups are delivery audiences, not RBAC principals.
func DirectoryGroupPrincipal(id uuid.UUID) string {
	return directoryGroupPrincipalPrefix + id.String()
}

// DirectoryAttributePrincipal returns the plugin-only principal for an exact
// directory-user attribute match. Each component is encoded independently so
// arbitrary WorkOS attribute keys and values remain unambiguous.
func DirectoryAttributePrincipal(key, value string) string {
	return directoryAttributePrincipalPrefix + base64.RawURLEncoding.EncodeToString([]byte(key)) + ":" + base64.RawURLEncoding.EncodeToString([]byte(value))
}

// ResolveDirectoryAudiencePrincipalsByEmails resolves each normalized email to
// its active directory-group and directory-attribute delivery audiences in an
// organization. Multiple active directory-user records for the same email
// contribute their combined memberships and attributes.
func ResolveDirectoryAudiencePrincipalsByEmails(ctx context.Context, db workosrepo.DBTX, organizationID string, emails []string) (map[string][]string, error) {
	normalized := make([]string, 0, len(emails))
	for _, email := range emails {
		email = conv.NormalizeEmail(email)
		if email == "" || slices.Contains(normalized, email) {
			continue
		}
		normalized = append(normalized, email)
	}
	if len(normalized) == 0 {
		return map[string][]string{}, nil
	}

	rows, err := workosrepo.New(db).ListActiveDirectoryGroupIDsByEmails(ctx, workosrepo.ListActiveDirectoryGroupIDsByEmailsParams{
		OrganizationID: organizationID,
		Emails:         normalized,
	})
	if err != nil {
		return nil, fmt.Errorf("list active directory groups: %w", err)
	}
	attributes, err := workosrepo.New(db).ListActiveDirectoryUserAttributesByEmails(ctx, workosrepo.ListActiveDirectoryUserAttributesByEmailsParams{
		OrganizationID: organizationID,
		Emails:         normalized,
	})
	if err != nil {
		return nil, fmt.Errorf("list active directory user attributes: %w", err)
	}

	principals := make(map[string][]string, len(normalized))
	for _, row := range rows {
		principals[row.Email] = append(principals[row.Email], DirectoryGroupPrincipal(row.DirectoryGroupID))
	}
	for _, attribute := range attributes {
		principals[attribute.Email] = append(principals[attribute.Email], DirectoryAttributePrincipal(attribute.AttributeKey, attribute.AttributeValue))
	}
	return principals, nil
}

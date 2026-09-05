package plugins

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
)

func ResolveDirectoryAudiencePrincipalsByEmails(ctx context.Context, db database.DBTX, organizationID string, emails []string) (map[string][]string, error) {
	associations, err := directory.NewService(db).ResolveUserAssociationsByEmails(ctx, organizationID, emails)
	if err != nil {
		return nil, fmt.Errorf("resolve directory user associations: %w", err)
	}

	principals := make(map[string][]string, len(associations))
	for email, association := range associations {
		for _, groupID := range association.GroupIDs {
			principals[email] = append(principals[email], directory.GroupPrincipal(groupID))
		}
		for _, attribute := range association.Attributes {
			principals[email] = append(principals[email], directory.AttributePrincipal(attribute.Key, attribute.Value))
		}
	}
	return principals, nil
}

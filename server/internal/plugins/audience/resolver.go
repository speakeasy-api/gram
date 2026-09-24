package audience

import (
	"context"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Audience is one organization audience that can receive a plugin. PrincipalURN
// is transport-neutral internal state: callers must project it through a
// privacy-safe reference before returning it outside the server.
type Audience struct {
	Kind         string
	DisplayName  string
	MemberCount  *int64
	PrincipalURN string
}

// Resolve returns the current roles and directory audiences used by plugin
// assignment flows. Transport adapters decide which fields are safe to expose.
func Resolve(ctx context.Context, db database.DBTX, organizationID string) ([]Audience, error) {
	roles, err := accessrepo.New(db).ListActiveOrganizationRoles(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list roles for plugin assignments: %w", err)
	}
	directoryService := directory.NewService(db)
	groups, err := directoryService.ListActiveGroups(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list directory groups for plugin assignments: %w", err)
	}
	attributes, err := directoryService.ListActiveAttributeValues(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list directory attribute values for plugin assignments: %w", err)
	}

	audiences := make([]Audience, 0, 1+len(roles)+len(groups)+len(attributes))
	audiences = append(audiences, Audience{
		Kind:         "everyone",
		DisplayName:  "Everyone",
		MemberCount:  nil,
		PrincipalURN: urn.PrincipalWildcard,
	})
	for _, role := range roles {
		audiences = append(audiences, Audience{
			Kind:         "role",
			DisplayName:  role.WorkosName,
			MemberCount:  &role.MemberCount,
			PrincipalURN: role.RoleUrn,
		})
	}
	for _, group := range groups {
		audiences = append(audiences, Audience{
			Kind:         "directory_group",
			DisplayName:  group.Name,
			MemberCount:  &group.MemberCount,
			PrincipalURN: directory.GroupPrincipal(group.ID),
		})
	}
	for _, attribute := range attributes {
		audiences = append(audiences, Audience{
			Kind:         "directory_attribute",
			DisplayName:  fmt.Sprintf("%s: %s", attribute.Key, attribute.Value),
			MemberCount:  &attribute.MemberCount,
			PrincipalURN: directory.AttributePrincipal(attribute.Key, attribute.Value),
		})
	}
	return audiences, nil
}

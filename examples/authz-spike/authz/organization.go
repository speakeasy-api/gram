package authz

const (
	// OrgRead permits reading organization metadata.
	OrgRead Permission = "org:read"

	// OrgAdmin permits organization administration, including project creation.
	OrgAdmin Permission = "org:admin"

	// OrgBlockedRead opposes organization read and administration.
	OrgBlockedRead Permission = "org:blocked_read"

	// OrgBlockedAdmin opposes organization administration only.
	OrgBlockedAdmin Permission = "org:blocked_admin"
)

// OrganizationDefinitions declares organization operations without a global bypass.
// Admin implies organization read, not permissions in unrelated resource families.
func OrganizationDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: OrgRead, Resource: Organization,
			AllowedAnchors: []ResourceKind{Organization},
			ImpliedBy:      []Permission{OrgAdmin}, ExcludedBy: []Permission{OrgBlockedRead},
			Forms: []OperationForm{ResourceOperation},
		},
		{
			Name: OrgAdmin, Resource: Organization,
			AllowedAnchors: []ResourceKind{Organization},
			ExcludedBy:     []Permission{OrgBlockedAdmin, OrgBlockedRead},
			Forms:          []OperationForm{ResourceOperation},
		},
		{
			Name: OrgBlockedRead, Resource: Organization,
			AllowedAnchors: []ResourceKind{Organization},
		},
		{
			Name: OrgBlockedAdmin, Resource: Organization,
			AllowedAnchors: []ResourceKind{Organization},
		},
	}
}

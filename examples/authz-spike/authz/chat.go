package authz

const (
	// ChatRead permits explicit grant-based access to an existing chat's content.
	ChatRead Permission = "chat:read"

	// ChatWrite permits explicit grant-based mutations and implies content read.
	ChatWrite Permission = "chat:write"
)

// ChatDefinitions declares explicit content access, not owner admission or
// organization-wide metadata visibility. No chat block scopes are invented.
func ChatDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: ChatRead, Resource: Chat,
			AllowedAnchors: []ResourceKind{Chat, Project, Organization},
			ImpliedBy:      []Permission{ChatWrite},
			Forms:          []OperationForm{ResourceOperation}, RequiredAncestors: []ResourceKind{Project},
		},
		{
			Name: ChatWrite, Resource: Chat,
			AllowedAnchors: []ResourceKind{Chat, Project, Organization},
			Forms:          []OperationForm{ResourceOperation}, RequiredAncestors: []ResourceKind{Project},
		},
	}
}

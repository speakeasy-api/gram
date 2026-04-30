package workos

// EventKind represents the type of events that come from the WorkOS Events API.
// The list of events from the official SDK is missing a few key event types so
// these are manually enumerated in this package.
type EventKind string

const (
	EventKindOrganizationCreated EventKind = "organization.created"
	EventKindOrganizationDeleted EventKind = "organization.deleted"
	EventKindOrganizationUpdated EventKind = "organization.updated"

	EventKindDirectorySyncActivated        EventKind = "dsync.activated"
	EventKindDirectorySyncDeleted          EventKind = "dsync.deleted"
	EventKindDirectorySyncGroupCreated     EventKind = "dsync.group.created"
	EventKindDirectorySyncGroupDeleted     EventKind = "dsync.group.deleted"
	EventKindDirectorySyncGroupUpdated     EventKind = "dsync.group.updated"
	EventKindDirectorySyncGroupUserAdded   EventKind = "dsync.group.user_added"
	EventKindDirectorySyncGroupUserRemoved EventKind = "dsync.group.user_removed"

	EventKindUserCreated EventKind = "user.created"
	EventKindUserDeleted EventKind = "user.deleted"
	EventKindUserUpdated EventKind = "user.updated"

	EventKindOrganizationMembershipCreated EventKind = "organization_membership.created"
	EventKindOrganizationMembershipDeleted EventKind = "organization_membership.deleted"
	EventKindOrganizationMembershipUpdated EventKind = "organization_membership.updated"

	EventKindOrganizationRoleCreated EventKind = "organization_role.created"
	EventKindOrganizationRoleDeleted EventKind = "organization_role.deleted"
	EventKindOrganizationRoleUpdated EventKind = "organization_role.updated"

	EventKindRoleCreated EventKind = "role.created"
	EventKindRoleDeleted EventKind = "role.deleted"
	EventKindRoleUpdated EventKind = "role.updated"
)

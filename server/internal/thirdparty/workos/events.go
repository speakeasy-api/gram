package workos

// EventKind represents the type of events that come from the WorkOS Events API.
// The list of events from the official SDK is missing a few key event types so
// these are manually enumerated in this package.
type EventKind string

const (
	EventKindOrganizationCreated EventKind = "organization.created"
	EventKindOrganizationDeleted EventKind = "organization.deleted"
	EventKindOrganizationUpdated EventKind = "organization.updated"
)

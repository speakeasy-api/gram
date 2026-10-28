package admin

import (
	"time"

	"github.com/google/uuid"
)

// OrganizationActivityFilter narrows a staff audit read before sequence pagination.
type OrganizationActivityFilter struct {
	// OrganizationID selects an exact organization, not a slug.
	OrganizationID string

	// Cursor continues after a previously returned sequence position.
	Cursor *string

	// ProjectID restricts events to a project belonging to OrganizationID.
	ProjectID uuid.NullUUID

	// ActorID matches the persisted actor identifier.
	ActorID *string

	// Action matches the recorded operation.
	Action *string

	// SubjectType matches the recorded resource type, including assistants.
	SubjectType *string

	// SubjectID matches the recorded resource identifier.
	SubjectID *string

	// ActingSurface matches the recorded surface; unknown includes null surfaces.
	ActingSurface *string

	// From is the inclusive creation-time lower bound.
	From *time.Time

	// To is the exclusive creation-time upper bound.
	To *time.Time
}

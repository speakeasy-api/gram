package events

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox"
)

var DirectoryReviewOpenedV1 = outbox.NewEventDef[DirectoryReviewPayloadV1](
	"directory_review.opened_v1",
	"A directory change needs a decision from an organization admin: a new group, a new attribute value, or a mapped source that has gone",
)

var DirectoryReviewResolvedV1 = outbox.NewEventDef[DirectoryReviewPayloadV1](
	"directory_review.resolved_v1",
	"A directory change no longer needs a decision: an admin approved or dismissed it, or a later directory change superseded it",
)

// DirectoryReviewEventID derives the stable delivery id for one item's
// opened or resolved event. Each item opens once and resolves once, so a
// replayed write publishes the same id and Svix delivers it once.
func DirectoryReviewEventID(itemID uuid.UUID, transition string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:directory-review:"+transition+":"+itemID.String()))
}

// DirectoryReviewPayloadV1 describes one review item. It carries IDs, the
// kind and counts only: never a group's name or an attribute's key or value,
// which can identify people.
type DirectoryReviewPayloadV1 struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID string    `json:"organization_id"`
	// Kind is new_group, new_attribute_value or source_missing.
	Kind string `json:"kind"`
	// SourceKind is group or attribute.
	SourceKind string `json:"source_kind"`
	// DirectoryGroupID is set for a group source.
	DirectoryGroupID *uuid.UUID `json:"directory_group_id,omitempty"`
	// Occurrence counts how many times this condition has been raised for the
	// source, starting at 1.
	Occurrence int32 `json:"occurrence"`
	// State is pending when opened, then approved, dismissed or superseded.
	State string `json:"state"`
	// PendingCount is how many items of the organization are pending after
	// this change.
	PendingCount int64     `json:"pending_count"`
	OccurredAt   time.Time `json:"occurred_at"`
}

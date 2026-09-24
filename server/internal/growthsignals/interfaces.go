package growthsignals

import (
	"context"

	"github.com/google/uuid"
)

// PostHogClient is the slice of the PostHog client this package uses. Declaring
// it here rather than depending on the concrete client keeps the vendor types
// out of the emitter and lets callers and tests supply a capture that records
// instead of ships.
type PostHogClient interface {
	// CaptureEvent records one event against a distinct id. Implementations
	// enqueue rather than block, and a disabled client reports success.
	CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]any) error
}

// OrganizationDetails is what an organization id resolves to.
type OrganizationDetails struct {
	// Slug is the organization's URL slug, and is empty when the organization
	// could not be resolved.
	Slug string

	// Name is the organization's display name, and is empty when the
	// organization could not be resolved.
	Name string
}

// ProjectDetails is what a project id resolves to.
type ProjectDetails struct {
	// Slug is the project's URL slug, and is empty when the project could not
	// be resolved.
	Slug string

	// Name is the project's display name, and is empty when the project could
	// not be resolved.
	Name string
}

// Enricher resolves the ids an activity carries into the values an event
// reports. Audit payloads carry ids and little else, and Slack readers need
// names.
//
// A caller that cannot find a row returns zero details and no error: an
// organization that no longer exists is a fact about the event, not a failure
// to resolve it. An error means the lookup itself failed, and the emitter
// degrades the event rather than dropping it.
type Enricher interface {
	// Organization resolves an organization id to its slug and name.
	Organization(ctx context.Context, organizationID string) (OrganizationDetails, error)

	// Project resolves a project id to its slug and name.
	Project(ctx context.Context, projectID uuid.UUID) (ProjectDetails, error)

	// UserEmail resolves a Gram user id to that user's email address.
	UserEmail(ctx context.Context, userID string) (string, error)
}

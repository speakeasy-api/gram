// Package catalogpromotion promotes reviewed, healthy remote MCP sources into the catalogue.
package catalogpromotion

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
)

var (
	ErrNotFound   = errors.New("promotion candidate not found")
	ErrStale      = errors.New("promotion source or evidence changed; refresh the candidate")
	ErrIneligible = errors.New("remote server is not eligible for promotion")
	ErrConflict   = errors.New("this source, endpoint, name, or retry key already belongs to a promotion")
	ErrInvalid    = errors.New("invalid public catalogue metadata")

	// ErrPublicTitleTooLong identifies the bounded public-title validation failure.
	ErrPublicTitleTooLong = errors.New("public title must be 100 characters or fewer")

	// ErrPublicDescriptionTooLong identifies the bounded public-description validation failure.
	ErrPublicDescriptionTooLong = errors.New("public description must be 100 characters or fewer")

	ErrUnavailable = errors.New("catalogue promotion validation temporarily unavailable")
)

// Target pins the source to its canonical tenant and project.
type Target struct {
	// OrganizationID identifies the canonical source tenant.
	OrganizationID string `json:"organization_id"`
	// ProjectID pins the active source project.
	ProjectID uuid.UUID `json:"project_id"`
	// MCPServerID pins the active source deployment.
	MCPServerID uuid.UUID `json:"mcp_server_id"`
}

// PublicMetadata contains only staff-reviewed values intended for public distribution.
type PublicMetadata struct {
	// Name is the reviewed registry identity.
	Name string `json:"public_name"`
	// Title is the reviewed public display name, at most 100 Unicode code points.
	Title string `json:"public_title" jsonschema:"Public title, at most 100 Unicode code points"`
	// Description is the reviewed public service description, at most 100 Unicode code points.
	Description string `json:"public_description" jsonschema:"Public description, at most 100 Unicode code points"`
	// URL is the reviewed canonical public MCP endpoint.
	URL string `json:"public_url"`
	// DocumentationURL is the reviewed primary setup reference.
	DocumentationURL string `json:"documentation_url"`
	// DocsSlug is the stable public setup guide key.
	DocsSlug string `json:"docs_slug"`
	// AuthMode is none or oauth for public setup instructions.
	AuthMode string `json:"auth_mode"`
}

// Request is the exact reviewed change; no source credentials enter it.
type Request struct {
	// Target pins the source ownership boundary.
	Target Target `json:"target"`
	// SourceRevision binds the change to the observed source configuration.
	SourceRevision string `json:"source_revision"`
	// CurationRevision prevents older approvals from surviving rejection and restoration.
	CurationRevision string `json:"curation_revision"`
	// Public contains the exact metadata approved for public distribution.
	Public PublicMetadata `json:"public"`
	// PublicMetadataConfirmed records explicit staff review of public suitability.
	PublicMetadataConfirmed bool `json:"public_metadata_confirmed"`
	// OverrideEligibility explicitly accepts insufficient or unhealthy usage evidence.
	OverrideEligibility bool `json:"override_eligibility,omitempty"`
	// RetryKey identifies one exact requested change.
	RetryKey string `json:"retry_key"`
}

// Candidate exposes identifiers and aggregate evidence, with untrusted URL drafts
// in staff-only SuggestedMetadata. It never includes source credentials.
type Candidate struct {
	// ID identifies the durable candidate and its delivery receipts.
	ID string `json:"id"`
	// OrganizationID identifies the canonical source tenant.
	OrganizationID string `json:"organization_id"`
	// ProjectID identifies the source project; empty after source deletion.
	ProjectID string `json:"project_id"`
	// MCPServerID identifies the source deployment; empty after source deletion.
	MCPServerID string `json:"mcp_server_id"`
	// RemoteMCPServerID identifies the upstream configuration; empty after deletion.
	RemoteMCPServerID string `json:"remote_mcp_server_id"`
	// CreatedAt is the immutable observation creation timestamp for newest-first ordering.
	CreatedAt string `json:"created_at"`
	// CurationRevision pins the exact staff review disposition.
	CurationRevision string `json:"curation_revision"`
	// SuggestedMetadata is untrusted draft input, never an approval or published value.
	SuggestedMetadata *PublicMetadata `json:"suggested_metadata,omitempty"`
	// SourceName is a bounded private label for the staff UI only.
	SourceName string `json:"source_name,omitempty"`
	// SourceRevision binds the change to the observed source configuration.
	SourceRevision string `json:"source_revision"`
	// Eligible reports whether current evidence permits staff promotion.
	Eligible bool `json:"eligible"`
	// Reason is a bounded eligibility or lifecycle code.
	Reason string `json:"reason"`
	// Status is candidate, rejected, archived, or promoted.
	Status string `json:"status"`
	// ObservedSuccesses counts successful calls in the observation window.
	ObservedSuccesses int64 `json:"observed_successes"`
	// ObservedFailures counts known non-success outcomes, including unknown outcomes.
	ObservedFailures int64 `json:"observed_failures"`
	// ObservedDays counts distinct UTC days with successful direct activity.
	ObservedDays int `json:"observed_days"`
	// CheckedAt timestamps the start of the evidence observation.
	CheckedAt string `json:"checked_at,omitempty"`
	// RegistryEntryID identifies the published native catalogue entry.
	RegistryEntryID string `json:"registry_entry_id,omitempty"`
	// DocsStatus distinguishes pending delivery from a queued guide request.
	DocsStatus string `json:"docs_status,omitempty"`
	// DocsURL points to the queued guide request, not a completed guide.
	DocsURL string `json:"docs_url,omitempty"`

	// DocsRevision binds manual retries to an exact delivery state.
	DocsRevision string `json:"docs_revision"`
	// DocsRetryAllowed permits staff to reschedule a failed request outside an active lease.
	DocsRetryAllowed bool `json:"docs_retry_allowed"`
	// DocsAttempts counts all delivery attempts, including prior manual retry cycles.
	DocsAttempts int `json:"docs_attempts"`
	// DocsLastAttemptAt is the start of the last documentation attempt.
	DocsLastAttemptAt string `json:"docs_last_attempt_at,omitempty"`
	// DocsNextAttemptAt is the earliest automatic retry time.
	DocsNextAttemptAt string `json:"docs_next_attempt_at,omitempty"`
	// DocsQueuedAt records acknowledgement of the guide factory request.
	DocsQueuedAt string `json:"docs_queued_at,omitempty"`
	// PromotedAt records the original reviewed catalogue publication.
	PromotedAt string `json:"promoted_at,omitempty"`
	// PublicTitle is the reviewed public display name, never a source label.
	PublicTitle string `json:"public_title,omitempty"`
	// DeliveryError is a bounded delivery failure or configuration code.
	DeliveryError string `json:"delivery_error,omitempty"`
}

// CanOverrideEligibility reports whether staff may explicitly accept this candidate's health evidence.
func (c Candidate) CanOverrideEligibility() bool {
	return c.Status == "candidate" && !c.Eligible && CanOverrideEligibility(c.Reason)
}

// HealthReader supplies the existing server-specific outcomes and direct-call series.
type HealthReader interface {
	Outcomes(context.Context, telemetry.MCPServerTelemetryTarget, time.Time, time.Time) (*telemetry.MCPServerOutcomes, error)
	Series(context.Context, telemetry.MCPServerTelemetryTarget, time.Time, time.Time, time.Duration) ([]telemetry.MCPServerSeriesPoint, error)
}

// DeliveryClient submits idempotent documentation jobs.
type DeliveryClient interface {
	EnsureGuide(context.Context, delivery.Guide) (string, error)
}

// DocsRetryRequest identifies an exact staff-approved documentation retry.
type DocsRetryRequest struct {
	// OrganizationID scopes the persisted promotion.
	OrganizationID string `json:"organization_id"`
	// PromotionID identifies the durable publication receipt, including orphaned sources.
	PromotionID uuid.UUID `json:"promotion_id"`
	// DocsRevision binds the request to reviewed delivery state.
	DocsRevision string `json:"docs_revision"`
	// RetryKey identifies one exact retry request.
	RetryKey string `json:"retry_key"`
}

// JobStatus contains bounded aggregate operational health without tenant information.
type JobStatus struct {
	// PendingDocs counts guide requests waiting for automatic delivery.
	PendingDocs int64
	// NeedsAttention counts parked requests requiring staff action.
	NeedsAttention int64
	// OldestPendingAt is the earliest publication awaiting a guide request.
	OldestPendingAt time.Time
	// LastDiscoveryAt is the most recent started discovery observation.
	LastDiscoveryAt time.Time
	// DiscoveryFailures counts candidates whose latest discovery attempt failed.
	DiscoveryFailures int64
}

// CurationRequest identifies one exact staff review disposition change.
type CurationRequest struct {
	// OrganizationID scopes the stored candidate to its canonical tenant.
	OrganizationID string `json:"organization_id"`
	// PromotionID identifies the unpublished candidate.
	PromotionID uuid.UUID `json:"promotion_id"`
	// CurationRevision binds the change to the reviewed disposition.
	CurationRevision string `json:"curation_revision"`
	// Action selects reject, archive, or restore.
	Action string `json:"action"`
	// RetryKey identifies the exact staff request.
	RetryKey string `json:"retry_key"`
}

package adminmcp

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/catalogauth"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
)

type catalogPromotionReader interface {
	ListCandidates(context.Context, string, int, string) ([]catalogpromotion.Candidate, string, error)
	InspectCandidate(context.Context, catalogpromotion.Target) (catalogpromotion.Candidate, error)
}

// CatalogPromotionTargetInput selects one source without accepting credentials or URLs.
type CatalogPromotionTargetInput struct {
	// OrganizationID is the canonical source organization.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID"`
	// ProjectID is the canonical source project.
	ProjectID string `json:"project_id" jsonschema:"Exact canonical project UUID"`
	// MCPServerID is the remote source's server identity.
	MCPServerID string `json:"mcp_server_id" jsonschema:"Exact MCP server UUID"`
}

// ListCatalogPromotionInput bounds candidate enumeration.
type ListCatalogPromotionInput struct {
	// Cursor continues the previous page.
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next cursor from the previous page"`
	// Status selects active (default), all, candidate, rejected, archived, or promoted.
	Status string `json:"status,omitempty" jsonschema:"Queue filter: active,all,candidate,rejected,archived,promoted; default active"`
	// Limit bounds returned candidates.
	Limit int `json:"limit,omitempty" jsonschema:"Page size from 1 to 50; default 20"`
}

// CatalogPromotionPage contains private staff evidence without source configuration.
type CatalogPromotionPage struct {
	// Candidates contains aggregate eligibility and delivery state.
	Candidates []CatalogPromotionCandidate `json:"candidates"`
	// NextCursor continues the bounded scan.
	NextCursor string `json:"next_cursor,omitempty"`
}

func registerCatalogPromotionTools(server *mcp.Server, reader catalogPromotionReader) {
	mcp.AddTool(server, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: "list_registry_promotion_candidates", Title: "List Catalogue Promotion Candidates",
		Description: "List newest-first remote MCP promotion candidates, defaulting to active unpublished candidates excluding endpoints already in the catalogue; status filters include all, candidate, rejected, archived, and promoted. Returns a bounded staff-only page with aggregate health evidence, setup documentation delivery status, and auth_discovery for promoted entries when available. docs_status is pending, retrying, needs_attention, queued (guide request filed), existing_guide (a setup guide already exists, so none was filed) or guide_in_review (waiting on an open guide pull request, rechecked daily); docs_url links to the matching GitHub issue, guide folder or pull request. Authentication capabilities are advertised evidence, not a verified connection; unknown setup does not mean manual setup. Source URLs, credentials, headers, tool arguments and customer text are omitted. Eligibility does not authorize publication.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListCatalogPromotionInput) (*mcp.CallToolResult, CatalogPromotionPage, error) {
		out := CatalogPromotionPage{Candidates: []CatalogPromotionCandidate{}, NextCursor: ""}
		if reader == nil || !verifiedStaff(ctx) {
			return nil, out, errRegistryUnavailable
		}
		if len(input.Cursor) > maxRegistryCursor || input.Limit < 0 || input.Limit > maxRegistryPage {
			return nil, out, errors.New("provide a cursor up to 2048 bytes and a limit from 1 to 50")
		}
		limit := input.Limit
		if limit == 0 {
			limit = 20
		}
		status := input.Status
		if status == "" {
			status = "active"
		}
		switch status {
		case "active", "all", "candidate", "rejected", "archived", "promoted":
		default:
			return nil, out, errors.New("provide a supported promotion status filter")
		}
		candidates, next, err := reader.ListCandidates(ctx, input.Cursor, limit, status)
		if err != nil || len(candidates) > limit || len(next) > maxRegistryCursor {
			return nil, out, errRegistryUnavailable
		}
		for _, candidate := range candidates {
			safe, ok := projectCatalogPromotion(candidate)
			if !ok {
				return nil, CatalogPromotionPage{Candidates: []CatalogPromotionCandidate{}, NextCursor: ""}, errRegistryUnavailable
			}
			out.Candidates = append(out.Candidates, safe)
		}
		out.NextCursor = next
		return nil, out, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: "get_registry_promotion_candidate", Title: "Inspect Catalogue Promotion Candidate",
		Description: "Refresh aggregate promotion eligibility for an exact organization, project and remote MCP server. Returns current source revision, eligibility reasons, bounded advertised authentication capabilities and delivery status without source URLs or credentials. Authentication discovery is metadata evidence, not a verified connection. Use the revision to prepare_registry_promotion; staff must independently review every public field before approval.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(true), Title: ""},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CatalogPromotionTargetInput) (*mcp.CallToolResult, CatalogPromotionCandidate, error) {
		if reader == nil || !verifiedStaff(ctx) {
			return nil, CatalogPromotionCandidate{}, errRegistryUnavailable
		}
		if input.OrganizationID == "" || len(input.OrganizationID) > maxRegistryQuery || !validRegistryUUID(input.ProjectID) || !validRegistryUUID(input.MCPServerID) {
			return nil, CatalogPromotionCandidate{}, errors.New("provide exact organization, project and server IDs")
		}
		candidate, err := reader.InspectCandidate(ctx, catalogpromotion.Target{OrganizationID: input.OrganizationID, ProjectID: uuid.MustParse(input.ProjectID), MCPServerID: uuid.MustParse(input.MCPServerID)})
		if err != nil || candidate.OrganizationID != input.OrganizationID || candidate.ProjectID != input.ProjectID || candidate.MCPServerID != input.MCPServerID {
			return nil, CatalogPromotionCandidate{}, errRegistryUnavailable
		}
		safe, ok := projectCatalogPromotion(candidate)
		if !ok {
			return nil, CatalogPromotionCandidate{}, errRegistryUnavailable
		}
		return nil, safe, nil
	})
}

// CatalogPromotionCandidate is the bounded staff projection; source configuration is never serialized.
type CatalogPromotionCandidate struct {
	// AuthDiscovery reports bounded public capability evidence without issuer URLs.
	AuthDiscovery *catalogauth.Discovery `json:"auth_discovery,omitempty"`
	// ID identifies the stored observation.
	ID string `json:"id"`
	// OrganizationID identifies the canonical source tenant.
	OrganizationID string `json:"organization_id"`
	// ProjectID identifies the canonical source project.
	ProjectID string `json:"project_id"`
	// MCPServerID identifies the reviewed deployment.
	MCPServerID string `json:"mcp_server_id"`
	// RemoteMCPServerID identifies its remote configuration.
	RemoteMCPServerID string `json:"remote_mcp_server_id"`
	// CreatedAt is the immutable candidate creation timestamp.
	CreatedAt string `json:"created_at"`
	// CurationRevision pins the exact staff review disposition.
	CurationRevision string `json:"curation_revision"`
	// SourceRevision pins the reviewed configuration.
	SourceRevision string `json:"source_revision"`
	// Eligible reports current qualification.
	Eligible bool `json:"eligible"`
	// CanOverrideEligibility reports whether staff may explicitly accept the health evidence.
	CanOverrideEligibility bool `json:"can_override_eligibility"`
	// Reason is a fixed qualification category.
	Reason string `json:"reason"`
	// Status distinguishes candidates from published entries.
	Status string `json:"status"`
	// ObservedSuccesses counts successful calls.
	ObservedSuccesses int64 `json:"observed_successes"`
	// ObservedFailures counts failed calls.
	ObservedFailures int64 `json:"observed_failures"`
	// ObservedDays counts active UTC days.
	ObservedDays int `json:"observed_days"`
	// CheckedAt timestamps the health observation.
	CheckedAt string `json:"checked_at,omitempty"`
	// RegistryEntryID identifies a committed public entry.
	RegistryEntryID string `json:"registry_entry_id,omitempty"`
	// DocsStatus reports guide-request delivery: pending, retrying, needs_attention, queued, existing_guide or guide_in_review.
	DocsStatus string `json:"docs_status,omitempty"`
	// DocsURL links only to a GitHub guide issue, guide folder or guide pull request.
	DocsURL string `json:"docs_url,omitempty"`
	// DocsRevision binds a retry to reviewed delivery state.
	DocsRevision string `json:"docs_revision"`
	// DocsRetryAllowed reports whether staff can request a retry.
	DocsRetryAllowed bool `json:"docs_retry_allowed"`
	// DocsAttempts counts delivery attempts.
	DocsAttempts int `json:"docs_attempts"`
	// DocsLastAttemptAt records the latest delivery attempt.
	DocsLastAttemptAt string `json:"docs_last_attempt_at,omitempty"`
	// DocsNextAttemptAt records the next automatic attempt.
	DocsNextAttemptAt string `json:"docs_next_attempt_at,omitempty"`
	// DocsQueuedAt records guide-factory acknowledgement, or when an existing guide was found.
	DocsQueuedAt string `json:"docs_queued_at,omitempty"`
	// PromotedAt records catalogue publication.
	PromotedAt string `json:"promoted_at,omitempty"`
	// PublicTitle is the explicitly reviewed public listing title.
	PublicTitle string `json:"public_title,omitempty"`
	// DeliveryError is a fixed failure category, never upstream error text.
	DeliveryError string `json:"delivery_error,omitempty"`
}

// promotionGuideLink permits only the public GitHub links produced by guide
// delivery: a guide request issue, an open guide pull request, or an existing
// guide folder on the default branch.
var promotionGuideLink = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/(?:issues/[1-9][0-9]*|pull/[1-9][0-9]*|tree/HEAD/guides/[a-z0-9]+(?:-[a-z0-9]+)*)$`)

func projectCatalogPromotion(candidate catalogpromotion.Candidate) (CatalogPromotionCandidate, bool) {
	var empty CatalogPromotionCandidate
	if !validRegistryUUID(candidate.ID) {
		return empty, false
	}
	for _, sourceID := range []string{candidate.ProjectID, candidate.MCPServerID, candidate.RemoteMCPServerID} {
		if sourceID == "" && candidate.Status == "promoted" {
			continue
		}
		if !validRegistryUUID(sourceID) {
			return empty, false
		}
	}
	if candidate.OrganizationID == "" || len(candidate.OrganizationID) > maxRegistryQuery || strings.TrimSpace(candidate.OrganizationID) != candidate.OrganizationID {
		return empty, false
	}
	if candidate.RegistryEntryID != "" && !validRegistryUUID(candidate.RegistryEntryID) {
		return empty, false
	}
	if candidate.SourceRevision == "" || len(candidate.SourceRevision) > maxRegistryText {
		return empty, false
	}
	if _, err := hex.DecodeString(candidate.SourceRevision); err != nil && (candidate.SourceRevision != "unobserved" || candidate.Status == "promoted" || candidate.Eligible) {
		return empty, false
	}
	if candidate.ObservedSuccesses < 0 || candidate.ObservedFailures < 0 || candidate.ObservedDays < 0 {
		return empty, false
	}
	for _, stamp := range []string{candidate.CreatedAt, candidate.CheckedAt, candidate.DocsLastAttemptAt, candidate.DocsNextAttemptAt, candidate.DocsQueuedAt, candidate.PromotedAt} {
		if stamp != "" {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return empty, false
			}
		}
	}
	if candidate.DocsAttempts < 0 || len(candidate.DocsRevision) > maxRegistryText || len(candidate.PublicTitle) > 200 {
		return empty, false
	}
	if candidate.CurationRevision != "" && !validRegistryUUID(candidate.CurationRevision) {
		return empty, false
	}
	if candidate.DocsRevision != "" && !validRegistryUUID(candidate.DocsRevision) {
		return empty, false
	}
	if candidate.DocsRetryAllowed && (candidate.Status != "promoted" || candidate.DocsRevision == "" || (candidate.DocsStatus != "needs_attention" && candidate.DocsStatus != "retrying")) {
		return empty, false
	}
	switch candidate.Reason {
	case "eligible", "insufficient_evidence", "errors_observed", "logging_disabled", "telemetry_unavailable", "source_changed", "disabled", "already_promoted", "already_catalogued", "unsupported_configuration":
	default:
		return empty, false
	}
	if candidate.Status != "candidate" && candidate.Status != "promoted" && candidate.Status != "rejected" && candidate.Status != "archived" {
		return empty, false
	}
	switch candidate.DocsStatus {
	case "", "pending", "retrying", "queued", "needs_attention", "existing_guide", "guide_in_review":
	default:
		return empty, false
	}
	switch candidate.DeliveryError {
	case "", "docs_transient", "docs_not_configured", "docs_permanent", "docs_attempts_exhausted", "docs_guide_pr_stalled":
	default:
		return empty, false
	}
	if candidate.DocsURL != "" && (len(candidate.DocsURL) > maxRegistryCursor || !promotionGuideLink.MatchString(candidate.DocsURL)) {
		return empty, false
	}
	if candidate.AuthDiscovery != nil && !candidate.AuthDiscovery.Valid() {
		return empty, false
	}
	return CatalogPromotionCandidate{
		AuthDiscovery: candidate.AuthDiscovery,
		ID:            candidate.ID, OrganizationID: candidate.OrganizationID, ProjectID: candidate.ProjectID, MCPServerID: candidate.MCPServerID, RemoteMCPServerID: candidate.RemoteMCPServerID,
		CreatedAt: candidate.CreatedAt, CurationRevision: candidate.CurationRevision, SourceRevision: candidate.SourceRevision, Eligible: candidate.Eligible, CanOverrideEligibility: candidate.CanOverrideEligibility(), Reason: candidate.Reason, Status: candidate.Status,
		ObservedSuccesses: candidate.ObservedSuccesses, ObservedFailures: candidate.ObservedFailures, ObservedDays: candidate.ObservedDays, CheckedAt: candidate.CheckedAt,
		DocsRevision: candidate.DocsRevision, DocsRetryAllowed: candidate.DocsRetryAllowed, DocsAttempts: candidate.DocsAttempts,
		DocsLastAttemptAt: candidate.DocsLastAttemptAt, DocsNextAttemptAt: candidate.DocsNextAttemptAt, DocsQueuedAt: candidate.DocsQueuedAt, PromotedAt: candidate.PromotedAt, PublicTitle: candidate.PublicTitle,
		RegistryEntryID: candidate.RegistryEntryID, DocsStatus: candidate.DocsStatus, DocsURL: candidate.DocsURL, DeliveryError: candidate.DeliveryError,
	}, true
}

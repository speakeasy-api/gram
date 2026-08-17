package risk

import (
	"context"

	"github.com/google/uuid"
)

// ShadowMCPApprovalIntake admits a blocked shadow-MCP server into the MCP
// approval workflow: the blocked employee's ask attaches as a requester on
// the server's single review — evidence gathered, deduplicated by canonical
// URL — instead of minting a per-user bypass request. It is the seam that
// makes approval the one flow: the block link redeems into the same review
// an admin decides, and the decision is what changes enforcement.
//
// Implemented by the mcpapproval service and injected at wiring, so this
// package never imports it. A nil intake, or an intake reporting the approval
// feature is unavailable, falls back to the legacy bypass request.
type ShadowMCPApprovalIntake interface {
	// AdmitBlockedServer records the ask and returns the id and current
	// status of the review it landed on — a repeat ask for an
	// already-approved server attaches without reopening it. A forbidden
	// error means the approval workflow is not enabled for the organization
	// and the caller should fall back to the legacy bypass request.
	AdmitBlockedServer(ctx context.Context, organizationID string, projectID uuid.UUID, serverURL, requesterUserID, requesterEmail, note string) (requestID string, status string, err error)
}

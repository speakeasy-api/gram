package risk

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	// ReconcileStandingDecisionsForPolicy replays the project's recorded
	// decisions onto a newly blocking policy, inside the transaction that
	// creates or transitions it. Without it, ordering decides what an
	// approval means: a policy created after decisions were recorded would
	// block servers whose reviews still read approved. A returned shareable
	// error (an inexpressible blast radius) aborts the policy write with its
	// explanation intact.
	ReconcileStandingDecisionsForPolicy(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, policyID uuid.UUID) error

	// ReviewShadowMCPPolicyURLEdit names the standing decisions a URL-list
	// edit on an already-blocking policy would contradict, plus every URL
	// whose grants carry a standing decision (so the reconciler can leave
	// retained ones untouched). The counterpart of the replay: when a policy
	// becomes blocking the replay runs and decisions win; when its list is
	// edited this review runs and the edit wins — only after the caller
	// confirms superseding what it contradicts. A nil URL list means that
	// list is not being edited.
	ReviewShadowMCPPolicyURLEdit(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, policyID uuid.UUID, disposition string, desiredAllowedURLs []string, desiredBlockedURLs []string) (shadowmcp.StandingDecisionReview, error)

	// SupersedeShadowMCPDecisions transitions each conflicted request to
	// superseded — actor-attributed and audit-logged, decision history and
	// rationale intact — in the same transaction as the policy edit that
	// displaces it.
	SupersedeShadowMCPDecisions(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, conflicts []shadowmcp.StandingDecisionConflict, actor urn.Principal, actorDisplayName *string) error
}

package access

import (
	"context"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// requireLiveOrgAdmin proves org:admin against the organization's current
// membership rather than the grants prepared when the session was minted.
//
// The Shadow AI reads are organization-wide lists of what named employees run,
// and the write is an organization-wide security control, so an administrator
// whose role was revoked after their session was prepared must not still reach
// either. Support sessions keep the prepared-grant path: their grants are
// minted per operator action and are the audited thing.
func (s *Service) requireLiveOrgAdmin(ctx context.Context, ac *contextvalues.AuthContext) error {
	if contextvalues.IsSupportSession(ctx) {
		return s.authz.Require(ctx, authz.Check{
			Scope:        authz.ScopeOrgAdmin,
			ResourceKind: "",
			ResourceID:   ac.ActiveOrganizationID,
			Dimensions:   nil,
		})
	}
	if err := s.authz.RequireUserOrganizationScope(ctx, ac.ActiveOrganizationID, ac.UserID, authz.ScopeOrgAdmin); err != nil {
		return fmt.Errorf("authorize organization administrator: %w", err)
	}
	return nil
}

// redactAIDecisionRationale drops the reason behind an access decision.
//
// Every other reader of gen.AIDetection is an org admin. listEmployeeAIDetections
// is not: it answers for one named employee to anyone who can read the project.
// The rationale is free text an administrator wrote for other administrators,
// and it routinely names people and internal threads. The state itself stays —
// that a tool is blocked is the useful half and reaches no person.
func redactAIDecisionRationale(detections []*gen.AIDetection) {
	for _, detection := range detections {
		if detection.Access == nil {
			continue
		}
		detection.Access.Rationale = nil
	}
}

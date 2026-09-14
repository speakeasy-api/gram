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

// redactAIDecisionAttribution drops who recorded an access decision and why.
//
// Every other reader of gen.AIDetection is an org admin. listEmployeeAIDetections
// is not: it answers for one named employee to anyone who can read the project,
// and the decision record it now carries names an administrator. The state
// itself stays — that a tool is blocked is the useful half and nothing about it
// reaches a person.
func redactAIDecisionAttribution(detections []*gen.AIDetection) {
	for _, detection := range detections {
		if detection.Access == nil {
			continue
		}
		detection.Access.DecidedBy = nil
		detection.Access.DecidedAt = nil
		detection.Access.Rationale = nil
	}
}

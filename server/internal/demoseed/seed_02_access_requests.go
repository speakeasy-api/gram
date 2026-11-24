package demoseed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// demoAccessRequestCount is how many access requests the seed writes, for
// the postflight.
const demoAccessRequestCount = 4

// seedRolesAccessRequests gives the Access Requests tab pending requests to
// review and a denied one in its history: Amara asks for the demo project's
// tool logs, Hana asks to edit skills for two hours, Jonas asks to manage
// environments until the end of his day, and Lucas, an admin, denied Hana's
// request to view environments, which hold secret values. None holds the
// scope they asked for.
func (s *seeder) seedRolesAccessRequests(ctx context.Context) error {
	org := s.orgID()
	if err := s.q.RolesDeleteAccessRequests(ctx, org); err != nil {
		return fmt.Errorf("delete demo access requests: %w", err)
	}

	projectSelector, err := json.Marshal(map[string]string{"resource_kind": "project", "resource_id": s.projectID().String()})
	if err != nil {
		return fmt.Errorf("encode demo access request selector: %w", err)
	}
	environmentSelector, err := json.Marshal(authz.NewSelector(authz.ScopeEnvironmentRead, authz.WildcardResource))
	if err != nil {
		return fmt.Errorf("encode demo access request selector: %w", err)
	}
	skillSelector, err := json.Marshal(authz.NewSelector(authz.ScopeSkillWrite, authz.WildcardResource))
	if err != nil {
		return fmt.Errorf("encode demo access request selector: %w", err)
	}
	environmentWriteSelector, err := json.Marshal(authz.NewSelector(authz.ScopeEnvironmentWrite, authz.WildcardResource))
	if err != nil {
		return fmt.Errorf("encode demo access request selector: %w", err)
	}
	noDecision := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	noDuration := pgtype.Int4{Int32: 0, Valid: false}
	noDeadline := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

	requests := []repo.RolesInsertAccessRequestParams{
		{
			ID:                       s.det("access-request-pending"),
			OrganizationID:           org,
			RequesterUserID:          s.userID("amara"),
			Scope:                    "log:read",
			Selector:                 projectSelector,
			ResourceName:             conv.ToPGText("Default"),
			Message:                  conv.ToPGText("A customer reported a failed refund call this morning; I need the tool logs to see what the agent sent."),
			Status:                   "pending",
			DecidedByUserID:          conv.ToPGTextEmpty(""),
			DecidedVia:               conv.ToPGTextEmpty(""),
			DecidedAt:                noDecision,
			RequestedDurationSeconds: noDuration,
			RequestedUntil:           noDeadline,
			ExpiresAt:                conv.ToPGTimestamptz(s.now.Add(20 * time.Hour)),
			CreatedAt:                conv.ToPGTimestamptz(s.now.Add(-4 * time.Hour)),
		},
		{
			ID:                       s.det("access-request-duration"),
			OrganizationID:           org,
			RequesterUserID:          s.userID("hana"),
			Scope:                    string(authz.ScopeSkillWrite),
			Selector:                 skillSelector,
			ResourceName:             conv.ToPGTextEmpty(""),
			Message:                  conv.ToPGText("Fixing a typo in the refunds skill before this afternoon's support rotation."),
			Status:                   "pending",
			DecidedByUserID:          conv.ToPGTextEmpty(""),
			DecidedVia:               conv.ToPGTextEmpty(""),
			DecidedAt:                noDecision,
			RequestedDurationSeconds: pgtype.Int4{Int32: int32((2 * time.Hour) / time.Second), Valid: true},
			RequestedUntil:           noDeadline,
			ExpiresAt:                conv.ToPGTimestamptz(s.now.Add(23 * time.Hour)),
			CreatedAt:                conv.ToPGTimestamptz(s.now.Add(-1 * time.Hour)),
		},
		{
			ID:                       s.det("access-request-deadline"),
			OrganizationID:           org,
			RequesterUserID:          s.userID("jonas"),
			Scope:                    string(authz.ScopeEnvironmentWrite),
			Selector:                 environmentWriteSelector,
			ResourceName:             conv.ToPGTextEmpty(""),
			Message:                  conv.ToPGText("Rotating the staging API credentials today."),
			Status:                   "pending",
			DecidedByUserID:          conv.ToPGTextEmpty(""),
			DecidedVia:               conv.ToPGTextEmpty(""),
			DecidedAt:                noDecision,
			RequestedDurationSeconds: noDuration,
			// The end of Jonas's day, as his own client worked it out.
			RequestedUntil: conv.ToPGTimestamptz(s.now.Add(7 * time.Hour)),
			ExpiresAt:      conv.ToPGTimestamptz(s.now.Add(23*time.Hour + 30*time.Minute)),
			CreatedAt:      conv.ToPGTimestamptz(s.now.Add(-30 * time.Minute)),
		},
		{
			ID:                       s.det("access-request-denied"),
			OrganizationID:           org,
			RequesterUserID:          s.userID("hana"),
			Scope:                    string(authz.ScopeEnvironmentRead),
			Selector:                 environmentSelector,
			ResourceName:             conv.ToPGTextEmpty(""),
			Message:                  conv.ToPGText("Want to check which credentials the billing server is configured with."),
			Status:                   "denied",
			DecidedByUserID:          conv.ToPGText(s.userID("lucas")),
			DecidedVia:               conv.ToPGText("slack"),
			DecidedAt:                conv.ToPGTimestamptz(s.now.Add(-46 * time.Hour)),
			RequestedDurationSeconds: noDuration,
			RequestedUntil:           noDeadline,
			ExpiresAt:                conv.ToPGTimestamptz(s.now.Add(-24 * time.Hour)),
			CreatedAt:                conv.ToPGTimestamptz(s.now.Add(-48 * time.Hour)),
		},
	}
	for _, request := range requests {
		if err := s.q.RolesInsertAccessRequest(ctx, request); err != nil {
			return fmt.Errorf("insert demo access request: %w", err)
		}
	}
	return nil
}

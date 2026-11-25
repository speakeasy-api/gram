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
const demoAccessRequestCount = 3

// seedRolesAccessRequests gives the Access Requests tab pending requests to
// review and a denied one in its history: Amara asks for the demo project's
// tool logs, Priya asks to view environments after the seeded challenges show
// her denied doing so (so Authorization Challenges marks her denied rows as
// requested), and Lucas, an admin, denied Hana's request to view
// environments, which hold secret values. None holds the scope they asked for.
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
	noDecision := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

	requests := []repo.RolesInsertAccessRequestParams{
		{
			ID:              s.det("access-request-pending"),
			OrganizationID:  org,
			RequesterUserID: s.userID("amara"),
			Scope:           "log:read",
			Selector:        projectSelector,
			ResourceName:    conv.ToPGText("Default"),
			Message:         conv.ToPGText("A customer reported a failed refund call this morning; I need the tool logs to see what the agent sent."),
			Status:          "pending",
			DecidedByUserID: conv.ToPGTextEmpty(""),
			DecidedVia:      conv.ToPGTextEmpty(""),
			DecidedAt:       noDecision,
			ExpiresAt:       conv.ToPGTimestamptz(s.now.Add(20 * time.Hour)),
			CreatedAt:       conv.ToPGTimestamptz(s.now.Add(-4 * time.Hour)),
		},
		{
			ID:              s.det("access-request-pending-challenged"),
			OrganizationID:  org,
			RequesterUserID: s.userID("priya"),
			Scope:           string(authz.ScopeEnvironmentRead),
			Selector:        environmentSelector,
			ResourceName:    conv.ToPGTextEmpty(""),
			Message:         conv.ToPGText("Setting up the staging MCP server and need to see which environment it reads its API keys from."),
			Status:          "pending",
			DecidedByUserID: conv.ToPGTextEmpty(""),
			DecidedVia:      conv.ToPGTextEmpty(""),
			DecidedAt:       noDecision,
			ExpiresAt:       conv.ToPGTimestamptz(s.now.Add(23 * time.Hour)),
			CreatedAt:       conv.ToPGTimestamptz(s.now.Add(-1 * time.Hour)),
		},
		{
			ID:              s.det("access-request-denied"),
			OrganizationID:  org,
			RequesterUserID: s.userID("hana"),
			Scope:           string(authz.ScopeEnvironmentRead),
			Selector:        environmentSelector,
			ResourceName:    conv.ToPGTextEmpty(""),
			Message:         conv.ToPGText("Want to check which credentials the billing server is configured with."),
			Status:          "denied",
			DecidedByUserID: conv.ToPGText(s.userID("lucas")),
			DecidedVia:      conv.ToPGText("slack"),
			DecidedAt:       conv.ToPGTimestamptz(s.now.Add(-46 * time.Hour)),
			ExpiresAt:       conv.ToPGTimestamptz(s.now.Add(-24 * time.Hour)),
			CreatedAt:       conv.ToPGTimestamptz(s.now.Add(-48 * time.Hour)),
		},
	}
	for _, request := range requests {
		if err := s.q.RolesInsertAccessRequest(ctx, request); err != nil {
			return fmt.Errorf("insert demo access request: %w", err)
		}
	}
	return nil
}

package access

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// maxPendingForAdmin bounds the device agent's list of pending requests for
// an administrator.
const maxPendingForAdmin = 50

// PendingAccessRequest is a pending request an administrator can decide in
// the dashboard.
type PendingAccessRequest struct {
	// Request is the request.
	Request *gen.AccessRequest

	// PermissionLabel names the scope the way the role editor does.
	PermissionLabel string

	// DecideURL opens the request in the dashboard, or is empty when the
	// deployment has no site URL.
	DecideURL string
}

// ListPendingForAdmin lists the organization's pending requests, newest
// first, at most 50, when the member is an organization administrator by
// their live grants. Anyone else gets an empty list.
func (s *Service) ListPendingForAdmin(ctx context.Context, organizationID, userID string) ([]PendingAccessRequest, error) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	admin, err := isOrganizationAdmin(ctx, s.db, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("check organization admin: %w", err)
	}
	if !admin {
		return []PendingAccessRequest{}, nil
	}
	rows, err := repo.New(s.db).ListAccessRequests(ctx, repo.ListAccessRequestsParams{
		OrganizationID:  organizationID,
		RequesterUserID: pgtype.Text{String: "", Valid: false},
		Status:          pgtype.Text{String: AccessRequestStatusPending, Valid: true},
		MaxRows:         maxPendingForAdmin,
	})
	if err != nil {
		return nil, fmt.Errorf("list pending access requests: %w", err)
	}
	if len(rows) == 0 {
		return []PendingAccessRequest{}, nil
	}
	views, err := s.accessRequestViews(ctx, s.db, rows)
	if err != nil {
		return nil, fmt.Errorf("render access requests: %w", err)
	}
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}

	out := make([]PendingAccessRequest, 0, len(views))
	for _, view := range views {
		label := view.Scope
		if entry, ok := authz.ScopeEntryFor(authz.Scope(view.Scope)); ok {
			label = entry.Label
		}
		resourceID := ""
		if view.Selector != nil && view.Selector.ResourceID != authz.WildcardResource {
			resourceID = view.Selector.ResourceID
		}
		// The Access Requests tab opens the decision dialog from the same
		// query the emails carry.
		link := s.manageAccessLink(ctx, logger, org.Slug, org.DefaultHost, "requests", organizationID, view.RequesterUserID, view.Scope, resourceID, view)
		out = append(out, PendingAccessRequest{Request: view, PermissionLabel: label, DecideURL: link})
	}
	return out, nil
}

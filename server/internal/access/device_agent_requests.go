package access

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
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

	projects := s.mcpRequestProjects(ctx, logger, organizationID, views)
	out := make([]PendingAccessRequest, 0, len(views))
	for _, view := range views {
		label := view.Scope
		if entry, ok := authz.ScopeEntryFor(authz.Scope(view.Scope)); ok {
			label = entry.Label
		}
		resourceID := requestResourceID(view)
		// The Access Requests tab opens the decision dialog from the same
		// query the emails carry.
		link := s.accessLinkURL(accessLink{
			OrgSlug:         org.Slug,
			DefaultHost:     org.DefaultHost,
			Tab:             accessLinkTabRequests,
			RequesterUserID: view.RequesterUserID,
			Scope:           view.Scope,
			ResourceID:      resourceID,
			ProjectID:       projects[resourceID],
			RequestID:       view.ID,
		})
		out = append(out, PendingAccessRequest{Request: view, PermissionLabel: label, DecideURL: link})
	}
	return out, nil
}

// requestResourceID is the resource a request names, or empty when it covers
// every resource of its kind.
func requestResourceID(view *gen.AccessRequest) string {
	if view.Selector == nil || view.Selector.ResourceID == authz.WildcardResource {
		return ""
	}
	return view.Selector.ResourceID
}

// mcpRequestProjects resolves the project of every MCP resource the requests
// name with one query, keyed by resource id. It is best effort: a failed
// lookup leaves the links without a project.
func (s *Service) mcpRequestProjects(ctx context.Context, logger *slog.Logger, organizationID string, views []*gen.AccessRequest) map[string]string {
	ids := make([]uuid.UUID, 0, len(views))
	for _, view := range views {
		if !strings.HasPrefix(view.Scope, "mcp:") {
			continue
		}
		if id, err := uuid.Parse(requestResourceID(view)); err == nil && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	projects := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return projects
	}
	rows, err := repo.New(s.db).FindMCPResourceProjects(ctx, repo.FindMCPResourceProjectsParams{OrganizationID: organizationID, ResourceIds: ids})
	if err != nil {
		logger.WarnContext(ctx, "could not resolve MCP projects for access request links", attr.SlogError(err))
		return projects
	}
	for _, row := range rows {
		projects[row.ResourceID.String()] = row.ProjectID.String()
	}
	return projects
}

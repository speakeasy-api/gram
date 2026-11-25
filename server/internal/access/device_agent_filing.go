package access

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// maxOwnPendingForDevice bounds the device agent's list of a member's own
// pending requests.
const maxOwnPendingForDevice = 50

// projectScopeCheck returns the check that says whether a member holds a
// temporary-grant scope that applies to a project on one project. ok is false
// for a scope that can only be asked for on every project. project:read is
// one: the projects offered are the ones the member can already read.
func projectScopeCheck(scope authz.Scope) (check func(authz.ProjectRef) authz.Check, ok bool) {
	switch scope {
	case authz.ScopeProjectWrite:
		return authz.ProjectRef.Write, true
	case authz.ScopeLogRead:
		return authz.ProjectRef.LogRead, true
	default:
		return nil, false
	}
}

// RequestablePermission is a permission a member can ask for as temporary
// access from the device agent.
type RequestablePermission struct {
	// Entry describes the scope the way the role editor does.
	Entry authz.ScopeEntry

	// AllResources is true when the member does not already hold the scope on
	// every resource of its kind, so they can ask for all of them.
	AllResources bool

	// Projects are, for a scope that applies to a project, the projects the
	// member can read but does not hold the scope on.
	Projects []RequestableProject
}

// RequestableProject is a project a member can ask for a scope on.
type RequestableProject struct {
	ID   string
	Name string
}

// RequestablePermissions lists, in the role editor's order, the scopes that
// can be given temporarily and that grants do not already give on every
// resource. grants are the member's live grants.
func (s *Service) RequestablePermissions(ctx context.Context, organizationID string, grants []authz.Grant) ([]RequestablePermission, error) {
	var projects []projectsrepo.Project
	projectsLoaded := false
	out := make([]RequestablePermission, 0, len(temporaryGrantScopes))
	for _, entry := range authz.ScopeCatalog() {
		if !slices.Contains(temporaryGrantScopes, entry.Scope) {
			continue
		}
		if authz.GrantsCoverEveryResource(grants, entry.Scope) {
			continue
		}
		permission := RequestablePermission{Entry: entry, AllResources: true, Projects: []RequestableProject{}}
		if check, ok := projectScopeCheck(entry.Scope); ok {
			if !projectsLoaded {
				rows, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, organizationID)
				if err != nil {
					return nil, fmt.Errorf("list organization projects: %w", err)
				}
				projects = rows
				projectsLoaded = true
			}
			for _, project := range projects {
				ref := authz.Project(authz.ProjectIDFrom(project.ID.String()))
				visible, err := authz.GrantsAuthorize(grants, ref.Read())
				if err != nil {
					return nil, fmt.Errorf("check project read: %w", err)
				}
				if !visible {
					continue
				}
				held, err := authz.GrantsAuthorize(grants, check(ref))
				if err != nil {
					return nil, fmt.Errorf("check project scope: %w", err)
				}
				if !held {
					permission.Projects = append(permission.Projects, RequestableProject{ID: project.ID.String(), Name: project.Name})
				}
			}
			slices.SortStableFunc(permission.Projects, func(a, b RequestableProject) int {
				switch {
				case a.Name < b.Name:
					return -1
				case a.Name > b.Name:
					return 1
				default:
					return 0
				}
			})
		}
		out = append(out, permission)
	}
	return out, nil
}

// OwnAccessRequest is one of a member's own pending requests.
type OwnAccessRequest struct {
	// Request is the request.
	Request *gen.AccessRequest

	// PermissionLabel names the scope the way the role editor does.
	PermissionLabel string

	// ResourceID is the resource asked for, or empty for every resource of
	// the scope's kind.
	ResourceID string
}

// ListOwnPending lists a member's own pending requests, newest first, at most
// 50.
func (s *Service) ListOwnPending(ctx context.Context, organizationID, userID string) ([]OwnAccessRequest, error) {
	rows, err := repo.New(s.db).ListAccessRequests(ctx, repo.ListAccessRequestsParams{
		OrganizationID:  organizationID,
		RequesterUserID: pgtype.Text{String: userID, Valid: true},
		Status:          pgtype.Text{String: AccessRequestStatusPending, Valid: true},
		MaxRows:         maxOwnPendingForDevice,
	})
	if err != nil {
		return nil, fmt.Errorf("list own pending access requests: %w", err)
	}
	if len(rows) == 0 {
		return []OwnAccessRequest{}, nil
	}
	views, err := s.accessRequestViews(ctx, s.db, rows)
	if err != nil {
		return nil, fmt.Errorf("render access requests: %w", err)
	}
	out := make([]OwnAccessRequest, 0, len(views))
	for _, view := range views {
		// The query leaves out lapsed rows; this covers a request whose
		// window passed between the query and rendering it.
		if view.Status != AccessRequestStatusPending {
			continue
		}
		out = append(out, OwnAccessRequest{Request: view, PermissionLabel: permissionLabel(view.Scope), ResourceID: requestResourceID(view)})
	}
	return out, nil
}

// ErrAccessRequestsOff reports a device agent request filed while access
// requests are off for the organization.
var ErrAccessRequestsOff = errors.New("access requests are off for this organization")

// RequestAccessFromDevice files a request for the member a device agent's
// per-user key belongs to. The agent service has already checked the key and
// that the member may ask for the resource. The request is stored and
// administrators are notified exactly as for a request from the dashboard.
// It reports whether a new request was stored.
func (s *Service) RequestAccessFromDevice(ctx context.Context, ac *contextvalues.AuthContext, payload *gen.RequestAccessPayload) (OwnAccessRequest, bool, error) {
	result, created, err := s.requestAccess(ctx, ac, payload, true)
	if err != nil {
		return OwnAccessRequest{}, false, err
	}
	if result.Request == nil {
		return OwnAccessRequest{}, false, ErrAccessRequestsOff
	}
	view := result.Request
	return OwnAccessRequest{Request: view, PermissionLabel: permissionLabel(view.Scope), ResourceID: requestResourceID(view)}, created, nil
}

// permissionLabel names scope the way the role editor does, or returns the
// slug for an unknown scope.
func permissionLabel(scope string) string {
	if entry, ok := authz.ScopeEntryFor(authz.Scope(scope)); ok {
		return entry.Label
	}
	return scope
}

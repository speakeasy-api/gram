package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

// ListRequestableAccess lists the permissions the enrolled user can ask for
// as temporary access, and their own pending requests.
func (s *Service) ListRequestableAccess(ctx context.Context, _ *gen.ListRequestableAccessPayload) (*gen.ListRequestableAccessResult, error) {
	_, member, err := s.accessRequestMember(ctx)
	if err != nil {
		return nil, err
	}
	permissions, err := s.requestablePermissions(ctx, member)
	if err != nil {
		return nil, err
	}
	pending, err := s.accessRequests.ListOwnPending(ctx, member.organizationID, member.userID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing own access requests").LogError(ctx, s.logger)
	}

	result := &gen.ListRequestableAccessResult{
		Permissions: make([]*gen.AgentRequestablePermission, 0, len(permissions)),
		Pending:     make([]*gen.AgentOwnAccessRequest, 0, len(pending)),
	}
	for _, permission := range permissions {
		projects := make([]*gen.AgentRequestableProject, 0, len(permission.Projects))
		for _, project := range permission.Projects {
			projects = append(projects, &gen.AgentRequestableProject{ID: project.ID, Name: project.Name})
		}
		result.Permissions = append(result.Permissions, &gen.AgentRequestablePermission{
			Scope:        string(permission.Entry.Scope),
			Label:        permission.Entry.Label,
			Description:  permission.Entry.Description,
			ResourceType: permission.Entry.ResourceType,
			AllResources: permission.AllResources,
			Projects:     projects,
		})
	}
	for _, request := range pending {
		result.Pending = append(result.Pending, ownAccessRequestView(request))
	}
	return result, nil
}

// RequestAccess files an access request for the enrolled user. Only what the
// device agent offers can be asked for: a listed permission on every resource
// of its kind or on one listed project, or the server a no_access entry of
// getMyConnections names.
func (s *Service) RequestAccess(ctx context.Context, payload *gen.RequestAccessPayload) (*gen.AgentRequestAccessResult, error) {
	authCtx, member, err := s.accessRequestMember(ctx)
	if err != nil {
		return nil, err
	}
	scope := authz.Scope(payload.Scope)
	if err := access.ValidateTemporaryGrantScope(scope); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s cannot be requested; ask an organization administrator to assign it", payload.Scope)
	}
	resourceID := strings.TrimSpace(conv.PtrValOr(payload.ResourceID, ""))

	var resourceName *string
	if entry, ok := authz.ScopeEntryFor(scope); ok && entry.ResourceType == authz.ResourceKindMCP && resourceID != "" {
		name, err := s.requestableServer(ctx, member, scope, resourceID)
		if err != nil {
			return nil, err
		}
		resourceName = name
	} else {
		name, err := s.requestablePermission(ctx, member, scope, resourceID)
		if err != nil {
			return nil, err
		}
		resourceName = name
	}

	request, created, err := s.accessRequests.RequestAccessFromDevice(ctx, authCtx, &accessgen.RequestAccessPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		Scope:          string(scope),
		ResourceID:     conv.PtrEmpty(resourceID),
		ResourceName:   resourceName,
		Message:        payload.Message,
		Duration:       payload.Duration,
		RequestedUntil: payload.RequestedUntil,
	})
	switch {
	case errors.Is(err, access.ErrAccessRequestsOff):
		return nil, oops.E(oops.CodeNotFound, err, "access requests are off")
	case err != nil:
		return nil, fmt.Errorf("file access request: %w", err)
	}
	return &gen.AgentRequestAccessResult{Request: ownAccessRequestView(request), Created: created}, nil
}

// requestablePermissions lists what the member can ask for, by their live
// grants.
func (s *Service) requestablePermissions(ctx context.Context, member agentMember) ([]access.RequestablePermission, error) {
	grants, err := authz.LoadGrants(ctx, s.db, member.organizationID, grantPrincipals(member.principals))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading member grants").LogError(ctx, s.logger)
	}
	permissions, err := s.accessRequests.RequestablePermissions(ctx, member.organizationID, grants)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing requestable permissions").LogError(ctx, s.logger)
	}
	return permissions, nil
}

// requestablePermission checks that listRequestableAccess offers scope, on
// every resource when resourceID is empty or on that project otherwise, and
// returns the project's name.
func (s *Service) requestablePermission(ctx context.Context, member agentMember, scope authz.Scope, resourceID string) (*string, error) {
	permissions, err := s.requestablePermissions(ctx, member)
	if err != nil {
		return nil, err
	}
	idx := slices.IndexFunc(permissions, func(p access.RequestablePermission) bool { return p.Entry.Scope == scope })
	if idx < 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "you already have %s", scope)
	}
	permission := permissions[idx]
	if resourceID == "" {
		if !permission.AllResources {
			return nil, oops.E(oops.CodeBadRequest, nil, "you already have %s on every resource", scope)
		}
		return nil, nil
	}
	projectIdx := slices.IndexFunc(permission.Projects, func(p access.RequestableProject) bool { return p.ID == resourceID })
	if projectIdx < 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s cannot be requested on this resource from the device agent", scope)
	}
	return new(permission.Projects[projectIdx].Name), nil
}

// requestableServer checks that a server delivered to the member reports
// no_access with a request for exactly scope on resourceID, and returns the
// server's name.
func (s *Service) requestableServer(ctx context.Context, member agentMember, scope authz.Scope, resourceID string) (*string, error) {
	refused := oops.E(oops.CodeBadRequest, nil, "%s cannot be requested on this server from the device agent", scope)
	serverRows, err := s.memberServers(ctx, member)
	if err != nil {
		return nil, err
	}
	grants, err := authz.LoadGrants(ctx, s.db, member.organizationID, grantPrincipals(member.principals))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading member grants").LogError(ctx, s.logger)
	}
	principal := platformmcp.Principal{UserID: member.userID, OrganizationID: member.organizationID, ConnectionID: "", Generation: "", ClientID: "", Surface: ""}
	for _, row := range serverRows {
		entry, ok := s.serverConnection(ctx, principal, grants, row, *pluginServerID(row))
		if !ok || entry.RequestAccess == nil {
			continue
		}
		target := entry.RequestAccess
		if target.Scope == string(scope) && target.ResourceID == resourceID {
			return target.ResourceName, nil
		}
	}
	s.logger.InfoContext(ctx, "device agent asked for access to a server it was not offered", attr.SlogOrganizationID(member.organizationID))
	return nil, refused
}

func ownAccessRequestView(request access.OwnAccessRequest) *gen.AgentOwnAccessRequest {
	return &gen.AgentOwnAccessRequest{
		ID:              request.Request.ID,
		Scope:           request.Request.Scope,
		PermissionLabel: request.PermissionLabel,
		ResourceID:      conv.PtrEmpty(request.ResourceID),
		ResourceName:    request.Request.ResourceName,
		CreatedAt:       request.Request.CreatedAt,
		ExpiresAt:       request.Request.ExpiresAt,
	}
}

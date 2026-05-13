package access

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var ErrRoleNotFound = errors.New("role not found")

var validRoleNamePattern = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)

type RoleProvider interface {
	CreateRole(ctx context.Context, orgID string, opts workos.CreateRoleOpts) (*workos.Role, error)
	UpdateRole(ctx context.Context, orgID string, roleSlug string, opts workos.UpdateRoleOpts) (*workos.Role, error)
	DeleteRole(ctx context.Context, orgID string, roleSlug string) error
	UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*workos.Member, error)
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

// RoleManager owns role reads from local records and role writes through WorkOS.
type RoleManager struct {
	db     *pgxpool.Pool
	logger *slog.Logger
	roles  RoleProvider
	authz  *authz.Engine
}

// NewRoleManager wires the role manager to the local DB, the WorkOS role client, and the authz engine.
func NewRoleManager(logger *slog.Logger, db *pgxpool.Pool, roles RoleProvider, authzEngine *authz.Engine) *RoleManager {
	return &RoleManager{
		db:     db,
		logger: logger.With(attr.SlogComponent("access.role_manager")),
		roles:  roles,
		authz:  authzEngine,
	}
}

// ListRoles returns active roles for an organization from local records and enriches them with local grants and member counts.
func (r *RoleManager) ListRoles(ctx context.Context, gramOrgID string) (*gen.ListRolesResult, error) {
	rows, err := repo.New(r.db).ListActiveOrganizationRoles(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles").Log(ctx, r.logger)
	}

	memberCounts, err := r.memberCounts(ctx, gramOrgID)
	if err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	roles := make([]*gen.Role, 0, len(rows))
	for _, row := range rows {
		role, err := r.roleViewFromLocalRole(ctx, gramOrgID, localRoleFromActiveRow(row), memberCounts[row.WorkosSlug])
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}

	return &gen.ListRolesResult{Roles: roles}, nil
}

// GetRoleByID returns one active role from the local role table with local grants and member count.
func (r *RoleManager) GetRoleByID(ctx context.Context, gramOrgID, id string) (*gen.Role, error) {
	role, err := r.getLocalRoleByID(ctx, gramOrgID, id)
	if err != nil {
		return nil, err
	}

	memberCounts, err := r.memberCounts(ctx, gramOrgID)
	if err != nil {
		return nil, err
	}

	return r.roleViewFromLocalRole(ctx, gramOrgID, role, memberCounts[role.Slug])
}

type localRoleAssignment struct {
	UserID       string
	WorkosUserID string
	MembershipID string
	RoleSlug     string
	CreatedAt    string
}

// ListMembers returns locally known organization members with role IDs resolved from local role assignments.
func (r *RoleManager) ListMembers(ctx context.Context, gramOrgID string) (*gen.ListMembersResult, error) {
	roleRows, err := repo.New(r.db).ListActiveOrganizationRoles(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	roles := make(map[string]string, len(roleRows))
	for _, row := range roleRows {
		roles[row.WorkosSlug] = row.ID.String()
	}

	assignmentRows, err := repo.New(r.db).ListOrganizationRoleAssignmentsForOrg(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	assignments := make([]localRoleAssignment, 0, len(assignmentRows))
	for _, row := range assignmentRows {
		assignments = append(assignments, localRoleAssignment{
			UserID:       conv.FromPGTextOrEmpty[string](row.UserID),
			WorkosUserID: row.WorkosUserID,
			MembershipID: conv.FromPGTextOrEmpty[string](row.WorkosMembershipID),
			RoleSlug:     row.RoleSlug,
			CreatedAt:    conv.FromPGTimestamptz(row.CreatedAt),
		})
	}

	userIDs := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.UserID != "" {
			userIDs = append(userIDs, assignment.UserID)
		}
	}
	localRows, err := usersrepo.New(r.db).GetUsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve users by ids").Log(ctx, r.logger)
	}
	localUsers := make(map[string]usersrepo.User, len(localRows))
	for _, u := range localRows {
		localUsers[u.ID] = u
	}

	result := make([]*gen.AccessMember, 0, len(assignments))
	for _, assignment := range assignments {
		user, ok := localUsers[assignment.UserID]
		if !ok {
			continue
		}

		result = append(result, &gen.AccessMember{
			ID:       user.ID,
			Name:     conv.Default(user.DisplayName, user.Email),
			Email:    user.Email,
			PhotoURL: conv.FromPGText[string](user.PhotoUrl),
			RoleID:   roles[assignment.RoleSlug],
			JoinedAt: assignment.CreatedAt,
		})
	}

	return &gen.ListMembersResult{Members: result}, nil
}

type roleCreateResult struct {
	Role *gen.Role
	Slug string
}

// CreateRole creates a WorkOS role, upserts the local role record, syncs local grants, and optionally assigns members.
func (r *RoleManager) CreateRole(ctx context.Context, gramOrgID, workosOrgID string, payload *gen.CreateRolePayload) (roleCreateResult, error) {
	roleSlug, err := slugify(payload.Name)
	if err != nil {
		return roleCreateResult{}, err
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSlug(roleSlug))

	wr, err := r.roles.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{
		Name:        payload.Name,
		Slug:        roleSlug,
		Description: payload.Description,
	})
	var apiErr *workos.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.StatusCode == 409:
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "create role in workos").Log(ctx, r.logger)
	case err != nil:
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "create role in workos").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleID(wr.ID))
	if err := repo.New(r.db).UpsertOrganizationRole(ctx, organizationRoleParams(gramOrgID, *wr)); err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		_ = oops.E(oops.CodeUnexpected, err, "upsert local role record after workos write").Log(ctx, r.logger)
	}

	if err := authz.SyncGrants(ctx, r.logger, r.db, gramOrgID, wr.Slug, roleGrantPayloads(payload.Grants)); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "sync grants for created role").Log(ctx, r.logger)
	}

	assignedCount := 0
	if len(payload.MemberIds) > 0 {
		assignedCount, err = r.assignMembersToRole(ctx, gramOrgID, wr.Slug, payload.MemberIds)
		if err != nil {
			return roleCreateResult{}, err
		}
	}

	role, err := r.roleViewFromLocalRole(ctx, gramOrgID, localRoleFromWorkOS(*wr), assignedCount)
	if err != nil {
		return roleCreateResult{}, err
	}

	return roleCreateResult{Role: role, Slug: wr.Slug}, nil
}

type localRole struct {
	ID          string
	Name        string
	Slug        string
	Description string
	CreatedAt   string
	UpdatedAt   string
}

type roleUpdateResult struct {
	Before *gen.Role
	After  *gen.Role
	Role   localRole
}

// UpdateRole updates an existing role and optionally replaces its assigned members.
func (r *RoleManager) UpdateRole(ctx context.Context, gramOrgID, workosOrgID string, payload *gen.UpdateRolePayload) (roleUpdateResult, error) {
	currentRole, err := r.getLocalRoleByID(ctx, gramOrgID, payload.ID)
	if err != nil {
		return roleUpdateResult{}, err
	}
	memberCountsBefore, err := r.memberCounts(ctx, gramOrgID)
	if err != nil {
		return roleUpdateResult{}, err
	}
	existingRole, err := r.roleViewFromLocalRole(ctx, gramOrgID, currentRole, memberCountsBefore[currentRole.Slug])
	if err != nil {
		return roleUpdateResult{}, err
	}

	sysRole := isSystemRole(currentRole.Slug)
	if sysRole && (payload.Name != nil || payload.Description != nil || payload.Grants != nil) {
		return roleUpdateResult{}, oops.E(oops.CodeBadRequest, nil, "system role properties cannot be updated, only member assignment is allowed").Log(ctx, r.logger)
	}
	if sysRole && payload.MemberIds == nil {
		return roleUpdateResult{}, oops.E(oops.CodeBadRequest, nil, "system role update requires member_ids").Log(ctx, r.logger)
	}
	if payload.Name != nil {
		if _, err := slugify(*payload.Name); err != nil {
			return roleUpdateResult{}, err
		}
	}

	updatedRole := currentRole
	if !sysRole {
		wRole, err := r.roles.UpdateRole(ctx, workosOrgID, currentRole.Slug, workos.UpdateRoleOpts{
			Name:        payload.Name,
			Description: payload.Description,
		})
		if err != nil {
			return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "update role in workos").Log(ctx, r.logger)
		}
		updatedRole = localRoleFromWorkOS(*wRole)
		if err := repo.New(r.db).UpsertOrganizationRole(ctx, organizationRoleParams(gramOrgID, *wRole)); err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			_ = oops.E(oops.CodeUnexpected, err, "upsert local role record after workos write").Log(ctx, r.logger)
		}

		if payload.Grants != nil {
			if err := authz.SyncGrants(ctx, r.logger, r.db, gramOrgID, currentRole.Slug, roleGrantPayloads(payload.Grants)); err != nil {
				return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "sync grants for updated role").Log(ctx, r.logger)
			}
		}
	}

	if payload.MemberIds != nil {
		if _, err := r.assignMembersToRole(ctx, gramOrgID, currentRole.Slug, payload.MemberIds); err != nil {
			return roleUpdateResult{}, err
		}
	}

	memberCounts, err := r.memberCounts(ctx, gramOrgID)
	if err != nil {
		return roleUpdateResult{}, err
	}
	updatedRoleView, err := r.roleViewFromLocalRole(ctx, gramOrgID, updatedRole, memberCounts[updatedRole.Slug])
	if err != nil {
		return roleUpdateResult{}, err
	}

	return roleUpdateResult{Before: existingRole, After: updatedRoleView, Role: updatedRole}, nil
}

// DeleteRole deletes a custom role after moving assigned members to the default member role.
// Side effects: reads local records; writes WorkOS membership reassignments before deleting the WorkOS role; upserts local assignment records, marks the local role deleted, invalidates role caches, and deletes local role grants.
func (r *RoleManager) DeleteRole(ctx context.Context, gramOrgID, workosOrgID, roleID string) (localRole, error) {
	currentRole, err := r.getLocalRoleByID(ctx, gramOrgID, roleID)
	if err != nil {
		return localRole{}, err
	}
	if isSystemRole(currentRole.Slug) {
		return localRole{}, oops.E(oops.CodeBadRequest, nil, "system roles cannot be deleted").Log(ctx, r.logger)
	}

	rows, err := repo.New(r.db).ListOrganizationRoleAssignmentsForOrg(ctx, gramOrgID)
	if err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))

	reassigned := false
	for _, row := range rows {
		if row.RoleSlug != currentRole.Slug {
			continue
		}
		membershipID := conv.FromPGTextOrEmpty[string](row.WorkosMembershipID)
		if _, err := r.roles.UpdateMemberRole(ctx, membershipID, authz.SystemRoleMember); err != nil {
			if reassigned {
				r.authz.InvalidateAllRoleCaches(ctx, gramOrgID)
			}
			return localRole{}, oops.E(oops.CodeUnexpected, err, "reassign member to default role").Log(ctx, r.logger)
		}
		if row.WorkosUserID != "" {
			if err := repo.New(r.db).ReplaceOrganizationRoleAssignment(ctx, replaceRoleAssignmentParams(gramOrgID, row.WorkosUserID, authz.SystemRoleMember, "", membershipID)); err != nil {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				_ = oops.E(oops.CodeUnexpected, err, "upsert local role assignment record after workos write").Log(ctx, r.logger)
			}
		}
		reassigned = true
	}
	if reassigned {
		r.authz.InvalidateAllRoleCaches(ctx, gramOrgID)
	}

	if err := r.roles.DeleteRole(ctx, workosOrgID, currentRole.Slug); err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "delete role in workos").Log(ctx, r.logger)
	}
	if _, err := repo.New(r.db).MarkOrganizationRoleDeleted(ctx, repo.MarkOrganizationRoleDeletedParams{
		OrganizationID:    gramOrgID,
		WorkosSlug:        currentRole.Slug,
		WorkosDeletedAt:   conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	}); err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		_ = oops.E(oops.CodeUnexpected, err, "mark local role record deleted after workos write").Log(ctx, r.logger)
	}

	if _, err := repo.New(r.db).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: gramOrgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, currentRole.Slug),
	}); err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "delete grants for deleted role").Log(ctx, r.logger)
	}

	return currentRole, nil
}

type memberRoleUpdateContext struct {
	RoleSlug     string
	MembershipID string
	WorkosUserID string
	UserID       string
	Before       *gen.AccessMember
	After        *gen.AccessMember
}

// UpdateMemberRole changes one member's role assignment.
// Side effects: reads local user, role, and local assignment records; writes the WorkOS membership first, upserts the local assignment record, and invalidates that member's role cache.
func (r *RoleManager) UpdateMemberRole(ctx context.Context, gramOrgID, userID, roleID string) (memberRoleUpdateContext, error) {
	role, err := r.getLocalRoleByID(ctx, gramOrgID, roleID)
	if err != nil {
		return memberRoleUpdateContext{}, err
	}

	connectedUser, err := connectedUser(ctx, r.db, gramOrgID, userID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member has not joined this organization").Log(ctx, r.logger)
	case err != nil:
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "load connected user").Log(ctx, r.logger)
	}
	if !connectedUser.WorkosID.Valid || connectedUser.WorkosID.String == "" {
		return memberRoleUpdateContext{}, oops.E(oops.CodeBadRequest, nil, "member is not linked to WorkOS").Log(ctx, r.logger)
	}

	roleRows, err := repo.New(r.db).ListActiveOrganizationRoles(ctx, gramOrgID)
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "list roles").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	roleIDBySlug := make(map[string]string, len(roleRows))
	for _, row := range roleRows {
		roleIDBySlug[row.WorkosSlug] = row.ID.String()
	}

	assignmentRows, err := repo.New(r.db).ListOrganizationRoleAssignmentsForOrg(ctx, gramOrgID)
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	var existing localRoleAssignment
	for _, row := range assignmentRows {
		if row.WorkosUserID == connectedUser.WorkosID.String {
			existing = localRoleAssignment{
				UserID:       conv.FromPGTextOrEmpty[string](row.UserID),
				WorkosUserID: row.WorkosUserID,
				MembershipID: conv.FromPGTextOrEmpty[string](row.WorkosMembershipID),
				RoleSlug:     row.RoleSlug,
				CreatedAt:    conv.FromPGTimestamptz(row.CreatedAt),
			}
			break
		}
	}
	if existing.MembershipID == "" {
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member not found").Log(ctx, r.logger)
	}

	updatedMember, err := r.roles.UpdateMemberRole(ctx, existing.MembershipID, role.Slug)
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "update member role in workos").Log(ctx, r.logger)
	}
	if updatedMember.UserID != "" && role.Slug != "" {
		if err := repo.New(r.db).ReplaceOrganizationRoleAssignment(ctx, replaceRoleAssignmentParams(gramOrgID, updatedMember.UserID, role.Slug, connectedUser.ID, updatedMember.ID)); err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			_ = oops.E(oops.CodeUnexpected, err, "upsert local role assignment record after workos write").Log(ctx, r.logger)
		}
	}
	r.authz.InvalidateRoleCache(ctx, userID, gramOrgID)

	memberName := conv.Default(connectedUser.DisplayName, connectedUser.Email)
	return memberRoleUpdateContext{
		RoleSlug:     role.Slug,
		MembershipID: existing.MembershipID,
		WorkosUserID: connectedUser.WorkosID.String,
		UserID:       connectedUser.ID,
		Before: &gen.AccessMember{
			ID:       connectedUser.ID,
			Name:     memberName,
			Email:    connectedUser.Email,
			PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
			RoleID:   roleIDBySlug[existing.RoleSlug],
			JoinedAt: existing.CreatedAt,
		},
		After: &gen.AccessMember{
			ID:       connectedUser.ID,
			Name:     memberName,
			Email:    connectedUser.Email,
			PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
			RoleID:   roleID,
			JoinedAt: existing.CreatedAt,
		},
	}, nil
}

// MemberRoleSlugs returns local role slugs assigned to a WorkOS user inside an organization.
// Side effects: reads Postgres local assignment records; does not call WorkOS.
func (r *RoleManager) MemberRoleSlugs(ctx context.Context, gramOrgID, workosUserID string) ([]string, error) {
	if workosUserID == "" {
		return nil, nil
	}

	roleSlugs, err := repo.New(r.db).ListMemberRoleSlugsByWorkosUser(ctx, repo.ListMemberRoleSlugsByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   workosUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list member roles").Log(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	return roleSlugs, nil
}

// getLocalRoleByID loads one local role record by Gram role ID.
// Side effects: reads Postgres local role records; does not call WorkOS.
func (r *RoleManager) getLocalRoleByID(ctx context.Context, gramOrgID, id string) (localRole, error) {
	roleID, err := uuid.Parse(id)
	if err != nil {
		return localRole{}, oops.E(oops.CodeBadRequest, err, "invalid role ID").Log(ctx, r.logger)
	}

	row, err := repo.New(r.db).GetOrganizationRoleByID(ctx, repo.GetOrganizationRoleByIDParams{
		ID:             roleID,
		OrganizationID: gramOrgID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return localRole{}, oops.E(oops.CodeNotFound, ErrRoleNotFound, "role not found").Log(ctx, r.logger)
	case err != nil:
		return localRole{}, oops.E(oops.CodeUnexpected, err, "get role").Log(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	return localRoleFromRoleRow(row), nil
}

type memberAssignmentTarget struct {
	UserID       string
	WorkosUserID string
	MembershipID string
}

// memberAssignmentTargets resolves Gram user IDs to WorkOS membership IDs using local user and local assignment records.
func (r *RoleManager) memberAssignmentTargets(ctx context.Context, gramOrgID string, memberIDs []string) ([]memberAssignmentTarget, error) {
	if len(memberIDs) == 0 {
		return nil, nil
	}

	users, err := usersrepo.New(r.db).GetUsersByIDs(ctx, memberIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve users by ids").Log(ctx, r.logger)
	}
	workosByGramID := make(map[string]string, len(users))
	for _, user := range users {
		if user.WorkosID.Valid && user.WorkosID.String != "" {
			workosByGramID[user.ID] = user.WorkosID.String
		}
	}

	assignmentRows, err := repo.New(r.db).ListOrganizationRoleAssignmentsForOrg(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	membershipByWorkosID := make(map[string]string, len(assignmentRows))
	for _, row := range assignmentRows {
		membershipByWorkosID[row.WorkosUserID] = conv.FromPGTextOrEmpty[string](row.WorkosMembershipID)
	}

	targets := make([]memberAssignmentTarget, 0, len(memberIDs))
	for _, gramID := range memberIDs {
		workosID, ok := workosByGramID[gramID]
		if !ok {
			continue
		}
		membershipID, ok := membershipByWorkosID[workosID]
		if !ok {
			continue
		}
		targets = append(targets, memberAssignmentTarget{
			UserID:       gramID,
			WorkosUserID: workosID,
			MembershipID: membershipID,
		})
	}

	return targets, nil
}

// assignMembersToRole moves each requested member to the given WorkOS role and mirrors the result locally.
// Side effects: reads local users and assignments, writes WorkOS memberships, upserts local assignment records, and invalidates org role caches when any member is assigned.
func (r *RoleManager) assignMembersToRole(ctx context.Context, gramOrgID, roleSlug string, memberIDs []string) (int, error) {
	targets, err := r.memberAssignmentTargets(ctx, gramOrgID, memberIDs)
	if err != nil {
		return 0, err
	}

	assignedCount := 0
	for _, target := range targets {
		if _, err := r.roles.UpdateMemberRole(ctx, target.MembershipID, roleSlug); err != nil {
			return 0, oops.E(oops.CodeUnexpected, err, "assign members to role").Log(ctx, r.logger)
		}
		if target.WorkosUserID != "" && roleSlug != "" {
			if err := repo.New(r.db).ReplaceOrganizationRoleAssignment(ctx, replaceRoleAssignmentParams(gramOrgID, target.WorkosUserID, roleSlug, target.UserID, target.MembershipID)); err != nil {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				_ = oops.E(oops.CodeUnexpected, err, "upsert local role assignment record after workos write").Log(ctx, r.logger)
			}
		}
		assignedCount++
	}
	if assignedCount > 0 {
		r.authz.InvalidateAllRoleCaches(ctx, gramOrgID)
	}

	return assignedCount, nil
}

// memberCounts returns the number of locally connected members per role slug.
// Side effects: reads Postgres local assignment records; does not call WorkOS.
func (r *RoleManager) memberCounts(ctx context.Context, gramOrgID string) (map[string]int, error) {
	rows, err := repo.New(r.db).CountMembersByRoleForOrg(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count local members by role").Log(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.RoleSlug] = int(row.MemberCount)
	}
	return counts, nil
}

// roleViewFromLocalRole converts a local role record into the public API role view and attaches local grants.
// Side effects: reads Postgres grants; does not call WorkOS.
func (r *RoleManager) roleViewFromLocalRole(ctx context.Context, organizationID string, role localRole, memberCount int) (*gen.Role, error) {
	grants, err := authz.GrantsForRole(ctx, r.logger, r.db, organizationID, role.Slug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load role grants").Log(ctx, r.logger)
	}
	genGrants := make([]*gen.RoleGrant, 0, len(grants))
	for _, g := range grants {
		genGrants = append(genGrants, scopedGrantToGenRoleGrant(g))
	}

	return &gen.Role{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    isSystemRole(role.Slug),
		Grants:      genGrants,
		MemberCount: memberCount,
		CreatedAt:   conv.Default(role.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:   conv.Default(role.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}, nil
}

// localRoleFromActiveRow converts a sqlc active-role row into the manager's internal local role record shape.
// Side effects: none.
func localRoleFromActiveRow(row repo.ListActiveOrganizationRolesRow) localRole {
	return localRole{
		ID:          row.ID.String(),
		Name:        row.WorkosName,
		Slug:        row.WorkosSlug,
		Description: conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:   conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:   conv.FromPGTimestamptz(row.WorkosUpdatedAt),
	}
}

// localRoleFromRoleRow converts a sqlc role lookup row into the manager's internal local role record shape.
// Side effects: none.
func localRoleFromRoleRow(row repo.GetOrganizationRoleByIDRow) localRole {
	return localRole{
		ID:          row.ID.String(),
		Name:        row.WorkosName,
		Slug:        row.WorkosSlug,
		Description: conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:   conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:   conv.FromPGTimestamptz(row.WorkosUpdatedAt),
	}
}

// localRoleFromWorkOS converts a WorkOS role response into the manager's internal local role record shape.
// Side effects: none.
func localRoleFromWorkOS(role workos.Role) localRole {
	return localRole{
		ID:          role.ID,
		Name:        role.Name,
		Slug:        role.Slug,
		Description: role.Description,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}

// organizationRoleParams builds the SQL parameters for storing a local role record from a WorkOS write response.
// Side effects: reads the clock for updated_at and possibly created_at fallback.
func organizationRoleParams(gramOrgID string, role workos.Role) repo.UpsertOrganizationRoleParams {
	return repo.UpsertOrganizationRoleParams{
		OrganizationID:    gramOrgID,
		WorkosSlug:        role.Slug,
		WorkosName:        role.Name,
		WorkosDescription: conv.ToPGTextEmpty(role.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(workosTimeOrNow(role.CreatedAt)),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	}
}

// replaceRoleAssignmentParams builds SQL parameters for storing the authoritative local role assignment after a WorkOS write.
// Side effects: reads the clock for updated_at.
func replaceRoleAssignmentParams(gramOrgID, workosUserID, roleSlug, userID, membershipID string) repo.ReplaceOrganizationRoleAssignmentParams {
	return repo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     gramOrgID,
		WorkosUserID:       workosUserID,
		WorkosRoleSlug:     roleSlug,
		UserID:             conv.ToPGTextEmpty(userID),
		WorkosMembershipID: conv.ToPGTextEmpty(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	}
}

// workosTimeOrNow parses a WorkOS RFC3339 timestamp or returns the current UTC time when WorkOS omits or malforms it.
// Side effects: reads the clock only when a fallback is needed.
func workosTimeOrNow(value string) time.Time {
	if value == "" {
		return time.Now().UTC()
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Now().UTC()
	}
	return t.UTC()
}

// slugify validates a role name and turns it into Gram's WorkOS role slug format.
// Side effects: none.
func slugify(name string) (string, error) {
	slug := conv.ToSlug(strings.ReplaceAll(name, "_", " "))
	if slug == "" {
		return "", oops.E(oops.CodeBadRequest, nil, "role name must contain at least one letter or digit")
	}
	if !validRoleNamePattern.MatchString(name) {
		return "", oops.E(oops.CodeBadRequest, nil, "role name contains invalid characters")
	}
	if !strings.HasPrefix(slug, "org-") {
		slug = "org-" + slug
	}

	return slug, nil
}

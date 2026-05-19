package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var ErrRoleNotFound = errors.New("role not found")

var validRoleNamePattern = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)

const workOSSyncAttempts = 3

type RoleProvider interface {
	CreateRole(ctx context.Context, orgID string, opts workos.CreateRoleOpts) (*workos.Role, error)
	UpdateRole(ctx context.Context, orgID string, roleSlug string, opts workos.UpdateRoleOpts) (*workos.Role, error)
	DeleteRole(ctx context.Context, orgID string, roleSlug string) error
	UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*workos.Member, error)
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

// RoleManager owns role reads and writes against local records, then syncs successful writes to WorkOS.
type RoleManager struct {
	db     *pgxpool.Pool
	logger *slog.Logger
	roles  RoleProvider
	audit  *audit.Logger
}

// NewRoleManager wires the role manager to the local DB, the WorkOS role client, and the audit logger.
func NewRoleManager(logger *slog.Logger, db *pgxpool.Pool, roles RoleProvider, auditLogger *audit.Logger) *RoleManager {
	return &RoleManager{
		db:     db,
		logger: logger.With(attr.SlogComponent("access.role_manager")),
		roles:  roles,
		audit:  auditLogger,
	}
}

// ListRoles returns active roles for an organization from local records and enriches them with local grants and member counts.
func (r *RoleManager) ListRoles(ctx context.Context, gramOrgID string) (*gen.ListRolesResult, error) {
	rows, err := repo.New(r.db).ListActiveOrganizationRoles(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles").Log(ctx, r.logger)
	}

	roles := make([]*gen.Role, 0, len(rows))
	for _, row := range rows {
		role, err := r.roleViewFromLocalRole(ctx, gramOrgID, localRoleFromActiveRow(row))
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

	return r.roleViewFromLocalRole(ctx, gramOrgID, role)
}

// ListMembers returns locally known organization members with role IDs resolved from local role assignments.
func (r *RoleManager) ListMembers(ctx context.Context, gramOrgID string) (*gen.ListMembersResult, error) {
	rows, err := repo.New(r.db).ListAccessMembers(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}

	result := make([]*gen.AccessMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, &gen.AccessMember{
			ID:       row.ID,
			Name:     conv.Default(row.DisplayName, row.Email),
			Email:    row.Email,
			PhotoURL: conv.FromPGText[string](row.PhotoUrl),
			RoleID:   row.RoleID.String(),
			JoinedAt: conv.FromPGTimestamptz(row.JoinedAt),
		})
	}

	return &gen.ListMembersResult{Members: result}, nil
}

type roleCreateResult struct {
	Role *gen.Role
	Slug string
}

type workosSync func(context.Context)

type accessAuditActor struct {
	Principal   urn.Principal
	DisplayName *string
	Slug        *string
}

// CreateRole creates the local role, grants, optional assignments, and audit entry atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) CreateRole(ctx context.Context, gramOrgID, workosOrgID string, actor accessAuditActor, payload *gen.CreateRolePayload) (roleCreateResult, error) {
	roleSlug, err := slugify(payload.Name)
	if err != nil {
		return roleCreateResult{}, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").Log(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	now := time.Now().UTC().Format(time.RFC3339)
	createdRow, err := repo.New(tx).UpsertOrganizationRole(ctx, organizationRoleParams(gramOrgID, workos.Role{
		ID:          "",
		Type:        "",
		Name:        payload.Name,
		Slug:        roleSlug,
		Description: payload.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	if err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "upsert local role record").Log(ctx, r.logger)
	}
	createdRole := localRoleFromUpsertRow(createdRow)
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleID(createdRole.ID))

	if _, err := authz.SyncGrantsTx(ctx, tx, gramOrgID, roleSlug, createdRole.PrincipalURN, roleGrantPayloads(payload.Grants)); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "sync grants for created role").Log(ctx, r.logger)
	}

	workosSyncs := []workosSync{func(ctx context.Context) {
		r.syncWorkOS(ctx, "create role in workos", func() error {
			_, err := r.roles.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{
				Name:        payload.Name,
				Slug:        roleSlug,
				Description: payload.Description,
			})
			var apiErr *workos.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 409 {
				return nil
			}
			if err == nil {
				return nil
			}
			return fmt.Errorf("create role in workos: %w", err)
		})
	}}

	if len(payload.MemberIds) > 0 {
		var memberSyncs []workosSync
		if _, memberSyncs, err = r.assignMembersToRoleTx(ctx, tx, gramOrgID, roleSlug, payload.MemberIds); err != nil {
			return roleCreateResult{}, err
		}
		workosSyncs = append(workosSyncs, memberSyncs...)
		createdRole, err = r.getLocalRoleBySlugTx(ctx, tx, gramOrgID, roleSlug)
		if err != nil {
			return roleCreateResult{}, err
		}
	}

	if err := r.audit.LogAccessRoleCreate(ctx, tx, audit.LogAccessRoleCreateEvent{
		OrganizationID:   gramOrgID,
		Actor:            actor.Principal,
		ActorDisplayName: actor.DisplayName,
		ActorSlug:        actor.Slug,
		RoleID:           createdRole.ID,
		RoleName:         createdRole.Name,
		RoleSlug:         createdRole.Slug,
	}); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "log access role creation").Log(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").Log(ctx, r.logger)
	}

	r.runWorkOSSyncs(ctx, workosSyncs)

	role, err := r.roleViewFromLocalRole(ctx, gramOrgID, createdRole)
	if err != nil {
		return roleCreateResult{}, err
	}

	return roleCreateResult{Role: role, Slug: roleSlug}, nil
}

type localRole struct {
	ID           string
	PrincipalURN string
	Name         string
	Slug         string
	Description  string
	CreatedAt    string
	UpdatedAt    string
	MemberCount  int
}

type roleUpdateResult struct {
	Before *gen.Role
	After  *gen.Role
	Role   localRole
}

// UpdateRole updates an existing local role, optional grants/assignments, and audit entry atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) UpdateRole(ctx context.Context, gramOrgID, workosOrgID string, actor accessAuditActor, payload *gen.UpdateRolePayload) (roleUpdateResult, error) {
	currentRole, err := r.getLocalRoleByID(ctx, gramOrgID, payload.ID)
	if err != nil {
		return roleUpdateResult{}, err
	}
	existingRole, err := r.roleViewFromLocalRole(ctx, gramOrgID, currentRole)
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

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").Log(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	updatedRole := currentRole
	var workosSyncs []workosSync
	var updatedGrants []*gen.RoleGrant
	if !sysRole {
		localRecord := currentRole
		if payload.Name != nil {
			localRecord.Name = *payload.Name
		}
		if payload.Description != nil {
			localRecord.Description = *payload.Description
		}
		localRecord.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		updatedRow, err := repo.New(tx).UpsertOrganizationRole(ctx, organizationRoleParams(gramOrgID, workos.Role{
			ID:          "",
			Type:        "",
			Name:        localRecord.Name,
			Slug:        localRecord.Slug,
			Description: localRecord.Description,
			CreatedAt:   localRecord.CreatedAt,
			UpdatedAt:   localRecord.UpdatedAt,
		}))
		if err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "upsert local role record").Log(ctx, r.logger)
		}
		updatedRole = localRoleFromUpsertRow(updatedRow)

		if payload.Grants != nil {
			syncedGrants, err := authz.SyncGrantsTx(ctx, tx, gramOrgID, currentRole.Slug, currentRole.PrincipalURN, roleGrantPayloads(payload.Grants))
			if err != nil {
				return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "sync grants for updated role").Log(ctx, r.logger)
			}
			updatedGrants = make([]*gen.RoleGrant, 0, len(syncedGrants))
			for _, grant := range syncedGrants {
				updatedGrants = append(updatedGrants, scopedGrantToGenRoleGrant(grant))
			}
		}

		workosSyncs = append(workosSyncs, func(ctx context.Context) {
			r.syncWorkOS(ctx, "update role in workos", func() error {
				_, err := r.roles.UpdateRole(ctx, workosOrgID, currentRole.Slug, workos.UpdateRoleOpts{
					Name:        payload.Name,
					Description: payload.Description,
				})
				if err == nil {
					return nil
				}
				return fmt.Errorf("update role in workos: %w", err)
			})
		})
	}

	if payload.MemberIds != nil {
		var memberSyncs []workosSync
		if _, memberSyncs, err = r.assignMembersToRoleTx(ctx, tx, gramOrgID, currentRole.Slug, payload.MemberIds); err != nil {
			return roleUpdateResult{}, err
		}
		workosSyncs = append(workosSyncs, memberSyncs...)
		updatedRole, err = r.getLocalRoleByIDTx(ctx, tx, gramOrgID, payload.ID)
		if err != nil {
			return roleUpdateResult{}, err
		}
	}

	updatedRoleView := roleViewFromLocalRoleAndGrants(updatedRole, existingRole.Grants)
	if updatedGrants != nil {
		updatedRoleView.Grants = updatedGrants
	}

	if err := r.audit.LogAccessRoleUpdate(ctx, tx, audit.LogAccessRoleUpdateEvent{
		OrganizationID:     gramOrgID,
		Actor:              actor.Principal,
		ActorDisplayName:   actor.DisplayName,
		ActorSlug:          actor.Slug,
		RoleID:             updatedRole.ID,
		RoleName:           updatedRoleView.Name,
		RoleSlug:           updatedRole.Slug,
		RoleSnapshotBefore: existingRole,
		RoleSnapshotAfter:  updatedRoleView,
	}); err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "log access role update").Log(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").Log(ctx, r.logger)
	}

	r.runWorkOSSyncs(ctx, workosSyncs)

	return roleUpdateResult{Before: existingRole, After: updatedRoleView, Role: updatedRole}, nil
}

type roleDeleteResult struct {
	Role localRole
}

// DeleteRole deletes a custom local role, reassignment records, grants, and audit entry atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) DeleteRole(ctx context.Context, gramOrgID, workosOrgID, roleID string, actor accessAuditActor) (roleDeleteResult, error) {
	currentRole, err := r.getLocalRoleByID(ctx, gramOrgID, roleID)
	if err != nil {
		return roleDeleteResult{}, err
	}
	if isSystemRole(currentRole.Slug) {
		return roleDeleteResult{}, oops.E(oops.CodeBadRequest, nil, "system roles cannot be deleted").Log(ctx, r.logger)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").Log(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	rows, err := repo.New(tx).ListOrganizationRoleAssignmentsBySlug(ctx, repo.ListOrganizationRoleAssignmentsBySlugParams{
		OrganizationID: gramOrgID,
		WorkosRoleSlug: currentRole.Slug,
	})
	if err != nil {
		return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))

	var workosSyncs []workosSync
	for _, row := range rows {
		membershipID := conv.FromPGTextOrEmpty[string](row.WorkosMembershipID)
		if row.WorkosUserID != "" {
			replaced, err := repo.New(tx).ReplaceOrganizationRoleAssignment(ctx, replaceRoleAssignmentParams(gramOrgID, row.WorkosUserID, authz.SystemRoleMember, "", membershipID))
			if err != nil {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "upsert local role assignment record").Log(ctx, r.logger)
			}
			if replaced == 0 {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return roleDeleteResult{}, oops.E(oops.CodeUnexpected, nil, "upsert local role assignment record").Log(ctx, r.logger)
			}
		}
		workosSyncs = append(workosSyncs, func(ctx context.Context) {
			r.syncWorkOS(ctx, "reassign member to default role in workos", func() error {
				_, err := r.roles.UpdateMemberRole(ctx, membershipID, authz.SystemRoleMember)
				if err == nil {
					return nil
				}
				return fmt.Errorf("reassign member to default role in workos: %w", err)
			})
		})
	}

	deletedCount, err := repo.New(tx).MarkOrganizationRoleDeletedLocally(ctx, repo.MarkOrganizationRoleDeletedLocallyParams{
		OrganizationID:    gramOrgID,
		WorkosSlug:        currentRole.Slug,
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	if err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "mark local role record deleted").Log(ctx, r.logger)
	}
	if deletedCount == 0 {
		return roleDeleteResult{}, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, r.logger)
	}

	if err := authz.DeleteRoleGrants(ctx, repo.New(tx), gramOrgID, currentRole.Slug, currentRole.PrincipalURN); err != nil {
		return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "delete grants for deleted role").Log(ctx, r.logger)
	}

	if err := r.audit.LogAccessRoleDelete(ctx, tx, audit.LogAccessRoleDeleteEvent{
		OrganizationID:   gramOrgID,
		Actor:            actor.Principal,
		ActorDisplayName: actor.DisplayName,
		ActorSlug:        actor.Slug,
		RoleID:           currentRole.ID,
		RoleName:         currentRole.Name,
		RoleSlug:         currentRole.Slug,
	}); err != nil {
		return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "log access role deletion").Log(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return roleDeleteResult{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").Log(ctx, r.logger)
	}

	workosSyncs = append(workosSyncs, func(ctx context.Context) {
		r.syncWorkOS(ctx, "delete role in workos", func() error {
			if err := r.roles.DeleteRole(ctx, workosOrgID, currentRole.Slug); err != nil {
				return fmt.Errorf("delete role in workos: %w", err)
			}
			return nil
		})
	})

	r.runWorkOSSyncs(ctx, workosSyncs)

	return roleDeleteResult{Role: currentRole}, nil
}

type memberRoleUpdateContext struct {
	RoleSlug     string
	MembershipID string
	WorkosUserID string
	UserID       string
	Before       *gen.AccessMember
	After        *gen.AccessMember
}

// UpdateMemberRole changes one member's local role assignment and audit entry atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) UpdateMemberRole(ctx context.Context, gramOrgID, userID, roleID string, actor accessAuditActor) (memberRoleUpdateContext, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").Log(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	role, err := r.getLocalRoleByIDTx(ctx, tx, gramOrgID, roleID)
	if err != nil {
		return memberRoleUpdateContext{}, err
	}

	connectedUser, err := connectedUser(ctx, tx, gramOrgID, userID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member has not joined this organization").Log(ctx, r.logger)
	case err != nil:
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "load connected user").Log(ctx, r.logger)
	}
	if !connectedUser.WorkosID.Valid || connectedUser.WorkosID.String == "" {
		return memberRoleUpdateContext{}, oops.E(oops.CodeBadRequest, nil, "member is not linked to WorkOS").Log(ctx, r.logger)
	}

	existing, err := repo.New(tx).GetOrganizationRoleAssignmentByWorkosUser(ctx, repo.GetOrganizationRoleAssignmentByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   connectedUser.WorkosID.String,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member not found").Log(ctx, r.logger)
	case err != nil:
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "load member role assignment").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))

	membershipID := conv.FromPGTextOrEmpty[string](existing.WorkosMembershipID)
	if membershipID == "" {
		// WorkOS sync must attach membership IDs before role changes can be propagated upstream.
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member not found").Log(ctx, r.logger)
	}

	if existing.WorkosUserID != "" && role.Slug != "" {
		replaced, err := repo.New(tx).ReplaceOrganizationRoleAssignment(ctx, replaceRoleAssignmentParams(gramOrgID, existing.WorkosUserID, role.Slug, connectedUser.ID, membershipID))
		if err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "upsert local role assignment record").Log(ctx, r.logger)
		}
		if replaced == 0 {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, nil, "upsert local role assignment record").Log(ctx, r.logger)
		}
	}

	memberName := conv.Default(connectedUser.DisplayName, connectedUser.Email)
	result := memberRoleUpdateContext{
		RoleSlug:     role.Slug,
		MembershipID: membershipID,
		WorkosUserID: connectedUser.WorkosID.String,
		UserID:       connectedUser.ID,
		Before: &gen.AccessMember{
			ID:       connectedUser.ID,
			Name:     memberName,
			Email:    connectedUser.Email,
			PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
			RoleID:   existing.RoleID.String(),
			JoinedAt: conv.FromPGTimestamptz(existing.CreatedAt),
		},
		After: &gen.AccessMember{
			ID:       connectedUser.ID,
			Name:     memberName,
			Email:    connectedUser.Email,
			PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
			RoleID:   role.ID,
			JoinedAt: conv.FromPGTimestamptz(existing.CreatedAt),
		},
	}

	if err := r.audit.LogAccessMemberRoleUpdate(ctx, tx, audit.LogAccessMemberRoleUpdateEvent{
		OrganizationID:       gramOrgID,
		Actor:                actor.Principal,
		ActorDisplayName:     actor.DisplayName,
		ActorSlug:            actor.Slug,
		MemberID:             result.UserID,
		MemberName:           result.After.Name,
		MemberEmail:          result.After.Email,
		MemberSnapshotBefore: result.Before,
		MemberSnapshotAfter:  result.After,
	}); err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "log access member role update").Log(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").Log(ctx, r.logger)
	}

	r.runWorkOSSyncs(ctx, []workosSync{
		func(ctx context.Context) {
			r.syncWorkOS(ctx, "update member role in workos", func() error {
				_, err := r.roles.UpdateMemberRole(ctx, membershipID, role.Slug)
				if err == nil {
					return nil
				}
				return fmt.Errorf("update member role in workos: %w", err)
			})
		},
	})

	return result, nil
}

// MemberRolePrincipals returns role slug and principal URN for each role assigned to a WorkOS user in this org.
func (r *RoleManager) MemberRolePrincipals(ctx context.Context, gramOrgID, workosUserID string) ([]repo.ListMemberRolePrincipalsByWorkosUserRow, error) {
	if workosUserID == "" {
		return nil, nil
	}

	rows, err := repo.New(r.db).ListMemberRolePrincipalsByWorkosUser(ctx, repo.ListMemberRolePrincipalsByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   workosUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list member roles").Log(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	return rows, nil
}

// getLocalRoleByID loads one local role record by Gram role ID.
func (r *RoleManager) getLocalRoleByID(ctx context.Context, gramOrgID, id string) (localRole, error) {
	return r.getLocalRoleByIDTx(ctx, r.db, gramOrgID, id)
}

func (r *RoleManager) getLocalRoleByIDTx(ctx context.Context, dbtx repo.DBTX, gramOrgID, id string) (localRole, error) {
	roleID, err := uuid.Parse(id)
	if err != nil {
		return localRole{}, oops.E(oops.CodeBadRequest, err, "invalid role ID").Log(ctx, r.logger)
	}

	row, err := repo.New(dbtx).GetOrganizationRoleByID(ctx, repo.GetOrganizationRoleByIDParams{
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

func (r *RoleManager) getLocalRoleBySlugTx(ctx context.Context, dbtx repo.DBTX, gramOrgID, slug string) (localRole, error) {
	row, err := repo.New(dbtx).GetActiveOrganizationRoleBySlug(ctx, repo.GetActiveOrganizationRoleBySlugParams{
		OrganizationID: gramOrgID,
		WorkosSlug:     slug,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return localRole{}, oops.E(oops.CodeNotFound, ErrRoleNotFound, "role not found").Log(ctx, r.logger)
	case err != nil:
		return localRole{}, oops.E(oops.CodeUnexpected, err, "get role").Log(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	return localRoleFromSlugRow(row), nil
}

type memberAssignmentTarget struct {
	UserID       string
	WorkosUserID string
	MembershipID string
}

// requestedMemberAssignment groups input IDs that resolve to the same WorkOS user.
type requestedMemberAssignment struct {
	InputIDs []string
	UserID   string
}

func (r *RoleManager) memberAssignmentTargetsTx(ctx context.Context, dbtx repo.DBTX, gramOrgID string, memberIDs []string) ([]memberAssignmentTarget, error) {
	if len(memberIDs) == 0 {
		return nil, nil
	}

	users, err := usersrepo.New(dbtx).GetUsersByIDs(ctx, memberIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve users by ids").Log(ctx, r.logger)
	}
	usersByID := make(map[string]usersrepo.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}

	requestedInputs := make(map[string]struct{}, len(memberIDs))
	requestedByWorkosID := make(map[string]requestedMemberAssignment, len(memberIDs))
	workosIDs := make([]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		if _, ok := requestedInputs[id]; ok {
			continue
		}
		requestedInputs[id] = struct{}{}

		workosID := id
		userID := ""
		if user, ok := usersByID[id]; ok {
			userID = user.ID
			if !user.WorkosID.Valid || user.WorkosID.String == "" {
				return nil, oops.E(oops.CodeBadRequest, nil, "member %s is not linked to WorkOS", id).Log(ctx, r.logger)
			}
			workosID = user.WorkosID.String
		}
		if workosID == "" {
			continue
		}
		requested, ok := requestedByWorkosID[workosID]
		if ok {
			requested.InputIDs = append(requested.InputIDs, id)
			if requested.UserID == "" {
				requested.UserID = userID
			}
			requestedByWorkosID[workosID] = requested
			continue
		}
		requestedByWorkosID[workosID] = requestedMemberAssignment{
			InputIDs: []string{id},
			UserID:   userID,
		}
		workosIDs = append(workosIDs, workosID)
	}

	assignmentRows, err := repo.New(dbtx).ListOrganizationRoleAssignmentsByWorkosUsers(ctx, repo.ListOrganizationRoleAssignmentsByWorkosUsersParams{
		OrganizationID: gramOrgID,
		WorkosUserIds:  workosIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members").Log(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	targets := make([]memberAssignmentTarget, 0, len(memberIDs))
	resolvedWorkosIDs := make(map[string]struct{}, len(requestedByWorkosID))
	resolvedInputs := make(map[string]struct{}, len(requestedInputs))
	for _, row := range assignmentRows {
		requested, ok := requestedByWorkosID[row.WorkosUserID]
		if !ok {
			continue
		}
		if _, ok := resolvedWorkosIDs[row.WorkosUserID]; ok {
			continue
		}
		userID := conv.FromPGTextOrEmpty[string](row.UserID)
		if userID == "" {
			userID = requested.UserID
		}
		resolvedWorkosIDs[row.WorkosUserID] = struct{}{}
		for _, inputID := range requested.InputIDs {
			resolvedInputs[inputID] = struct{}{}
		}
		targets = append(targets, memberAssignmentTarget{
			UserID:       userID,
			WorkosUserID: row.WorkosUserID,
			MembershipID: conv.FromPGTextOrEmpty[string](row.WorkosMembershipID),
		})
	}
	if len(resolvedInputs) != len(requestedInputs) {
		return nil, oops.E(oops.CodeBadRequest, nil, "member role assignment not found; wait for WorkOS sync to complete").Log(ctx, r.logger)
	}

	return targets, nil
}

func (r *RoleManager) assignMembersToRoleTx(ctx context.Context, dbtx repo.DBTX, gramOrgID, roleSlug string, memberIDs []string) (int, []workosSync, error) {
	targets, err := r.memberAssignmentTargetsTx(ctx, dbtx, gramOrgID, memberIDs)
	if err != nil {
		return 0, nil, err
	}

	assignedCount := 0
	workosSyncs := make([]workosSync, 0, len(targets))
	for _, target := range targets {
		if target.WorkosUserID != "" && roleSlug != "" {
			replaced, err := repo.New(dbtx).ReplaceOrganizationRoleAssignment(ctx, replaceRoleAssignmentParams(gramOrgID, target.WorkosUserID, roleSlug, target.UserID, target.MembershipID))
			if err != nil {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return 0, nil, oops.E(oops.CodeUnexpected, err, "upsert local role assignment record").Log(ctx, r.logger)
			}
			if replaced == 0 {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return 0, nil, oops.E(oops.CodeUnexpected, nil, "upsert local role assignment record").Log(ctx, r.logger)
			}
		}
		assignedCount++
		membershipID := target.MembershipID
		workosSyncs = append(workosSyncs, func(ctx context.Context) {
			r.syncWorkOS(ctx, "assign member to role in workos", func() error {
				_, err := r.roles.UpdateMemberRole(ctx, membershipID, roleSlug)
				if err == nil {
					return nil
				}
				return fmt.Errorf("assign member to role in workos: %w", err)
			})
		})
	}
	return assignedCount, workosSyncs, nil
}

// runWorkOSSyncs starts best-effort WorkOS writes after the local transaction commits.
func (r *RoleManager) runWorkOSSyncs(ctx context.Context, syncs []workosSync) {
	if len(syncs) == 0 {
		return
	}
	syncCtx := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				r.logger.ErrorContext(syncCtx, "workos sync panic", attr.SlogError(fmt.Errorf("%v", recovered)))
			}
		}()
		for _, syncWorkOS := range syncs {
			syncWorkOS(syncCtx)
		}
	}()
}

// syncWorkOS runs a bounded best-effort WorkOS write after the local database already accepted the change.
func (r *RoleManager) syncWorkOS(ctx context.Context, operation string, fn func() error) {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 100 * time.Millisecond
	exp.MaxInterval = 300 * time.Millisecond
	exp.RandomizationFactor = 0

	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := fn()
		if err == nil {
			return struct{}{}, nil
		}
		if !retryWorkOSError(err) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	}, backoff.WithBackOff(exp), backoff.WithMaxTries(workOSSyncAttempts))
	if err == nil {
		return
	}

	r.logger.ErrorContext(ctx, "workos sync failed: "+operation, attr.SlogError(err))
}

// retryWorkOSError reports whether a WorkOS sync failure is worth retrying in-process.
func retryWorkOSError(err error) bool {
	var apiErr *workos.APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
}

// roleViewFromLocalRole converts a local role record into the public API role view and attaches local grants.
func (r *RoleManager) roleViewFromLocalRole(ctx context.Context, organizationID string, role localRole) (*gen.Role, error) {
	grants, err := authz.GrantsForRole(ctx, r.logger, r.db, organizationID, role.Slug, role.PrincipalURN)
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
		Slug:        role.Slug,
		Description: role.Description,
		IsSystem:    isSystemRole(role.Slug),
		Grants:      genGrants,
		MemberCount: role.MemberCount,
		CreatedAt:   conv.Default(role.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:   conv.Default(role.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}, nil
}

func roleViewFromLocalRoleAndGrants(role localRole, grants []*gen.RoleGrant) *gen.Role {
	return &gen.Role{
		ID:          role.ID,
		Name:        role.Name,
		Slug:        role.Slug,
		Description: role.Description,
		IsSystem:    isSystemRole(role.Slug),
		Grants:      grants,
		MemberCount: role.MemberCount,
		CreatedAt:   conv.Default(role.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:   conv.Default(role.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}
}

// localRoleFromActiveRow converts a sqlc active-role row into the manager's internal local role record shape.
func localRoleFromActiveRow(row repo.ListActiveOrganizationRolesRow) localRole {
	return localRole{
		ID:           row.ID.String(),
		PrincipalURN: row.RoleUrn,
		Name:         row.WorkosName,
		Slug:         row.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
		MemberCount:  int(row.MemberCount),
	}
}

// localRoleFromRoleRow converts a sqlc role lookup row into the manager's internal local role record shape.
func localRoleFromRoleRow(row repo.GetOrganizationRoleByIDRow) localRole {
	return localRole{
		ID:           row.ID.String(),
		PrincipalURN: row.RoleUrn,
		Name:         row.WorkosName,
		Slug:         row.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
		MemberCount:  int(row.MemberCount),
	}
}

func localRoleFromUpsertRow(row repo.UpsertOrganizationRoleRow) localRole {
	return localRole{
		ID:           row.ID.String(),
		PrincipalURN: row.RoleUrn,
		Name:         row.WorkosName,
		Slug:         row.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
		MemberCount:  int(row.MemberCount),
	}
}

// localRoleFromSlugRow converts a sqlc role slug lookup row into the manager's internal local role record shape.
func localRoleFromSlugRow(row repo.GetActiveOrganizationRoleBySlugRow) localRole {
	return localRole{
		ID:           row.ID.String(),
		PrincipalURN: row.RoleUrn,
		Name:         row.WorkosName,
		Slug:         row.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
		MemberCount:  int(row.MemberCount),
	}
}

// organizationRoleParams builds the SQL parameters for storing the authoritative local role record.
func organizationRoleParams(gramOrgID string, role workos.Role) repo.UpsertOrganizationRoleParams {
	return repo.UpsertOrganizationRoleParams{
		OrganizationID:    gramOrgID,
		WorkosSlug:        role.Slug,
		WorkosName:        role.Name,
		WorkosDescription: conv.ToPGTextEmpty(role.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(workosTimeOrNow(role.CreatedAt)),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(workosTimeOrNow(role.UpdatedAt)),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	}
}

// replaceRoleAssignmentParams builds SQL parameters for storing the authoritative local role assignment.
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

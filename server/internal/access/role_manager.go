package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	UpdateMemberRoles(ctx context.Context, membershipID string, roleSlugs []string) (*workos.Member, error)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list roles").LogError(ctx, r.logger)
	}

	roles := make([]*gen.Role, 0, len(rows))
	for _, row := range rows {
		role, err := r.roleViewFromLocalRole(ctx, gramOrgID, localRole{
			ID:           row.ID.String(),
			PrincipalURN: row.RoleUrn,
			Name:         row.WorkosName,
			Slug:         row.WorkosSlug,
			Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
			CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
			UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
			MemberCount:  int(row.MemberCount),
		})
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

// ListMembers returns locally known organization members and includes a role ID only when a local assignment exists.
func (r *RoleManager) ListMembers(ctx context.Context, gramOrgID string) (*gen.ListMembersResult, error) {
	rows, err := repo.New(r.db).ListAccessMembers(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members").LogError(ctx, r.logger)
	}

	memberMap := make(map[string]*gen.AccessMember, len(rows))
	var order []string
	for _, row := range rows {
		m, exists := memberMap[row.ID]
		if !exists {
			m = &gen.AccessMember{
				ID:           row.ID,
				PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, row.ID).String(),
				Name:         conv.Default(row.DisplayName, row.Email),
				Email:        row.Email,
				PhotoURL:     conv.FromPGText[string](row.PhotoUrl),
				RoleIds:      nil,
				JoinedAt:     conv.FromPGTimestamptz(row.JoinedAt),
			}
			memberMap[row.ID] = m
			order = append(order, row.ID)
		}
		if row.RoleID != "" {
			m.RoleIds = append(m.RoleIds, row.RoleID)
		}
	}
	result := make([]*gen.AccessMember, 0, len(order))
	for _, id := range order {
		result = append(result, memberMap[id])
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
}

// CreateRole creates the local role, grants, optional assignments, and audit entry atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) CreateRole(ctx context.Context, gramOrgID, workosOrgID string, actor accessAuditActor, payload *gen.CreateRolePayload) (roleCreateResult, error) {
	roleSlug, err := slugify(payload.Name)
	if err != nil {
		return roleCreateResult{}, err
	}
	grants := roleGrantPayloads(payload.Grants)
	if err := authz.ValidateGrantSurface(authz.GrantSurfaceAccess, grants); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeBadRequest, err, "invalid access role grant: %s", err).LogError(ctx, r.logger)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").LogError(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	now := time.Now().UTC()
	createdRow, err := repo.New(tx).CreateOrganizationRole(ctx, repo.CreateOrganizationRoleParams{
		OrganizationID:    gramOrgID,
		WorkosSlug:        roleSlug,
		WorkosName:        payload.Name,
		WorkosDescription: conv.ToPGTextEmpty(payload.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return roleCreateResult{}, oops.E(oops.CodeConflict, err, "role %q already exists", payload.Name).LogError(ctx, r.logger)
	case errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation:
		return roleCreateResult{}, oops.E(oops.CodeConflict, err, "role %q already exists", payload.Name).LogError(ctx, r.logger)
	case err != nil:
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "create local role record").LogError(ctx, r.logger)
	}

	createdRole := localRole{
		ID:           createdRow.ID.String(),
		PrincipalURN: createdRow.RoleUrn,
		Name:         createdRow.WorkosName,
		Slug:         createdRow.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](createdRow.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(createdRow.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(createdRow.WorkosUpdatedAt),
		MemberCount:  int(createdRow.MemberCount),
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleID(createdRole.ID))

	if _, err := authz.PatchRoleGrantsTx(ctx, tx, gramOrgID, roleSlug, createdRole.PrincipalURN, grants, nil); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "add grants for created role").LogError(ctx, r.logger)
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
		ActorSlug:        nil,
		RoleID:           createdRole.ID,
		RoleName:         createdRole.Name,
		RoleSlug:         createdRole.Slug,
	}); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "log access role creation").LogError(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return roleCreateResult{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").LogError(ctx, r.logger)
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
	// System role names/descriptions are platform-managed and shared globally
	// (global_roles), so they stay immutable. Grants are stored per-org and may
	// be customized, so grant and member changes are allowed below.
	if sysRole && (payload.Name != nil || payload.Description != nil) {
		return roleUpdateResult{}, oops.E(oops.CodeBadRequest, nil, "system role name and description cannot be changed").LogError(ctx, r.logger)
	}
	if payload.Name != nil {
		if _, err := slugify(*payload.Name); err != nil {
			return roleUpdateResult{}, err
		}
	}
	addGrants := roleGrantPayloads(payload.AddGrants)
	removeGrants := roleGrantPayloads(payload.RemoveGrants)
	if err := authz.ValidateGrantSurface(authz.GrantSurfaceAccess, addGrants); err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeBadRequest, err, "invalid access role grant: %s", err).LogError(ctx, r.logger)
	}
	if err := authz.ValidateGrantSurface(authz.GrantSurfaceAccess, removeGrants); err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeBadRequest, err, "invalid access role grant: %s", err).LogError(ctx, r.logger)
	}

	// Lockout guardrail: removing org:admin from the Admin role would lock the
	// org out of administration. Validate against the payload up front so a
	// rejected request never performs grant writes that have to be rolled back.
	if currentRole.Slug == authz.SystemRoleAdmin {
		removingOrgAdmin, addingOrgAdmin := false, false
		for _, g := range removeGrants {
			if g.Scope == string(authz.ScopeOrgAdmin) {
				removingOrgAdmin = true
			}
		}
		for _, g := range addGrants {
			if g.Scope == string(authz.ScopeOrgAdmin) {
				addingOrgAdmin = true
			}
		}
		if removingOrgAdmin && !addingOrgAdmin {
			return roleUpdateResult{}, oops.E(oops.CodeBadRequest, nil, "the Admin role must keep the org:admin permission").LogError(ctx, r.logger)
		}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").LogError(ctx, r.logger)
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
		updatedRow, err := repo.New(tx).UpsertOrganizationRole(ctx, repo.UpsertOrganizationRoleParams{
			OrganizationID:    gramOrgID,
			WorkosSlug:        localRecord.Slug,
			WorkosName:        localRecord.Name,
			WorkosDescription: conv.ToPGTextEmpty(localRecord.Description),
			WorkosCreatedAt:   conv.ToPGTimestamptz(workosTimeOrNow(localRecord.CreatedAt)),
			WorkosUpdatedAt:   conv.ToPGTimestamptz(workosTimeOrNow(localRecord.UpdatedAt)),
			WorkosLastEventID: conv.ToPGTextEmpty(""),
		})
		if err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "upsert local role record").LogError(ctx, r.logger)
		}
		updatedRole = localRole{
			ID:           updatedRow.ID.String(),
			PrincipalURN: updatedRow.RoleUrn,
			Name:         updatedRow.WorkosName,
			Slug:         updatedRow.WorkosSlug,
			Description:  conv.FromPGTextOrEmpty[string](updatedRow.WorkosDescription),
			CreatedAt:    conv.FromPGTimestamptz(updatedRow.WorkosCreatedAt),
			UpdatedAt:    conv.FromPGTimestamptz(updatedRow.WorkosUpdatedAt),
			MemberCount:  int(updatedRow.MemberCount),
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

	// Grants live in the per-org grant store and are patched the same way for
	// custom and system roles. WorkOS only tracks role identity/membership, not
	// gram scopes, so no grant sync to WorkOS is needed.
	if payload.AddGrants != nil || payload.RemoveGrants != nil {
		syncedGrants, err := authz.PatchRoleGrantsTx(ctx, tx, gramOrgID, currentRole.Slug, currentRole.PrincipalURN, addGrants, removeGrants)
		if err != nil {
			return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "patch grants for updated role").LogError(ctx, r.logger)
		}
		updatedGrants = make([]*gen.RoleGrant, 0, len(syncedGrants))
		for _, grant := range syncedGrants {
			updatedGrants = append(updatedGrants, scopedGrantToGenRoleGrant(grant))
		}
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

	updatedRoleView := &gen.Role{
		ID:           updatedRole.ID,
		PrincipalUrn: updatedRole.PrincipalURN,
		Name:         updatedRole.Name,
		Slug:         updatedRole.Slug,
		Description:  updatedRole.Description,
		IsSystem:     isSystemRole(updatedRole.Slug),
		Grants:       existingRole.Grants,
		MemberCount:  updatedRole.MemberCount,
		CreatedAt:    conv.Default(updatedRole.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:    conv.Default(updatedRole.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}
	if updatedGrants != nil {
		updatedRoleView.Grants = updatedGrants
	}

	if err := r.audit.LogAccessRoleUpdate(ctx, tx, audit.LogAccessRoleUpdateEvent{
		OrganizationID:     gramOrgID,
		Actor:              actor.Principal,
		ActorDisplayName:   actor.DisplayName,
		ActorSlug:          nil,
		RoleID:             updatedRole.ID,
		RoleName:           updatedRoleView.Name,
		RoleSlug:           updatedRole.Slug,
		RoleSnapshotBefore: existingRole,
		RoleSnapshotAfter:  updatedRoleView,
	}); err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "log access role update").LogError(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return roleUpdateResult{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").LogError(ctx, r.logger)
	}

	r.runWorkOSSyncs(ctx, workosSyncs)

	return roleUpdateResult{Before: existingRole, After: updatedRoleView, Role: updatedRole}, nil
}

// DeleteRole deletes a custom local role, reassignment records, grants, and audit entry atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) DeleteRole(ctx context.Context, gramOrgID, workosOrgID, roleID string, actor accessAuditActor) (localRole, error) {
	currentRole, err := r.getLocalRoleByID(ctx, gramOrgID, roleID)
	if err != nil {
		return localRole{}, err
	}
	if isSystemRole(currentRole.Slug) {
		return localRole{}, oops.E(oops.CodeBadRequest, nil, "system roles cannot be deleted").LogError(ctx, r.logger)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").LogError(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	rows, err := repo.New(tx).ListOrganizationRoleAssignmentsBySlug(ctx, repo.ListOrganizationRoleAssignmentsBySlugParams{
		OrganizationID: gramOrgID,
		WorkosRoleSlug: currentRole.Slug,
	})
	if err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "list members").LogError(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))

	var workosSyncs []workosSync
	for _, row := range rows {
		membershipID := conv.FromPGTextOrEmpty[string](row.WorkosMembershipID)
		if row.WorkosUserID != "" {
			// Soft-delete only this role's assignment; other roles for the user are preserved.
			if _, err := repo.New(tx).SoftDeleteRoleAssignmentsBySlug(ctx, repo.SoftDeleteRoleAssignmentsBySlugParams{
				OrganizationID: gramOrgID,
				WorkosUserID:   row.WorkosUserID,
				WorkosRoleSlug: currentRole.Slug,
			}); err != nil {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return localRole{}, oops.E(oops.CodeUnexpected, err, "soft-delete role assignment for deleted role").LogError(ctx, r.logger)
			}

			// Collect remaining role slugs for WorkOS sync.
			remaining, err := repo.New(tx).ListMemberRolePrincipalsByWorkosUser(ctx, repo.ListMemberRolePrincipalsByWorkosUserParams{
				OrganizationID: gramOrgID,
				WorkosUserID:   row.WorkosUserID,
			})
			if err != nil {
				return localRole{}, oops.E(oops.CodeUnexpected, err, "list remaining role assignments").LogError(ctx, r.logger)
			}
			remainingSlugs := make([]string, 0, len(remaining))
			for _, r := range remaining {
				remainingSlugs = append(remainingSlugs, r.RoleSlug)
			}

			// If user has no remaining roles, assign the default member role locally.
			if len(remainingSlugs) == 0 {
				remainingSlugs = []string{authz.SystemRoleMember}
				if _, err := repo.New(tx).UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
					OrganizationID:     gramOrgID,
					WorkosUserID:       row.WorkosUserID,
					WorkosRoleSlug:     authz.SystemRoleMember,
					UserID:             row.UserID,
					WorkosMembershipID: conv.ToPGTextEmpty(membershipID),
					WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
					WorkosLastEventID:  conv.ToPGTextEmpty(""),
				}); err != nil {
					trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
					return localRole{}, oops.E(oops.CodeUnexpected, err, "assign default member role").LogError(ctx, r.logger)
				}
			}

			workosSyncs = append(workosSyncs, func(ctx context.Context) {
				r.syncWorkOS(ctx, "sync member roles after role deletion in workos", func() error {
					_, err := r.roles.UpdateMemberRoles(ctx, membershipID, remainingSlugs)
					if err == nil {
						return nil
					}
					return fmt.Errorf("sync member roles after role deletion in workos: %w", err)
				})
			})
		}
	}

	deletedCount, err := repo.New(tx).MarkOrganizationRoleDeletedLocally(ctx, repo.MarkOrganizationRoleDeletedLocallyParams{
		OrganizationID: gramOrgID,
		WorkosSlug:     currentRole.Slug,
	})
	if err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		return localRole{}, oops.E(oops.CodeUnexpected, err, "mark local role record deleted").LogError(ctx, r.logger)
	}
	if deletedCount == 0 {
		return localRole{}, oops.E(oops.CodeNotFound, nil, "role not found").LogError(ctx, r.logger)
	}

	if err := authz.DeleteRoleGrants(ctx, repo.New(tx), gramOrgID, currentRole.Slug, currentRole.PrincipalURN); err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "delete grants for deleted role").LogError(ctx, r.logger)
	}

	if err := r.audit.LogAccessRoleDelete(ctx, tx, audit.LogAccessRoleDeleteEvent{
		OrganizationID:   gramOrgID,
		Actor:            actor.Principal,
		ActorDisplayName: actor.DisplayName,
		ActorSlug:        nil,
		RoleID:           currentRole.ID,
		RoleName:         currentRole.Name,
		RoleSlug:         currentRole.Slug,
	}); err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "log access role deletion").LogError(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return localRole{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").LogError(ctx, r.logger)
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

	return currentRole, nil
}

type memberRoleUpdateContext struct {
	RoleSlugs    []string
	MembershipID string
	WorkosUserID string
	UserID       string
	Before       *gen.AccessMember
	After        *gen.AccessMember
}

// UpdateMemberRoles replaces all of a member's local role assignments atomically, then best-effort syncs WorkOS after commit.
func (r *RoleManager) UpdateMemberRoles(ctx context.Context, gramOrgID, userID string, roleIDs []string, actor accessAuditActor) (memberRoleUpdateContext, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "begin role transaction").LogError(ctx, r.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// Resolve all role IDs to local role records.
	roles := make([]localRole, 0, len(roleIDs))
	for _, id := range roleIDs {
		role, err := r.getLocalRoleByIDTx(ctx, tx, gramOrgID, id)
		if err != nil {
			return memberRoleUpdateContext{}, err
		}
		roles = append(roles, role)
	}

	connectedUser, err := connectedUser(ctx, tx, gramOrgID, userID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member has not joined this organization").LogError(ctx, r.logger)
	case err != nil:
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "load connected user").LogError(ctx, r.logger)
	}
	if !connectedUser.WorkosID.Valid || connectedUser.WorkosID.String == "" {
		return memberRoleUpdateContext{}, oops.E(oops.CodeBadRequest, nil, "member is not linked to WorkOS").LogError(ctx, r.logger)
	}

	existing, err := repo.New(tx).GetOrganizationRoleAssignmentByWorkosUser(ctx, repo.GetOrganizationRoleAssignmentByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   connectedUser.WorkosID.String,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member not found").LogError(ctx, r.logger)
	case err != nil:
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "load member role assignment").LogError(ctx, r.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))

	membershipID := conv.FromPGTextOrEmpty[string](existing.WorkosMembershipID)
	if membershipID == "" {
		return memberRoleUpdateContext{}, oops.E(oops.CodeNotFound, nil, "member is missing local WorkOS membership linkage").LogError(ctx, r.logger)
	}

	// Snapshot existing role IDs before any mutations.
	existingRoleIDs, err := repo.New(tx).ListActiveRoleIDsByWorkosUser(ctx, repo.ListActiveRoleIDsByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   connectedUser.WorkosID.String,
	})
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "list existing role assignments").LogError(ctx, r.logger)
	}

	// Snapshot existing role slugs to detect admin removal.
	existingRolePrincipals, err := repo.New(tx).ListMemberRolePrincipalsByWorkosUser(ctx, repo.ListMemberRolePrincipalsByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   connectedUser.WorkosID.String,
	})
	if err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "list existing role slugs").LogError(ctx, r.logger)
	}
	hadAdmin := slices.ContainsFunc(existingRolePrincipals, func(rp repo.ListMemberRolePrincipalsByWorkosUserRow) bool {
		return rp.RoleSlug == authz.SystemRoleAdmin
	})

	// Soft-delete all existing assignments, then insert one per role.
	if _, err := repo.New(tx).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, repo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{
		OrganizationID: gramOrgID,
		WorkosUserID:   connectedUser.WorkosID.String,
	}); err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "soft-delete existing role assignments").LogError(ctx, r.logger)
	}

	afterRoleIDs := make([]string, 0, len(roles))
	roleSlugs := make([]string, 0, len(roles))
	for _, role := range roles {
		inserted, err := repo.New(tx).UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
			OrganizationID:     gramOrgID,
			WorkosUserID:       connectedUser.WorkosID.String,
			WorkosRoleSlug:     role.Slug,
			UserID:             conv.ToPGTextEmpty(connectedUser.ID),
			WorkosMembershipID: conv.ToPGTextEmpty(membershipID),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
			WorkosLastEventID:  conv.ToPGTextEmpty(""),
		})
		if err != nil {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "insert role assignment").LogError(ctx, r.logger)
		}
		if inserted == 0 {
			trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
			return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, nil, "insert role assignment").LogError(ctx, r.logger)
		}
		afterRoleIDs = append(afterRoleIDs, role.ID)
		roleSlugs = append(roleSlugs, role.Slug)
	}

	// Guard: prevent removing admin from the last admin in the org.
	hasAdmin := slices.Contains(roleSlugs, authz.SystemRoleAdmin)
	if hadAdmin && !hasAdmin {
		remainingAdmins, err := repo.New(tx).ListOrganizationRoleAssignmentsBySlug(ctx, repo.ListOrganizationRoleAssignmentsBySlugParams{
			OrganizationID: gramOrgID,
			WorkosRoleSlug: authz.SystemRoleAdmin,
		})
		if err != nil {
			return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "count remaining admins").LogError(ctx, r.logger)
		}
		if len(remainingAdmins) == 0 {
			return memberRoleUpdateContext{}, oops.E(oops.CodeBadRequest, nil, "cannot remove admin role: this is the last admin in the organization").LogError(ctx, r.logger)
		}
	}

	memberName := conv.Default(connectedUser.DisplayName, connectedUser.Email)
	memberPrincipalURN := urn.NewPrincipal(urn.PrincipalTypeUser, connectedUser.ID).String()
	result := memberRoleUpdateContext{
		RoleSlugs:    roleSlugs,
		MembershipID: membershipID,
		WorkosUserID: connectedUser.WorkosID.String,
		UserID:       connectedUser.ID,
		Before: &gen.AccessMember{
			ID:           connectedUser.ID,
			PrincipalUrn: memberPrincipalURN,
			Name:         memberName,
			Email:        connectedUser.Email,
			PhotoURL:     conv.FromPGText[string](connectedUser.PhotoUrl),
			RoleIds:      existingRoleIDs,
			JoinedAt:     conv.FromPGTimestamptz(existing.CreatedAt),
		},
		After: &gen.AccessMember{
			ID:           connectedUser.ID,
			PrincipalUrn: memberPrincipalURN,
			Name:         memberName,
			Email:        connectedUser.Email,
			PhotoURL:     conv.FromPGText[string](connectedUser.PhotoUrl),
			RoleIds:      afterRoleIDs,
			JoinedAt:     conv.FromPGTimestamptz(existing.CreatedAt),
		},
	}

	if err := r.audit.LogAccessMemberRoleUpdate(ctx, tx, audit.LogAccessMemberRoleUpdateEvent{
		OrganizationID:       gramOrgID,
		Actor:                actor.Principal,
		ActorDisplayName:     actor.DisplayName,
		ActorSlug:            nil,
		MemberID:             result.UserID,
		MemberName:           result.After.Name,
		MemberEmail:          result.After.Email,
		MemberSnapshotBefore: result.Before,
		MemberSnapshotAfter:  result.After,
	}); err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "log access member role update").LogError(ctx, r.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return memberRoleUpdateContext{}, oops.E(oops.CodeUnexpected, err, "commit role transaction").LogError(ctx, r.logger)
	}

	r.runWorkOSSyncs(ctx, []workosSync{
		func(ctx context.Context) {
			r.syncWorkOS(ctx, "update member roles in workos", func() error {
				_, err := r.roles.UpdateMemberRoles(ctx, result.MembershipID, result.RoleSlugs)
				if err == nil {
					return nil
				}
				return fmt.Errorf("update member roles in workos: %w", err)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list member roles").LogError(ctx, r.logger)
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
		return localRole{}, oops.E(oops.CodeBadRequest, err, "invalid role ID").LogError(ctx, r.logger)
	}

	row, err := repo.New(dbtx).GetOrganizationRoleByID(ctx, repo.GetOrganizationRoleByIDParams{
		ID:             roleID,
		OrganizationID: gramOrgID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return localRole{}, oops.E(oops.CodeNotFound, ErrRoleNotFound, "role not found").LogError(ctx, r.logger)
	case err != nil:
		return localRole{}, oops.E(oops.CodeUnexpected, err, "get role").LogError(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	return localRole{
		ID:           row.ID.String(),
		PrincipalURN: row.RoleUrn,
		Name:         row.WorkosName,
		Slug:         row.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
		MemberCount:  int(row.MemberCount),
	}, nil
}

func (r *RoleManager) getLocalRoleBySlugTx(ctx context.Context, dbtx repo.DBTX, gramOrgID, slug string) (localRole, error) {
	row, err := repo.New(dbtx).GetActiveOrganizationRoleBySlug(ctx, repo.GetActiveOrganizationRoleBySlugParams{
		OrganizationID: gramOrgID,
		WorkosSlug:     slug,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return localRole{}, oops.E(oops.CodeNotFound, ErrRoleNotFound, "role not found").LogError(ctx, r.logger)
	case err != nil:
		return localRole{}, oops.E(oops.CodeUnexpected, err, "get role").LogError(ctx, r.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSource("db"))
	return localRole{
		ID:           row.ID.String(),
		PrincipalURN: row.RoleUrn,
		Name:         row.WorkosName,
		Slug:         row.WorkosSlug,
		Description:  conv.FromPGTextOrEmpty[string](row.WorkosDescription),
		CreatedAt:    conv.FromPGTimestamptz(row.WorkosCreatedAt),
		UpdatedAt:    conv.FromPGTimestamptz(row.WorkosUpdatedAt),
		MemberCount:  int(row.MemberCount),
	}, nil
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
		return nil, oops.E(oops.CodeUnexpected, err, "resolve users by ids").LogError(ctx, r.logger)
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
				return nil, oops.E(oops.CodeBadRequest, nil, "member %s is not linked to WorkOS", id).LogError(ctx, r.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list members").LogError(ctx, r.logger)
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
		return nil, oops.E(oops.CodeBadRequest, nil, "member is missing local WorkOS membership linkage").LogError(ctx, r.logger)
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
			inserted, err := repo.New(dbtx).UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
				OrganizationID:     gramOrgID,
				WorkosUserID:       target.WorkosUserID,
				WorkosRoleSlug:     roleSlug,
				UserID:             conv.ToPGTextEmpty(target.UserID),
				WorkosMembershipID: conv.ToPGTextEmpty(target.MembershipID),
				WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
				WorkosLastEventID:  conv.ToPGTextEmpty(""),
			})
			if err != nil {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return 0, nil, oops.E(oops.CodeUnexpected, err, "upsert local role assignment record").LogError(ctx, r.logger)
			}
			if inserted == 0 {
				trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleDBWriteFailed(true))
				return 0, nil, oops.E(oops.CodeUnexpected, nil, "upsert local role assignment record").LogError(ctx, r.logger)
			}

			// Collect all current role slugs for WorkOS sync.
			allRoles, err := repo.New(dbtx).ListMemberRolePrincipalsByWorkosUser(ctx, repo.ListMemberRolePrincipalsByWorkosUserParams{
				OrganizationID: gramOrgID,
				WorkosUserID:   target.WorkosUserID,
			})
			if err != nil {
				return 0, nil, oops.E(oops.CodeUnexpected, err, "list member roles for workos sync").LogError(ctx, r.logger)
			}
			allSlugs := make([]string, 0, len(allRoles))
			for _, r := range allRoles {
				allSlugs = append(allSlugs, r.RoleSlug)
			}

			membershipID := target.MembershipID
			workosSyncs = append(workosSyncs, func(ctx context.Context) {
				r.syncWorkOS(ctx, "assign member to role in workos", func() error {
					_, err := r.roles.UpdateMemberRoles(ctx, membershipID, allSlugs)
					if err == nil {
						return nil
					}
					return fmt.Errorf("assign member to role in workos: %w", err)
				})
			})
		}
		assignedCount++
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
	// Role grant reads intentionally include both canonical role URNs and
	// legacy role slugs until old principal_grants rows are backfilled.
	grants, err := authz.GrantsForRole(ctx, r.logger, r.db, organizationID, role.Slug, role.PrincipalURN)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load role grants").LogError(ctx, r.logger)
	}
	genGrants := make([]*gen.RoleGrant, 0, len(grants))
	for _, g := range grants {
		genGrants = append(genGrants, scopedGrantToGenRoleGrant(g))
	}

	return &gen.Role{
		ID:           role.ID,
		PrincipalUrn: role.PrincipalURN,
		Name:         role.Name,
		Slug:         role.Slug,
		Description:  role.Description,
		IsSystem:     isSystemRole(role.Slug),
		Grants:       genGrants,
		MemberCount:  role.MemberCount,
		CreatedAt:    conv.Default(role.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:    conv.Default(role.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}, nil
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

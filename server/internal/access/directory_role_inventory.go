package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// maxDirectoryRoleInventoryBytes bounds a private operator inventory, not the
// number of roles a directory source may grant.
const maxDirectoryRoleInventoryBytes = 1 << 20 // 1 MiB

// DirectoryRoleInventory records rules manually inventoried in WorkOS.
type DirectoryRoleInventory struct {
	// OrganizationID identifies the exact Gram organization.
	OrganizationID string `json:"organization_id"`
	// WorkOSOrganizationID must match the organization's current WorkOS link.
	WorkOSOrganizationID string `json:"workos_organization_id"`
	// DefaultRoleSlug records the WorkOS default, without importing or changing it.
	DefaultRoleSlug string `json:"default_role_slug"`
	// Assignments lists explicit group-to-role rules, not membership role rows.
	Assignments []DirectoryRoleInventoryAssignment `json:"assignments"`
}

// DirectoryRoleInventoryAssignment identifies one explicit WorkOS rule.
type DirectoryRoleInventoryAssignment struct {
	// WorkOSGroupID identifies the source independent of its display name.
	WorkOSGroupID string `json:"workos_directory_group_id"`
	// RoleSlug identifies an existing live Gram role.
	RoleSlug string `json:"role_slug"`
}

// DirectoryRoleInventoryDifference describes a conservative role-removal simulation.
type DirectoryRoleInventoryDifference struct {
	// UserID identifies the affected live member, without enumerating emails.
	UserID string `json:"user_id"`
	// LostRoleURNs are roles no longer reached after excluding inventoried direct roles.
	LostRoleURNs []string `json:"lost_role_urns"`
	// GainedRoleURNs are roles reached only in the simulated result.
	GainedRoleURNs []string `json:"gained_role_urns"`
}

// DirectoryRoleInventoryStaleMapping identifies an existing mapping that needs
// an administrator's decision before the additive import can continue.
type DirectoryRoleInventoryStaleMapping struct {
	// WorkOSGroupID identifies the inventoried source with the stale mapping.
	WorkOSGroupID string `json:"workos_directory_group_id"`
	// RoleURN identifies the existing mapping's missing or deleted role.
	RoleURN string `json:"role_urn"`
}

// DirectoryRoleInventoryReport is private operator output and must not be published.
type DirectoryRoleInventoryReport struct {
	// Committed is true only after an import transaction commits.
	Committed bool `json:"committed"`
	// DefaultRoleSlug records the observed default, which the command leaves unchanged.
	DefaultRoleSlug string `json:"default_role_slug"`
	// AddedMappings counts new mappings, or proposed additions in a dry run.
	AddedMappings int `json:"added_mappings"`
	// MissingRoleSlugs lists unresolved roles; any entry prevents the whole import.
	MissingRoleSlugs []string `json:"missing_role_slugs"`
	// AmbiguousRoleSlugs lists slugs matching multiple live roles.
	AmbiguousRoleSlugs []string `json:"ambiguous_role_slugs"`
	// MissingGroupIDs lists sources that are absent, deleted or in another tenant.
	MissingGroupIDs []string `json:"missing_group_ids"`
	// StaleMappings lists preserved mappings that cannot pass live-role validation.
	StaleMappings []DirectoryRoleInventoryStaleMapping `json:"stale_mappings"`
	// MembersChecked counts live members evaluated by the shadow report.
	MembersChecked int `json:"members_checked"`
	// Differences lists every member whose effective role set differs.
	Differences []DirectoryRoleInventoryDifference `json:"differences"`
}

// DecodeDirectoryRoleInventory accepts a single strict JSON object.
func DecodeDirectoryRoleInventory(reader io.Reader) (DirectoryRoleInventory, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxDirectoryRoleInventoryBytes+1))
	if err != nil || len(content) > maxDirectoryRoleInventoryBytes {
		return DirectoryRoleInventory{OrganizationID: "", WorkOSOrganizationID: "", DefaultRoleSlug: "", Assignments: nil}, errors.New("inventory is unreadable or exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var inventory DirectoryRoleInventory
	if err := decoder.Decode(&inventory); err != nil {
		return inventory, errors.New("invalid directory role inventory JSON")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return inventory, errors.New("inventory must contain one JSON object")
	}
	if inventory.OrganizationID == "" || inventory.WorkOSOrganizationID == "" || inventory.DefaultRoleSlug == "" || inventory.Assignments == nil {
		return inventory, errors.New("inventory requires organization IDs, default role slug and assignments")
	}
	for _, assignment := range inventory.Assignments {
		if assignment.WorkOSGroupID == "" || assignment.RoleSlug == "" {
			return inventory, errors.New("every assignment requires a WorkOS group ID and role slug")
		}
	}
	return inventory, nil
}

// RunDirectoryRoleInventory runs an additive import or read-only shadow report.
// The caller must authenticate a current, exact-organization support session and
// prepare its authorization context. This is not a management or MCP endpoint.
func RunDirectoryRoleInventory(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, engine *authz.Engine, auditLogger *audit.Logger, inventory DirectoryRoleInventory, shadow, apply bool) (*DirectoryRoleInventoryReport, error) {
	report := &DirectoryRoleInventoryReport{
		Committed: false, DefaultRoleSlug: inventory.DefaultRoleSlug, AddedMappings: 0,
		MissingRoleSlugs: []string{}, AmbiguousRoleSlugs: []string{}, MissingGroupIDs: []string{},
		StaleMappings: []DirectoryRoleInventoryStaleMapping{}, MembersChecked: 0, Differences: []DirectoryRoleInventoryDifference{},
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || !contextvalues.IsSupportSession(ctx) || ac.ActiveOrganizationID != inventory.OrganizationID || shadow && apply {
		return report, errors.New("requires an exact-organization support session and one operation")
	}
	if err := engine.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return report, err
	}

	options := pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite, DeferrableMode: "", BeginQuery: "", CommitQuery: ""}
	if shadow {
		options.IsoLevel, options.AccessMode = pgx.RepeatableRead, pgx.ReadOnly
	}
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return report, fmt.Errorf("begin directory role inventory: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	operator, err := usersrepo.New(tx).GetUser(ctx, ac.UserID)
	if err != nil || !operator.Admin || operator.DeletedAt.Valid {
		return report, errors.New("support operator is not a live platform administrator")
	}
	organization, err := orgrepo.New(tx).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil || organization.DisabledAt.Valid || !organization.WorkosID.Valid || organization.WorkosID.String != inventory.WorkOSOrganizationID {
		return report, errors.New("inventory organization is disabled or its WorkOS link does not match")
	}
	if !shadow {
		if _, err := queries.CheckDirectoryRoleInventoryOperator(ctx, ac.UserID); err != nil {
			return report, fmt.Errorf("lock support operator: %w", err)
		}
		workosID, err := queries.GetDirectoryRoleInventoryOrganization(ctx, ac.ActiveOrganizationID)
		if err != nil || workosID.String != inventory.WorkOSOrganizationID {
			return report, errors.New("organization changed before import")
		}
	}

	roleSlugs, groupIDs := []string{}, []string{}
	for _, assignment := range inventory.Assignments {
		if assignment.WorkOSGroupID == "" || assignment.RoleSlug == "" {
			return report, errors.New("inventory contains an incomplete assignment")
		}
		roleSlugs = append(roleSlugs, assignment.RoleSlug)
		groupIDs = append(groupIDs, assignment.WorkOSGroupID)
	}
	slices.Sort(roleSlugs)
	roleSlugs = slices.Compact(roleSlugs)
	slices.Sort(groupIDs)
	groupIDs = slices.Compact(groupIDs)
	roles, err := queries.ListDirectoryRoleInventoryRoles(ctx, repo.ListDirectoryRoleInventoryRolesParams{OrganizationID: ac.ActiveOrganizationID, RoleSlugs: roleSlugs})
	if err != nil {
		return report, fmt.Errorf("resolve inventoried roles: %w", err)
	}
	roleURNs := make(map[string]string, len(roles))
	for _, role := range roles {
		if _, exists := roleURNs[role.RoleSlug]; exists {
			report.AmbiguousRoleSlugs = append(report.AmbiguousRoleSlugs, role.RoleSlug)
		}
		roleURNs[role.RoleSlug] = role.RoleUrn
	}
	for _, slug := range roleSlugs {
		if _, exists := roleURNs[slug]; !exists {
			report.MissingRoleSlugs = append(report.MissingRoleSlugs, slug)
		}
	}
	groups, err := queries.ListDirectoryRoleInventoryGroups(ctx, repo.ListDirectoryRoleInventoryGroupsParams{OrganizationID: ac.ActiveOrganizationID, WorkosGroupIds: groupIDs})
	if err != nil {
		return report, fmt.Errorf("resolve inventoried groups: %w", err)
	}
	groupUUIDs := make(map[string]uuid.UUID, len(groups))
	for _, group := range groups {
		groupUUIDs[group.WorkosDirectoryGroupID] = group.ID
	}
	for _, id := range groupIDs {
		if _, exists := groupUUIDs[id]; !exists {
			report.MissingGroupIDs = append(report.MissingGroupIDs, id)
		}
	}
	if len(report.MissingRoleSlugs)+len(report.AmbiguousRoleSlugs)+len(report.MissingGroupIDs) > 0 {
		return report, errors.New("inventory cannot be resolved; no mappings committed")
	}

	if shadow {
		members, err := queries.ListDirectoryRoleInventoryMembers(ctx, ac.ActiveOrganizationID)
		if err != nil {
			return report, fmt.Errorf("list shadow members: %w", err)
		}
		excluded := make(map[string]bool, len(roleURNs))
		for _, roleURN := range roleURNs {
			excluded[roleURN] = true
		}
		for _, memberID := range members {
			principals, err := queries.ListUserRolePrincipals(ctx, repo.ListUserRolePrincipalsParams{OrganizationID: ac.ActiveOrganizationID, UserID: memberID})
			if err != nil {
				return report, fmt.Errorf("evaluate shadow member: %w", err)
			}
			current, simulated := map[string]bool{}, map[string]bool{}
			for _, principal := range principals {
				current[principal.PrincipalUrn] = true
				if principal.FromDirectoryMapping || !excluded[principal.PrincipalUrn] {
					simulated[principal.PrincipalUrn] = true
				}
			}
			difference := DirectoryRoleInventoryDifference{UserID: memberID, LostRoleURNs: []string{}, GainedRoleURNs: []string{}}
			for roleURN := range current {
				if !simulated[roleURN] {
					difference.LostRoleURNs = append(difference.LostRoleURNs, roleURN)
				}
			}
			for roleURN := range simulated {
				if !current[roleURN] {
					difference.GainedRoleURNs = append(difference.GainedRoleURNs, roleURN)
				}
			}
			slices.Sort(difference.LostRoleURNs)
			slices.Sort(difference.GainedRoleURNs)
			if len(difference.LostRoleURNs)+len(difference.GainedRoleURNs) > 0 {
				report.Differences = append(report.Differences, difference)
			}
		}
		report.MembersChecked = len(members)
		return report, nil
	}

	// Acquire every source lock in UUID order before reading existing sets.
	// This matches the single-source writer and prevents concurrent edits being lost.
	for _, group := range groups {
		if err := queries.LockDirectoryRoleMappingSource(ctx, ac.ActiveOrganizationID+":group:"+group.ID.String()); err != nil {
			return report, fmt.Errorf("lock inventoried mapping source: %w", err)
		}
	}
	desiredSets := make(map[uuid.UUID][]string, len(groups))
	for _, group := range groups {
		current, err := queries.ListLiveDirectoryRoleMappingsForSource(ctx, repo.ListLiveDirectoryRoleMappingsForSourceParams{
			OrganizationID: ac.ActiveOrganizationID, DirectoryGroupID: uuid.NullUUID{UUID: group.ID, Valid: true},
			AttributeKey: conv.ToPGTextEmpty(""), AttributeValue: conv.ToPGTextEmpty(""),
		})
		if err != nil {
			return report, fmt.Errorf("read inventoried mapping set: %w", err)
		}
		desired := make([]string, 0, len(current))
		for _, mapping := range current {
			desired = append(desired, mapping.RoleUrn)
		}
		live, err := queries.ListLiveDirectoryMappingRoles(ctx, repo.ListLiveDirectoryMappingRolesParams{OrganizationID: ac.ActiveOrganizationID, RoleUrns: desired})
		if err != nil {
			return report, fmt.Errorf("validate preserved mappings: %w", err)
		}
		for _, roleURN := range desired {
			if !slices.Contains(live, roleURN) {
				report.StaleMappings = append(report.StaleMappings, DirectoryRoleInventoryStaleMapping{WorkOSGroupID: group.WorkosDirectoryGroupID, RoleURN: roleURN})
			}
		}
		for _, assignment := range inventory.Assignments {
			if assignment.WorkOSGroupID != group.WorkosDirectoryGroupID || slices.Contains(desired, roleURNs[assignment.RoleSlug]) {
				continue
			}
			desired = append(desired, roleURNs[assignment.RoleSlug])
			report.AddedMappings++
		}
		desiredSets[group.ID] = desired
	}
	if len(report.StaleMappings) > 0 {
		return report, errors.New("existing mappings point to missing roles; review stale_mappings before importing, no mappings committed")
	}
	writer := &Service{
		tracer: nil, logger: logger, db: db, chConn: nil, auth: nil,
		authz: engine, roleMgr: nil, audit: auditLogger, email: nil, orgHosts: nil, foldGate: nil,
	}
	for _, group := range groups {
		if _, err := writer.setDirectoryRoleMappingsTx(ctx, tx, ac, &gen.SetDirectoryRoleMappingsPayload{
			SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: new(group.ID.String()), RoleUrns: desiredSets[group.ID],
			ApikeyToken: nil, SessionToken: nil, AttributeKey: nil, AttributeValue: nil,
		}, false); err != nil {
			return report, err
		}
	}
	if apply {
		if err := tx.Commit(ctx); err != nil {
			return report, fmt.Errorf("commit directory role inventory: %w", err)
		}
		report.Committed = true
	}
	return report, nil
}

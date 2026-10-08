package roledelivery

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// PlatformCleanupPageSize bounds discovery and exact-ID cleanup requests.
const PlatformCleanupPageSize = 100

// ApplyPlatformCleanup rechecks only the approved exact membership IDs, current
// provenance, current content and tenant scope. Replays are no-ops. The caller
// must request publication for the
// returned plugin IDs in this same transaction, then commit both atomically.
// Ambiguous provenance is preserved even when its membership ID is supplied.
func ApplyPlatformCleanup(ctx context.Context, tx pgx.Tx, org string, projectID uuid.UUID, membershipIDs []uuid.UUID) ([]uuid.UUID, error) {
	return applyPlatformCleanup(ctx, tx, org, projectID, membershipIDs, urn.NewSystemPrincipal("automatic-role-distribution"))
}

// ApplyPlatformCleanupAsUser applies the same exact-ID safeguards as
// ApplyPlatformCleanup, attributing removals to the initiating maintenance operator.
// Publication must still be requested in the same transaction.
func ApplyPlatformCleanupAsUser(ctx context.Context, tx pgx.Tx, org string, projectID uuid.UUID, membershipIDs []uuid.UUID, actorID string) ([]uuid.UUID, error) {
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, actorID)
	if _, err := actor.Value(); err != nil {
		return nil, fmt.Errorf("invalid cleanup actor: %w", err)
	}
	return applyPlatformCleanup(ctx, tx, org, projectID, membershipIDs, actor)
}

func applyPlatformCleanup(ctx context.Context, tx pgx.Tx, org string, projectID uuid.UUID, membershipIDs []uuid.UUID, actor urn.Principal) ([]uuid.UUID, error) {
	if err := validateCleanupScope(org, projectID); err != nil {
		return nil, err
	}
	if len(membershipIDs) > PlatformCleanupPageSize {
		return nil, fmt.Errorf("platform cleanup accepts at most %d membership IDs", PlatformCleanupPageSize)
	}
	if len(membershipIDs) == 0 {
		return nil, nil
	}
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		return nil, fmt.Errorf("lock platform cleanup project: %w", err)
	}
	rows, err := cleanupMemberships(ctx, tx, org, projectID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil, membershipIDs, PlatformCleanupPageSize)
	if err != nil {
		return nil, err
	}
	// Match normal role delivery lock ordering: project admission, then plugins.
	pluginIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		pluginIDs = append(pluginIDs, row.PluginServer.PluginID)
	}
	pluginIDs = sortedCleanupIDs(pluginIDs)
	for _, pluginID := range pluginIDs {
		_, err := pluginsrepo.New(tx).LockRoleDeliveryPlugin(ctx, pluginsrepo.LockRoleDeliveryPluginParams{OrganizationID: org, ProjectID: projectID, PluginID: pluginID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("lock platform cleanup plugin: %w", err)
		}
	}
	// Manual membership edits do not acquire project/plugin locks. Lock their
	// exact rows before the final provenance read so a concurrent update either
	// finishes (and becomes ambiguous) or waits until this transaction commits.
	if _, err := pluginsrepo.New(tx).LockPlatformCleanupMemberships(ctx, pluginsrepo.LockPlatformCleanupMembershipsParams{OrganizationID: org, ProjectID: projectID, MembershipIds: membershipIDs}); err != nil {
		return nil, fmt.Errorf("lock platform cleanup memberships: %w", err)
	}
	// A preview is not authority: re-read after obtaining mutation locks.
	rows, err = cleanupMemberships(ctx, tx, org, projectID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil, membershipIDs, PlatformCleanupPageSize)
	if err != nil {
		return nil, err
	}
	removedPlugins := make([]uuid.UUID, 0)
	for _, row := range rows {
		if !row.AutomaticProvenance {
			continue
		}
		membership := row.PluginServer
		platform, err := ContainsPlatformTools(ctx, tx, org, projectID, membership.ToolsetID, membership.McpServerID)
		if err != nil {
			return nil, err
		}
		if !platform {
			continue
		}
		removed, err := pluginsrepo.New(tx).RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: membership.ID, PluginID: membership.PluginID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("remove platform cleanup membership: %w", err)
		}
		if err := auditChangeAs(ctx, tx, org, projectID, membership.PluginID, removed, false, actor); err != nil {
			return nil, err
		}
		removedPlugins = append(removedPlugins, membership.PluginID)
	}
	return sortedCleanupIDs(removedPlugins), nil
}

// ContentChanged removes proven automatic memberships when current content
// contains platform tools, including legacy direct and typed wrapper entries.
// Otherwise it restores normal eligibility using Eligible's deliberate-removal
// semantics. The result contains ONLY plugins with removed memberships.
// Callers hold project admission and enqueue publication in this transaction on
// every content change, including ordinary-content additions with no removals.
func ContentChanged(ctx context.Context, tx pgx.Tx, org string, projectID, toolsetID uuid.UUID, guard *admission.Guard) ([]uuid.UUID, error) {
	if err := validateCleanupScope(org, projectID); err != nil {
		return nil, err
	}
	if toolsetID == uuid.Nil {
		return nil, fmt.Errorf("platform content change requires a toolset")
	}
	target := uuid.NullUUID{UUID: toolsetID, Valid: true}
	platform, err := ContainsPlatformTools(ctx, tx, org, projectID, target, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	if err != nil {
		return nil, err
	}
	if !platform {
		candidates, err := servers(ctx, tx, org, projectID)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			if candidate.LegacyToolsetID != target && (candidate.BackendKind != "toolset" || candidate.ID != toolsetID) {
				continue
			}
			direct, wrapper := target, uuid.NullUUID{UUID: uuid.Nil, Valid: false}
			if candidate.BackendKind == "mcp_server" {
				direct = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
				wrapper = uuid.NullUUID{UUID: candidate.ID, Valid: true}
			}
			if _, err := Eligible(ctx, tx, org, projectID, direct, wrapper, guard); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	removedPlugins := make([]uuid.UUID, 0)
	after := uuid.Nil
	for {
		rows, err := cleanupMemberships(ctx, tx, org, projectID, target, after, nil, PlatformCleanupPageSize)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, row := range rows {
			if row.AutomaticProvenance {
				ids = append(ids, row.PluginServer.ID)
			}
		}
		removed, err := ApplyPlatformCleanup(ctx, tx, org, projectID, ids)
		if err != nil {
			return nil, err
		}
		removedPlugins = append(removedPlugins, removed...)
		after = rows[len(rows)-1].PluginServer.ID
	}
	return sortedCleanupIDs(removedPlugins), nil
}

func cleanupMemberships(ctx context.Context, tx pgx.Tx, org string, projectID uuid.UUID, toolsetID uuid.NullUUID, after uuid.UUID, ids []uuid.UUID, limit int32) ([]pluginsrepo.ListPlatformCleanupMembershipsRow, error) {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	rows, err := pluginsrepo.New(tx).ListPlatformCleanupMemberships(ctx, pluginsrepo.ListPlatformCleanupMembershipsParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ToolsetID: toolsetID, AfterID: after, MembershipIds: ids, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("list platform cleanup memberships: %w", err)
	}
	return rows, nil
}

func validateCleanupScope(org string, projectID uuid.UUID) error {
	if org == "" || projectID == uuid.Nil {
		return fmt.Errorf("platform cleanup requires an organization and project")
	}
	return nil
}

func sortedCleanupIDs(ids []uuid.UUID) []uuid.UUID {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return slices.Compact(ids)
}

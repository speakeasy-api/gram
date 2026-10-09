// Package roledelivery applies event-scoped role access changes to plugin contents.
// It never reconciles contents during publication or records separate ownership.
package roledelivery

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Snapshot captures effective-grant inputs before the caller changes a role.
func Snapshot(ctx context.Context, tx pgx.Tx, organizationID, roleURN string) ([]authz.Grant, error) {
	p, err := urn.ParsePrincipal(roleURN)
	if err != nil {
		return nil, fmt.Errorf("parse role principal: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, tx, organizationID, []urn.Principal{p})
	if err != nil {
		return nil, fmt.Errorf("load role delivery grants: %w", err)
	}
	return grants, nil
}

type server = pluginsrepo.ListRoleDeliveryServersRow

func servers(ctx context.Context, tx pgx.Tx, org string, projectID uuid.UUID) ([]server, error) {
	candidates, err := pluginsrepo.New(tx).ListRoleDeliveryServers(ctx, pluginsrepo.ListRoleDeliveryServersParams{OrganizationID: org, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list role delivery server candidates: %w", err)
	}
	for i := range candidates {
		s := &candidates[i]
		// Keep the row for removals; only automatic additions use eligibility.
		for _, tool := range s.ToolUrns {
			if tool.Kind == urn.ToolKindPlatform {
				s.Eligible = false
				break
			}
		}
	}
	return candidates, nil
}

// allowed reports whether grants deliver s. Only connect (or root) grants
// deliver: read and write still satisfy connect at the endpoint (see
// authz scopeExpansions[ScopeMCPConnect]), but counting them here would make a
// narrowed connect rule meaningless.
func allowed(grants []authz.Grant, s server) (bool, error) {
	grants = slices.DeleteFunc(slices.Clone(grants), func(g authz.Grant) bool {
		return g.Scope == authz.ScopeMCPRead || g.Scope == authz.ScopeMCPWrite
	})
	allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, s.ResourceID.String(), s.ProjectID.String()))
	if err != nil {
		return false, fmt.Errorf("evaluate role delivery Use access: %w", err)
	}
	return allowed, nil
}

func roleGrants(ctx context.Context, tx pgx.Tx, org string, roles []string) (map[string][]authz.Grant, error) {
	result := make(map[string][]authz.Grant)
	for _, role := range roles {
		if !strings.HasPrefix(role, "role:organization:") && !strings.HasPrefix(role, "role:global:") {
			continue
		}
		grants, err := Snapshot(ctx, tx, org, role)
		if err != nil {
			return nil, err
		}
		result[role] = grants
	}
	return result, nil
}

func supplied(grants map[string][]authz.Grant, s server) (bool, error) {
	for _, g := range grants {
		yes, err := allowed(g, s)
		if err != nil {
			return false, err
		}
		if yes {
			return true, nil
		}
	}
	return false, nil
}

func apply(ctx context.Context, tx pgx.Tx, org string, pluginID uuid.UUID, s server, add, preserveRemoval bool, guard *admission.Guard) (bool, error) {
	if !add {
		memberships, err := pluginsrepo.New(tx).ListPluginServers(ctx, pluginID)
		if err != nil {
			return false, fmt.Errorf("list plugin memberships for role removal: %w", err)
		}
		changed := false
		for _, membership := range memberships {
			matches := s.BackendKind == "mcp_server" && membership.McpServerID.Valid && membership.McpServerID.UUID == s.ID
			matches = matches || (s.LegacyToolsetID.Valid && membership.ToolsetID.Valid && membership.ToolsetID.UUID == s.LegacyToolsetID.UUID)
			if !matches {
				continue
			}
			removed, err := pluginsrepo.New(tx).RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: membership.ID, PluginID: pluginID})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return false, fmt.Errorf("remove role-delivered membership: %w", err)
			}
			if err := auditChange(ctx, tx, org, s.ProjectID, pluginID, removed, false); err != nil {
				return false, err
			}
			changed = true
		}
		return changed, nil
	}
	if !s.Eligible {
		return false, nil
	}
	toolsetID, mcpServerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if s.BackendKind == "mcp_server" {
		mcpServerID = uuid.NullUUID{UUID: s.ID, Valid: true}
	} else {
		toolsetID = uuid.NullUUID{UUID: s.ID, Valid: true}
	}
	exists, err := pluginsrepo.New(tx).HasRoleDeliveryMembership(ctx, pluginsrepo.HasRoleDeliveryMembershipParams{PluginID: pluginID, OrganizationID: org, ProjectID: s.ProjectID, ToolsetID: toolsetID, McpServerID: mcpServerID, LegacyToolsetID: s.LegacyToolsetID, PreserveRemoval: preserveRemoval})
	if err != nil {
		return false, fmt.Errorf("check existing role delivery membership: %w", err)
	}
	if exists {
		return false, nil
	}
	if s.BackendKind == "mcp_server" {
		// An absent rollout context fails closed for provenance-bound remotes, while
		// CheckAttachment leaves legacy/non-remote backends unaffected.
		if err := checkAttachment(ctx, tx, guard, org, s.ProjectID, pluginID, s.ID); err != nil {
			return false, err
		}
	}
	// Choose an unused display name from the current inventory, including when
	// another entry already occupies the stable backend suffix.
	memberships, err := pluginsrepo.New(tx).ListPluginServers(ctx, pluginID)
	if err != nil {
		return false, fmt.Errorf("list role delivery display names: %w", err)
	}
	names := make(map[string]bool, len(memberships))
	for _, membership := range memberships {
		names[membership.DisplayName] = true
	}
	name := s.Name
	if names[name] {
		base := s.Name + " (" + s.ID.String() + ")"
		name = base
		for suffix := 2; names[name]; suffix++ {
			name = fmt.Sprintf("%s %d", base, suffix)
		}
	}
	params := pluginsrepo.AddPluginServerParams{PluginID: pluginID, DisplayName: name, Policy: "required", SortOrder: 0, ToolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, McpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	if s.BackendKind == "mcp_server" {
		params.McpServerID = uuid.NullUUID{UUID: s.ID, Valid: true}
	} else {
		params.ToolsetID = uuid.NullUUID{UUID: s.ID, Valid: true}
	}
	membership, err := pluginsrepo.New(tx).AddPluginServer(ctx, params)
	if err != nil {
		return false, fmt.Errorf("add role-delivered membership: %w", err)
	}
	if err := auditChange(ctx, tx, org, s.ProjectID, pluginID, membership, true); err != nil {
		return false, err
	}
	return true, nil
}

// AudienceChanged applies only newly added/removed role audiences. Replacing an
// unchanged audience is a no-op even when an administrator removed contents.
func AudienceChanged(ctx context.Context, tx pgx.Tx, org string, projectID, pluginID uuid.UUID, before, after []string, guard *admission.Guard) (bool, error) {
	old, err := roleGrants(ctx, tx, org, before)
	if err != nil {
		return false, err
	}
	next, err := roleGrants(ctx, tx, org, after)
	if err != nil {
		return false, err
	}
	added, removed := map[string][]authz.Grant{}, map[string][]authz.Grant{}
	for r, g := range next {
		if _, ok := old[r]; !ok {
			added[r] = g
		}
	}
	for r, g := range old {
		if _, ok := next[r]; !ok {
			removed[r] = g
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return false, nil
	}
	candidates, err := servers(ctx, tx, org, projectID)
	if err != nil {
		return false, err
	}
	changed := false
	for _, s := range candidates {
		add, err := supplied(added, s)
		if err != nil {
			return false, err
		}
		remove, err := supplied(removed, s)
		if err != nil {
			return false, err
		}
		survives, err := supplied(next, s)
		if err != nil {
			return false, err
		}
		if !add && (!remove || survives) {
			continue
		}
		did, err := apply(ctx, tx, org, pluginID, s, add, false, guard)
		if err != nil {
			return false, err
		}
		changed = changed || did
	}
	return changed, nil
}

// Populate is bounded setup/backfill, not an ongoing reconciliation. Deleted
// membership history is respected here but not by a later explicit assignment.
func Populate(ctx context.Context, tx pgx.Tx, org string, projectID, pluginID uuid.UUID, role string, guard *admission.Guard) (bool, error) {
	grants, err := Snapshot(ctx, tx, org, role)
	if err != nil {
		return false, err
	}
	candidates, err := servers(ctx, tx, org, projectID)
	if err != nil {
		return false, err
	}
	changed := false
	for _, s := range candidates {
		yes, err := allowed(grants, s)
		if err != nil {
			return false, err
		}
		if !yes {
			continue
		}
		did, err := apply(ctx, tx, org, pluginID, s, true, true, guard)
		if err != nil {
			return false, err
		}
		changed = changed || did
	}
	return changed, nil
}

// RoleChanged applies a single role's effective Use delta to existing audiences.
// It does not create plugins or restore admin removals on unchanged replay.
// New applicable grants can restore contents; revocation requires losing
// effective Use. Returned projects need publication.
func RoleChanged(ctx context.Context, tx pgx.Tx, org, role string, before []authz.Grant, guard *admission.Guard) ([]uuid.UUID, error) {
	after, err := Snapshot(ctx, tx, org, role)
	if err != nil {
		return nil, err
	}
	fresh := make([]authz.Grant, 0)
	for _, candidate := range after {
		found := slices.ContainsFunc(before, func(old authz.Grant) bool {
			return old.PrincipalUrn == candidate.PrincipalUrn && old.Scope == candidate.Scope && maps.Equal(old.Selector, candidate.Selector)
		})
		if !found {
			fresh = append(fresh, candidate)
		}
	}
	targets, err := pluginsrepo.New(tx).ListRoleDeliveryPlugins(ctx, pluginsrepo.ListRoleDeliveryPluginsParams{OrganizationID: org, PrincipalUrn: role})
	if err != nil {
		return nil, fmt.Errorf("list matching role plugins: %w", err)
	}
	changed := []uuid.UUID{}
	// Match audience/content writers: admission lock, then exact plugin row.
	lockedProjects := map[uuid.UUID]bool{}
	for _, p := range targets {
		if !lockedProjects[p.ProjectID] {
			if err := admission.LockProject(ctx, tx, p.ProjectID); err != nil {
				return nil, fmt.Errorf("lock role delivery project admission: %w", err)
			}
			lockedProjects[p.ProjectID] = true
		}
	}
	for _, p := range targets {
		_, err := pluginsrepo.New(tx).LockRoleDeliveryPlugin(ctx, pluginsrepo.LockRoleDeliveryPluginParams{PluginID: p.ID, OrganizationID: org, ProjectID: p.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lock role delivery plugin: %w", err)
		}
		assignments, err := pluginsrepo.New(tx).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: p.ID, OrganizationID: org, ProjectID: p.ProjectID})
		if err != nil {
			return nil, fmt.Errorf("list role delivery audiences: %w", err)
		}
		roles := make([]string, 0, len(assignments))
		for _, assignment := range assignments {
			roles = append(roles, assignment.PrincipalUrn)
		}
		if !slices.Contains(roles, role) {
			continue
		}
		remaining, err := roleGrants(ctx, tx, org, roles)
		if err != nil {
			return nil, err
		}
		candidates, err := servers(ctx, tx, org, p.ProjectID)
		if err != nil {
			return nil, err
		}
		for _, s := range candidates {
			was, err := allowed(before, s)
			if err != nil {
				return nil, err
			}
			now, err := allowed(after, s)
			if err != nil {
				return nil, err
			}
			if was == now {
				// A genuinely new grant may restore contents even when another scope
				// already supplied Use. Unchanged replay and unrelated grants cannot.
				newlySupplied, err := allowed(fresh, s)
				if err != nil {
					return nil, err
				}
				if !now || !newlySupplied {
					continue
				}
			}
			if !now {
				survives, err := supplied(remaining, s)
				if err != nil {
					return nil, err
				}
				if survives {
					continue
				}
			}
			did, err := apply(ctx, tx, org, p.ID, s, now, false, guard)
			if err != nil {
				return nil, fmt.Errorf("apply role server delivery: %w", err)
			}
			if did && !slices.Contains(changed, p.ProjectID) {
				changed = append(changed, p.ProjectID)
			}
		}
	}
	return changed, nil
}

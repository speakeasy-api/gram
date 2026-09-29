package assignments

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/hints"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	ErrInvalid  = errors.New("invalid plugin assignment")
	ErrNotFound = errors.New("plugin assignment target not found")
)

type Input struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	PluginID         uuid.UUID
	PrincipalURNs    []string
	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string
}

type Result struct {
	// Changed reports whether an origin command mutated the audience.
	Changed            bool
	Plugin             pluginsrepo.Plugin
	Assignments        []pluginsrepo.PluginAssignment
	PrincipalURNs      []string
	PreviousPrincipals []string
}

// Lock selects one exact live plugin under FOR UPDATE. The same lock is used by
// dashboard and Platform MCP writes, so concurrent assignment replacements are
// serialized before either computes its current version.
func Lock(ctx context.Context, tx pgx.Tx, organizationID string, projectID, pluginID uuid.UUID) (pluginsrepo.Plugin, error) {
	if tx == nil || organizationID == "" || projectID == uuid.Nil || pluginID == uuid.Nil {
		return pluginsrepo.Plugin{}, ErrInvalid
	}
	var plugin pluginsrepo.Plugin
	err := tx.QueryRow(ctx, `
SELECT id, organization_id, project_id, name, slug, description, is_default, created_at, updated_at, deleted_at, deleted
FROM plugins
WHERE id = $1
  AND organization_id = $2
  AND project_id = $3
  AND deleted IS FALSE
FOR UPDATE`, pluginID, organizationID, projectID).Scan(
		&plugin.ID,
		&plugin.OrganizationID,
		&plugin.ProjectID,
		&plugin.Name,
		&plugin.Slug,
		&plugin.Description,
		&plugin.IsDefault,
		&plugin.CreatedAt,
		&plugin.UpdatedAt,
		&plugin.DeletedAt,
		&plugin.Deleted,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pluginsrepo.Plugin{}, ErrNotFound
	}
	if err != nil {
		return pluginsrepo.Plugin{}, fmt.Errorf("lock plugin assignment target: %w", err)
	}
	return plugin, nil
}

// IsSubset compares canonical assignment sets without inferring group membership.
// A current wildcard covers every desired audience, including another wildcard.
func IsSubset(desired, current []string) bool {
	set := make(map[string]struct{}, len(current))
	for _, principal := range current {
		if principal == urn.PrincipalWildcard {
			return true
		}
		set[principal] = struct{}{}
	}
	for _, principal := range desired {
		if _, ok := set[principal]; !ok {
			return false
		}
	}
	return true
}

// Guard runs after the complete canonical current and desired sets are known,
// but before any assignment row or audit record is changed. It is required so a
// missed constructor injection can never silently authorize an audience change.
type Guard func(ctx context.Context, tx pgx.Tx, plugin pluginsrepo.Plugin, current, desired []string) error

// BeforeReplace remains separate from authorization. Platform MCP uses it for
// optimistic concurrency after the guard has accepted the desired audience.
type BeforeReplace func(ctx context.Context, plugin pluginsrepo.Plugin, current, desired []string) error

type Dependencies struct {
	Guard         Guard
	BeforeReplace BeforeReplace
}

// LegacyGuard is an explicit no-op authorization dependency for callers whose
// transaction cannot expose a provenance-bound direct remote. It prevents nil
// from accidentally becoming the allow path.
func LegacyGuard(context.Context, pgx.Tx, pluginsrepo.Plugin, []string, []string) error {
	return nil
}

// Replace atomically validates, authorizes, replaces, and audits one locked
// plugin's complete assignment set using the caller-owned transaction.
func Replace(ctx context.Context, tx pgx.Tx, logger *audit.Logger, plugin pluginsrepo.Plugin, input Input, dependencies Dependencies) (Result, error) {
	if tx == nil || logger == nil || dependencies.Guard == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.PluginID == uuid.Nil || input.Actor.IsZero() || plugin.ID != input.PluginID || plugin.OrganizationID != input.OrganizationID || plugin.ProjectID != input.ProjectID || plugin.Name == "" || plugin.Slug == "" || plugin.Deleted {
		return Result{}, ErrInvalid
	}

	queries := pluginsrepo.New(tx)
	principals, err := normalizePrincipals(ctx, tx, input.OrganizationID, input.PrincipalURNs)
	if err != nil {
		return Result{}, err
	}
	existing, err := queries.ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{
		PluginID:       input.PluginID,
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("list existing plugin assignments: %w", err)
	}
	current := make([]string, 0, len(existing))
	existingCanonical := make(map[string]struct{}, len(existing))
	for _, assignment := range existing {
		canonical := canonicalPrincipal(assignment.PrincipalUrn)
		current = append(current, canonical)
		existingCanonical[canonical] = struct{}{}
	}

	directoryService := directory.NewService(tx)
	for _, principal := range principals {
		if _, alreadyAssigned := existingCanonical[principal.URN]; alreadyAssigned {
			continue
		}
		switch principal.kind {
		case principalStandard:
			continue
		case principalDirectoryGroup:
			groupID, parseErr := directory.ParseGroupPrincipal(principal.URN)
			if parseErr != nil {
				return Result{}, fmt.Errorf("%w: directory group", ErrInvalid)
			}
			exists, existsErr := directoryService.GroupExists(ctx, input.OrganizationID, groupID)
			if existsErr != nil {
				return Result{}, fmt.Errorf("validate directory group assignment: %w", existsErr)
			}
			if !exists {
				return Result{}, fmt.Errorf("%w: directory group", ErrInvalid)
			}
		case principalDirectoryAttribute:
			attribute, parseErr := directory.ParseAttributePrincipal(principal.URN)
			if parseErr != nil {
				return Result{}, fmt.Errorf("%w: directory attribute", ErrInvalid)
			}
			exists, existsErr := directoryService.AttributeValueExists(ctx, input.OrganizationID, attribute)
			if existsErr != nil {
				return Result{}, fmt.Errorf("validate directory attribute assignment: %w", existsErr)
			}
			if !exists {
				return Result{}, fmt.Errorf("%w: directory attribute", ErrInvalid)
			}
		}
	}

	desired := make([]string, 0, len(principals))
	for _, principal := range principals {
		desired = append(desired, principal.URN)
	}
	if err := dependencies.Guard(ctx, tx, plugin, current, desired); err != nil {
		return Result{}, err
	}
	if dependencies.BeforeReplace != nil {
		if err := dependencies.BeforeReplace(ctx, plugin, current, desired); err != nil {
			return Result{}, err
		}
	}

	if _, err := queries.RemoveAllPluginAssignments(ctx, pluginsrepo.RemoveAllPluginAssignmentsParams{
		PluginID:       input.PluginID,
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
	}); err != nil {
		return Result{}, fmt.Errorf("remove existing plugin assignments: %w", err)
	}
	created := make([]pluginsrepo.PluginAssignment, 0, len(principals))
	for _, principal := range principals {
		row, err := queries.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
			PluginID:       input.PluginID,
			OrganizationID: input.OrganizationID,
			PrincipalUrn:   principal.URN,
		})
		if err != nil {
			return Result{}, fmt.Errorf("add plugin assignment: %w", err)
		}
		created = append(created, row)
	}
	if err := logger.LogPluginAssignmentsSet(ctx, tx, audit.LogPluginAssignmentsSetEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Actor:            input.Actor,
		ActorDisplayName: input.ActorDisplayName,
		ActorSlug:        input.ActorSlug,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
		PrincipalURNs:    desired,
	}); err != nil {
		return Result{}, fmt.Errorf("audit plugin assignments set: %w", err)
	}
	// A human replacement may intentionally omit the originating role. Persist
	// the hint with that edit; a later reconciler merges only the missing origin.
	missingOrigin, err := queries.HasMissingRolePluginOrigin(ctx, pluginsrepo.HasMissingRolePluginOriginParams{PluginID: plugin.ID, ProjectID: plugin.ProjectID, OrganizationID: plugin.OrganizationID, DesiredPrincipals: desired})
	if err != nil {
		return Result{}, fmt.Errorf("check role plugin association: %w", err)
	}
	if missingOrigin {
		if err := hints.Emit(ctx, tx, hints.Hint{OrganizationID: plugin.OrganizationID, RoleURN: "", PluginID: plugin.ID}); err != nil {
			return Result{}, fmt.Errorf("emit assignment maintenance hint: %w", err)
		}
	}
	return Result{Changed: true, Plugin: plugin, Assignments: created, PrincipalURNs: desired, PreviousPrincipals: current}, nil
}

// AddOrigin adds exactly one role principal without replacing other assignments.
// The caller must hold the admission project lock, then the plugin lock returned
// by Lock, in the same transaction. It must commit the mutation and audit together.
func AddOrigin(ctx context.Context, tx pgx.Tx, logger *audit.Logger, plugin pluginsrepo.Plugin, input Input, guard Guard) (Result, error) {
	return mutateOrigin(ctx, tx, logger, plugin, input, guard, true)
}

// RemoveOrigin removes exactly one role principal, even if its role was deleted.
// It has the same caller-owned transaction and lock requirements as AddOrigin.
func RemoveOrigin(ctx context.Context, tx pgx.Tx, logger *audit.Logger, plugin pluginsrepo.Plugin, input Input, guard Guard) (Result, error) {
	return mutateOrigin(ctx, tx, logger, plugin, input, guard, false)
}

func mutateOrigin(ctx context.Context, tx pgx.Tx, logger *audit.Logger, plugin pluginsrepo.Plugin, input Input, guard Guard, add bool) (Result, error) {
	if tx == nil || logger == nil || guard == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.PluginID == uuid.Nil || input.Actor.IsZero() || plugin.ID != input.PluginID || plugin.OrganizationID != input.OrganizationID || plugin.ProjectID != input.ProjectID || plugin.Name == "" || plugin.Slug == "" || plugin.Deleted || len(input.PrincipalURNs) != 1 {
		return Result{}, ErrInvalid
	}
	principal, err := urn.ParsePrincipal(input.PrincipalURNs[0])
	if err != nil || principal.Type != urn.PrincipalTypeRole {
		return Result{}, fmt.Errorf("%w: origin must be a role principal", ErrInvalid)
	}
	kind, id, ok := strings.Cut(principal.ID, ":")
	if _, err := uuid.Parse(id); !ok || err != nil || (kind != "organization" && kind != "global") {
		return Result{}, fmt.Errorf("%w: role principal", ErrInvalid)
	}
	origin := principal.String()
	queries := pluginsrepo.New(tx)
	params := pluginsrepo.ListPluginAssignmentsParams{PluginID: input.PluginID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID}
	existing, err := queries.ListPluginAssignments(ctx, params)
	if err != nil {
		return Result{}, fmt.Errorf("list existing plugin assignments: %w", err)
	}
	current := make([]string, 0, len(existing))
	desired := make([]string, 0, len(existing)+1)
	found := false
	for _, assignment := range existing {
		canonical := canonicalPrincipal(assignment.PrincipalUrn)
		current = append(current, canonical)
		if assignment.PrincipalUrn == origin {
			found = true
			if !add {
				continue
			}
		}
		desired = append(desired, canonical)
	}
	if add && !found {
		// Validate only the newly added role, never stale unrelated assignments.
		normalized, err := normalizePrincipal(ctx, tx, input.OrganizationID, origin)
		if err != nil {
			return Result{}, err
		}
		desired = append(desired, normalized.URN)
	}
	if err := guard(ctx, tx, plugin, current, desired); err != nil {
		return Result{}, err
	}
	result := Result{Changed: false, Plugin: plugin, Assignments: existing, PrincipalURNs: desired, PreviousPrincipals: current}
	if add == found {
		return result, nil
	}
	var affected int64
	if add {
		affected, err = queries.AddPluginAssignmentOrigin(ctx, pluginsrepo.AddPluginAssignmentOriginParams{
			PluginID: input.PluginID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, PrincipalUrn: origin,
		})
	} else {
		affected, err = queries.RemovePluginAssignmentOrigin(ctx, pluginsrepo.RemovePluginAssignmentOriginParams{
			PluginID: input.PluginID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, PrincipalUrn: origin,
		})
	}
	if err != nil {
		return Result{}, fmt.Errorf("mutate plugin assignment origin: %w", err)
	}
	if affected != 1 {
		return Result{}, fmt.Errorf("mutate plugin assignment origin: expected one locked assignment change, got %d", affected)
	}
	if err := logger.LogPluginAssignmentsSet(ctx, tx, audit.LogPluginAssignmentsSetEvent{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		Actor: input.Actor, ActorDisplayName: input.ActorDisplayName, ActorSlug: input.ActorSlug,
		PluginID: plugin.ID, PluginName: plugin.Name, PluginSlug: plugin.Slug, PrincipalURNs: desired,
	}); err != nil {
		return Result{}, fmt.Errorf("audit plugin assignment origin: %w", err)
	}
	result.Assignments, err = queries.ListPluginAssignments(ctx, params)
	if err != nil {
		return Result{}, fmt.Errorf("list updated plugin assignments: %w", err)
	}
	result.Changed = true
	return result, nil
}

type principalKind uint8

const (
	principalStandard principalKind = iota
	principalDirectoryGroup
	principalDirectoryAttribute
)

type normalizedPrincipal struct {
	kind principalKind
	URN  string
}

func normalizePrincipals(ctx context.Context, db pluginsrepo.DBTX, organizationID string, rawURNs []string) ([]normalizedPrincipal, error) {
	principals := make([]normalizedPrincipal, 0, len(rawURNs))
	seen := make(map[string]struct{}, len(rawURNs))
	for _, raw := range rawURNs {
		principal, err := normalizePrincipal(ctx, db, organizationID, raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[principal.URN]; ok {
			continue
		}
		seen[principal.URN] = struct{}{}
		principals = append(principals, principal)
	}
	return principals, nil
}

func normalizePrincipal(ctx context.Context, db pluginsrepo.DBTX, organizationID, raw string) (normalizedPrincipal, error) {
	switch {
	case raw == urn.PrincipalWildcard:
		return normalizedPrincipal{kind: principalStandard, URN: raw}, nil
	case directory.IsGroupPrincipal(raw):
		id, err := directory.ParseGroupPrincipal(raw)
		if err != nil {
			return normalizedPrincipal{}, fmt.Errorf("%w: directory group", ErrInvalid)
		}
		return normalizedPrincipal{kind: principalDirectoryGroup, URN: directory.GroupPrincipal(id)}, nil
	case directory.IsAttributePrincipal(raw):
		attribute, err := directory.ParseAttributePrincipal(raw)
		if err != nil {
			return normalizedPrincipal{}, fmt.Errorf("%w: directory attribute", ErrInvalid)
		}
		return normalizedPrincipal{kind: principalDirectoryAttribute, URN: directory.AttributePrincipal(attribute.Key, attribute.Value)}, nil
	default:
		normalized := raw
		if address, ok := strings.CutPrefix(raw, string(urn.PrincipalTypeEmail)+":"); ok {
			normalized = string(urn.PrincipalTypeEmail) + ":" + conv.NormalizeEmail(address)
		}
		principal, err := urn.ParsePrincipal(normalized)
		if err != nil {
			return normalizedPrincipal{}, fmt.Errorf("%w: principal", ErrInvalid)
		}
		if principal.Type == urn.PrincipalTypeAgent {
			return normalizedPrincipal{}, fmt.Errorf("%w: agent principals are not supported", ErrInvalid)
		}
		if principal.Type == urn.PrincipalTypeSystem {
			return normalizedPrincipal{}, fmt.Errorf("%w: system principals are not assignable", ErrInvalid)
		}
		if principal.Type == urn.PrincipalTypeWorkload {
			return normalizedPrincipal{}, fmt.Errorf("%w: workload principals are not assignable", ErrInvalid)
		}
		if principal.Type == urn.PrincipalTypeRole {
			if err := authz.ValidatePrincipal(ctx, db, organizationID, principal); err != nil {
				if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
					return normalizedPrincipal{}, fmt.Errorf("%w: role", ErrInvalid)
				}
				return normalizedPrincipal{}, fmt.Errorf("validate role principal: %w", err)
			}
		}
		return normalizedPrincipal{kind: principalStandard, URN: principal.String()}, nil
	}
}

func canonicalPrincipal(value string) string {
	if value == urn.PrincipalWildcard {
		return value
	}
	if groupID, err := directory.ParseGroupPrincipal(value); err == nil {
		return directory.GroupPrincipal(groupID)
	}
	if attribute, err := directory.ParseAttributePrincipal(value); err == nil {
		return directory.AttributePrincipal(attribute.Key, attribute.Value)
	}
	if principal, err := urn.ParsePrincipal(value); err == nil {
		return principal.String()
	}
	return value
}

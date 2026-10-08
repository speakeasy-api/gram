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
	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	ErrInvalid  = errors.New("invalid plugin assignment")
	ErrNotFound = errors.New("plugin assignment target not found")
)

type Input struct {
	OrganizationID string
	ProjectID      uuid.UUID
	PluginID       uuid.UUID
	PrincipalURNs  []string

	// InstallModes maps a principal URN in PrincipalURNs to its install mode.
	// A principal missing from the map keeps its current mode, or gets
	// installmode.Default when it is newly assigned.
	InstallModes     map[string]string
	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string
}

type Result struct {
	ContentChanged     bool
	Plugin             pluginsrepo.Plugin
	Assignments        []pluginsrepo.PluginAssignment
	PrincipalURNs      []string
	PreviousPrincipals []string

	// InstallModes is the stored install mode of each principal in PrincipalURNs.
	InstallModes map[string]installmode.Mode

	// PreviousInstallModes is the install mode of each principal in
	// PreviousPrincipals before the replacement.
	PreviousInstallModes map[string]installmode.Mode
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
SELECT id, organization_id, project_id, name, slug, description, is_default, auto_created, created_at, updated_at, deleted_at, deleted
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
		&plugin.AutoCreated,
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
// currentModes holds the stored install mode of each principal in current.
type BeforeReplace func(ctx context.Context, plugin pluginsrepo.Plugin, current, desired []string, currentModes map[string]installmode.Mode) error

type Dependencies struct {
	DeliveryGuard *admission.Guard
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
	previousModes := make(map[string]installmode.Mode, len(existing))
	for _, assignment := range existing {
		canonical := canonicalPrincipal(assignment.PrincipalUrn)
		current = append(current, canonical)
		existingCanonical[canonical] = struct{}{}
		previousModes[canonical] = installmode.FromStored(assignment.InstallMode)
	}
	modes, err := resolveInstallModes(ctx, tx, input.OrganizationID, principals, input.InstallModes, previousModes)
	if err != nil {
		return Result{}, err
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
		if err := dependencies.BeforeReplace(ctx, plugin, current, desired, previousModes); err != nil {
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
			InstallMode:    string(modes[principal.URN]),
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
		InstallModes:     auditInstallModes(modes),
	}); err != nil {
		return Result{}, fmt.Errorf("audit plugin assignments set: %w", err)
	}
	changed, err := roledelivery.AudienceChanged(ctx, tx, input.OrganizationID, input.ProjectID, plugin.ID, current, desired, dependencies.DeliveryGuard)
	if err != nil {
		return Result{}, fmt.Errorf("deliver role audience servers: %w", err)
	}
	return Result{ContentChanged: changed, Plugin: plugin, Assignments: created, PrincipalURNs: desired, PreviousPrincipals: current, InstallModes: modes, PreviousInstallModes: previousModes}, nil
}

// resolveInstallModes picks each desired principal's install mode: the
// requested one, else its current one, else installmode.Default. A requested
// mode must name a principal that is being assigned.
func resolveInstallModes(ctx context.Context, db pluginsrepo.DBTX, organizationID string, principals []normalizedPrincipal, requested map[string]string, previous map[string]installmode.Mode) (map[string]installmode.Mode, error) {
	modes := make(map[string]installmode.Mode, len(principals))
	for _, principal := range principals {
		mode, ok := previous[principal.URN]
		if !ok {
			mode = installmode.Default
		}
		modes[principal.URN] = mode
	}
	// Keys that normalize to the same principal must agree, or the stored mode
	// would depend on map iteration order.
	chosen := make(map[string]installmode.Mode, len(requested))
	for raw, value := range requested {
		principal, err := normalizePrincipal(ctx, db, organizationID, raw)
		if err != nil {
			return nil, err
		}
		if _, ok := modes[principal.URN]; !ok {
			return nil, fmt.Errorf("%w: install mode for a principal that is not assigned", ErrInvalid)
		}
		mode, err := installmode.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("%w: install mode", ErrInvalid)
		}
		if previous, ok := chosen[principal.URN]; ok && previous != mode {
			return nil, fmt.Errorf("%w: conflicting install modes for one principal", ErrInvalid)
		}
		chosen[principal.URN] = mode
		modes[principal.URN] = mode
	}
	return modes, nil
}

func auditInstallModes(modes map[string]installmode.Mode) map[string]string {
	out := make(map[string]string, len(modes))
	for principal, mode := range modes {
		out[principal] = string(mode)
	}
	return out
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

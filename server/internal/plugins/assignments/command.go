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

// BeforeReplace runs after the exact plugin row is locked and the complete
// current and desired canonical assignment sets are known, but before any row
// is replaced. Platform MCP uses it for optimistic concurrency; the dashboard
// passes nil and otherwise keeps its established behavior.
type BeforeReplace func(ctx context.Context, plugin pluginsrepo.Plugin, current, desired []string) error

// Replace atomically validates, replaces, and audits one locked plugin's
// complete assignment set using the caller-owned transaction.
func Replace(ctx context.Context, tx pgx.Tx, logger *audit.Logger, plugin pluginsrepo.Plugin, input Input, before BeforeReplace) (Result, error) {
	if tx == nil || logger == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.PluginID == uuid.Nil || input.Actor.IsZero() || plugin.ID != input.PluginID || plugin.OrganizationID != input.OrganizationID || plugin.ProjectID != input.ProjectID || plugin.Name == "" || plugin.Slug == "" || plugin.Deleted {
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
	if before != nil {
		if err := before(ctx, plugin, current, desired); err != nil {
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
	return Result{Plugin: plugin, Assignments: created, PrincipalURNs: desired, PreviousPrincipals: current}, nil
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

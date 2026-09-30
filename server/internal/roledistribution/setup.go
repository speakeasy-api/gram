// Package roledistribution performs one-time, best-effort role distribution setup.
package roledistribution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ProcessRoleDistributionSetup handles an additive role distribution event.
// Redelivery can re-add assignments removed by an administrator.
// Errors are retried by the existing transport, not by an application sweep.
func ProcessRoleDistributionSetup(ctx context.Context, db *pgxpool.Pool, publication plugins.PublicationRequests, guard *admission.Guard, roleURN string, organizationID string) (bool, error) {
	// Resolve external rollout state before taking database locks. The selected
	// project and organization are revalidated inside the transaction.
	var projectID uuid.UUID
	var organizationSlug, projectSlug string
	err := db.QueryRow(ctx, `SELECT p.id, o.slug, p.slug FROM projects p
 JOIN organization_metadata o ON o.id = p.organization_id
 WHERE p.organization_id = $1 AND p.deleted IS FALSE AND o.disabled_at IS NULL
 ORDER BY p.created_at, p.id LIMIT 1`, organizationID).Scan(&projectID, &organizationSlug, &projectSlug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("select role setup project: %w", err)
	}
	rollout, rolloutErr := guard.Resolve(ctx, organizationID, organizationSlug, projectSlug)
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin role plugin setup: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Serialize role setups in an organization. Wait rather than
	// acknowledging a busy setup without processing it.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('role-distribution-setup:' || $1, 0))`, organizationID); err != nil {
		return false, fmt.Errorf("lock organization role setup: %w", err)
	}
	skip := func() (bool, error) {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit skipped role setup attempt: %w", err)
		}
		return false, nil
	}
	// This row lock serializes completion with disabling the rollout feature.
	var featureID int64
	err = tx.QueryRow(ctx, `SELECT id FROM organization_features WHERE organization_id = $1 AND feature_name = 'automatic-role-distribution' AND deleted IS FALSE FOR UPDATE`, organizationID).Scan(&featureID)
	if errors.Is(err, pgx.ErrNoRows) {
		return skip()
	}
	if err != nil {
		return false, fmt.Errorf("lock role setup feature gate: %w", err)
	}
	var activeOrganization string
	err = tx.QueryRow(ctx, `SELECT id FROM organization_metadata WHERE id = $1 AND disabled_at IS NULL FOR SHARE`, organizationID).Scan(&activeOrganization)
	if errors.Is(err, pgx.ErrNoRows) {
		return skip()
	}
	if err != nil {
		return false, fmt.Errorf("lock active role setup organization: %w", err)
	}
	parts := strings.Split(roleURN, ":")
	if len(parts) != 3 || parts[0] != "role" {
		return skip()
	}
	roleID, err := uuid.Parse(parts[2])
	if err != nil {
		return skip()
	}
	var name string
	switch parts[1] {
	case "global":
		err = tx.QueryRow(ctx, `SELECT workos_name FROM global_roles WHERE id = $1 AND deleted IS FALSE AND workos_deleted IS FALSE AND 'role:global:' || id::text = $2 FOR SHARE`, roleID, roleURN).Scan(&name)
	case "organization":
		err = tx.QueryRow(ctx, `SELECT workos_name FROM organization_roles WHERE id = $1 AND organization_id = $2 AND deleted IS FALSE AND workos_deleted IS FALSE AND 'role:organization:' || id::text = $3 FOR SHARE`, roleID, organizationID, roleURN).Scan(&name)
	default:
		return skip()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return skip()
	}
	if err != nil {
		return false, fmt.Errorf("validate role setup liveness: %w", err)
	}
	if projectID == uuid.Nil {
		return false, fmt.Errorf("role distribution organization has no active project")
	}
	// Match ordinary audience/content writers: admission lock, then project and
	// plugin row locks. Never wait for admission while holding the project row.
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		return false, fmt.Errorf("lock role setup distribution admission: %w", err)
	}
	var currentProjectID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM projects WHERE organization_id = $1 AND deleted IS FALSE ORDER BY created_at, id LIMIT 1 FOR SHARE`, organizationID).Scan(&currentProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("role distribution organization has no active project")
	}
	if err != nil {
		return false, fmt.Errorf("revalidate role setup project: %w", err)
	}
	if currentProjectID != projectID {
		return false, fmt.Errorf("role distribution default project changed during setup")
	}
	slug := conv.ToSlug(name)
	var pluginID uuid.UUID
	created := false
	// Assignment writers lock the parent plugin FOR UPDATE. Reuse that lock to
	// preserve concurrent admin edits and prevent assigning a deleted plugin.
	err = tx.QueryRow(ctx, `SELECT id FROM plugins WHERE organization_id = $1 AND project_id = $2 AND deleted IS FALSE AND slug = $3 FOR UPDATE`, organizationID, projectID, slug).Scan(&pluginID)
	if errors.Is(err, pgx.ErrNoRows) {
		created = true
		pluginID = uuid.New()
		// The existing slug constraint rejects empty or overlong normalized names.
		_, err = tx.Exec(ctx, `INSERT INTO plugins (id, organization_id, project_id, name, slug, auto_created) VALUES ($1, $2, $3, $4, $5, true)`, pluginID, organizationID, projectID, name, slug)
	}
	if err != nil {
		return false, fmt.Errorf("find or create role plugin: %w", err)
	}
	plugin, err := repo.New(tx).GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: organizationID, ProjectID: projectID})
	if err != nil {
		return false, fmt.Errorf("read role plugin: %w", err)
	}
	existing, err := repo.New(tx).ListPluginAssignments(ctx, repo.ListPluginAssignmentsParams{PluginID: pluginID, OrganizationID: organizationID, ProjectID: projectID})
	if err != nil {
		return false, fmt.Errorf("list role plugin assignments: %w", err)
	}
	current := make([]string, 0, len(existing))
	alreadyAssigned := false
	for _, a := range existing {
		current = append(current, a.PrincipalUrn)
		alreadyAssigned = alreadyAssigned || a.PrincipalUrn == roleURN
	}
	desired := append([]string{}, current...)
	if !alreadyAssigned {
		desired = append(desired, roleURN)
	}
	if !assignments.IsSubset(desired, current) {
		if err := guard.CheckPluginAudience(ctx, tx, rollout, rolloutErr, organizationID, projectID, pluginID, desired); err != nil {
			return false, fmt.Errorf("check role plugin audience: %w", err)
		}
	}
	auditLogger := audit.NewLogger()
	actor := urn.NewPrincipal(urn.PrincipalTypeSystem, "automatic-role-distribution")
	if created {
		if err := auditLogger.LogPluginCreate(ctx, tx, audit.LogPluginCreateEvent{OrganizationID: organizationID, ProjectID: projectID, Actor: actor, ActorDisplayName: nil, ActorSlug: nil, PluginID: pluginID, PluginName: plugin.Name, PluginSlug: plugin.Slug}); err != nil {
			return false, fmt.Errorf("audit role plugin creation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn) VALUES ($1, $2, $3) ON CONFLICT (plugin_id, principal_urn) DO NOTHING`, pluginID, organizationID, roleURN); err != nil {
		return false, fmt.Errorf("add role plugin assignment: %w", err)
	}
	if !alreadyAssigned {
		if err := auditLogger.LogPluginAssignmentsSet(ctx, tx, audit.LogPluginAssignmentsSetEvent{OrganizationID: organizationID, ProjectID: projectID, Actor: actor, ActorDisplayName: nil, ActorSlug: nil, PluginID: pluginID, PluginName: plugin.Name, PluginSlug: plugin.Slug, PrincipalURNs: desired}); err != nil {
			return false, fmt.Errorf("audit role plugin assignment: %w", err)
		}
	}
	if err := publication.Project(ctx, tx, organizationID, projectID, ""); err != nil {
		return false, fmt.Errorf("request role plugin publication: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit role plugin setup: %w", err)
	}
	return true, nil
}

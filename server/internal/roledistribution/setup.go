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
	roledistributionrepo "github.com/speakeasy-api/gram/server/internal/roledistribution/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ProcessRoleDistributionSetup handles an additive role distribution event.
// Redelivery can re-add assignments removed by an administrator.
// Errors are retried by the existing transport, not by an application sweep.
func ProcessRoleDistributionSetup(ctx context.Context, db *pgxpool.Pool, publication plugins.PublicationRequests, guard *admission.Guard, roleURN string, organizationID string) (bool, error) {
	// Resolve external rollout state before taking database locks. The selected
	// project and organization are revalidated inside the transaction.
	project, err := roledistributionrepo.New(db).GetSetupProject(ctx, organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("select role setup project: %w", err)
	}
	projectID := project.ID
	rollout, rolloutErr := guard.Resolve(ctx, organizationID, project.OrganizationSlug, project.ProjectSlug)
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin role plugin setup: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Serialize role setups in an organization. Wait rather than
	// acknowledging a busy setup without processing it.
	if err := requests.LockOrganization(ctx, tx, organizationID); err != nil {
		return false, fmt.Errorf("lock organization role setup: %w", err)
	}
	skip := func() (bool, error) {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit skipped role setup attempt: %w", err)
		}
		return false, nil
	}
	// This row lock serializes completion with disabling the rollout feature.
	queries := roledistributionrepo.New(tx)
	_, err = queries.LockEnabledFeature(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return skip()
	}
	if err != nil {
		return false, fmt.Errorf("lock role setup feature gate: %w", err)
	}
	_, err = queries.LockActiveOrganization(ctx, organizationID)
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
		name, err = queries.LockGlobalRole(ctx, roledistributionrepo.LockGlobalRoleParams{RoleID: roleID, RoleUrn: roleURN})
	case "organization":
		name, err = queries.LockOrganizationRole(ctx, roledistributionrepo.LockOrganizationRoleParams{RoleID: roleID, OrganizationID: organizationID, RoleUrn: roleURN})
	default:
		return skip()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return skip()
	}
	if err != nil {
		return false, fmt.Errorf("validate role setup liveness: %w", err)
	}
	// A projectless organization is valid. Creating its first project publishes
	// an organization bootstrap, so skip rather than retry.
	if projectID == uuid.Nil {
		return skip()
	}
	// Match ordinary audience/content writers: admission lock, then project and
	// plugin row locks. Never wait for admission while holding the project row.
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		return false, fmt.Errorf("lock role setup distribution admission: %w", err)
	}
	currentProjectID, err := queries.LockFirstProject(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return skip()
	}
	if err != nil {
		return false, fmt.Errorf("revalidate role setup project: %w", err)
	}
	if currentProjectID != projectID {
		return false, fmt.Errorf("role distribution default project changed during setup")
	}
	slug := conv.ToSlug(name)
	created := false
	// Assignment writers lock the parent plugin FOR UPDATE. Reuse that lock to
	// preserve concurrent admin edits and prevent assigning a deleted plugin.
	pluginID, err := queries.LockPluginBySlug(ctx, roledistributionrepo.LockPluginBySlugParams{OrganizationID: organizationID, ProjectID: projectID, Slug: slug})
	if errors.Is(err, pgx.ErrNoRows) {
		created = true
		pluginID = uuid.New()
		// The existing slug constraint rejects empty or overlong normalized names.
		err = queries.CreateRolePlugin(ctx, roledistributionrepo.CreateRolePluginParams{ID: pluginID, OrganizationID: organizationID, ProjectID: projectID, Name: name, Slug: slug})
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
	if err := queries.AssignRolePlugin(ctx, roledistributionrepo.AssignRolePluginParams{PluginID: pluginID, OrganizationID: organizationID, ProjectID: projectID, PrincipalUrn: roleURN}); err != nil {
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

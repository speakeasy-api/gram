package roleprovisioning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

// Result distinguishes committed database state from pending intent and a
// publication request. Enqueued publication is never evidence of delivery.
type Result struct {
	PluginID    uuid.UUID
	Pending     string
	Skipped     bool
	Publication plugins.ProjectPublicationRequestOutcome
}

type resolvedAdmission struct {
	project uuid.UUID
	config  admission.RolloutConfig
	err     error
}

// Reconcile handles one role, using current committed intent. Lifecycle source
// hooks, deleted-role fanout, batch continuation and repair scheduling belong to
// callers. Inactive roles never create or restore a plugin.
func (s *Service) Reconcile(ctx context.Context, organizationID, roleURN string, actor Actor) (Result, error) {
	if organizationID == "" || actor.Principal.IsZero() || s.audit == nil {
		return Result{}, ErrInvalid
	}
	// Feature-provider I/O must precede all locks: rollout flags are an attempt
	// snapshot, while database admission policy is read after the project lock.
	// Flag changes apply to later attempts. A destination change fails closed
	// rather than reusing another project's rollout snapshot.
	resolved := resolvedAdmission{project: uuid.Nil, config: admission.RolloutConfig{Mode: "", DirectRemoteDistributionDisabled: false}, err: admission.ErrUnavailable}
	target, err := repo.New(s.db).GetRolloutTarget(ctx, repo.GetRolloutTargetParams{OrganizationID: organizationID, RoleUrn: roleURN})
	if err == nil {
		resolved.project = target.ProjectID
		resolved.config, resolved.err = s.guard.ResolveProject(ctx, s.db, organizationID, target.OrganizationSlug, target.ProjectID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, fmt.Errorf("resolve admission target: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin role reconciliation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	if _, err = q.LockOrganization(ctx, organizationID); err != nil {
		return Result{}, fmt.Errorf("lock organization: %w", err)
	}
	config, err := q.GetSettings(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{PluginID: uuid.Nil, Pending: "", Publication: "", Skipped: true}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("read provisioning settings: %w", err)
	}
	name, err := lockRole(ctx, q, organizationID, roleURN)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{PluginID: uuid.Nil, Pending: "", Publication: "", Skipped: true}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if !config.Enabled {
		return Result{PluginID: uuid.Nil, Pending: "", Publication: "", Skipped: true}, nil
	}
	if err = q.EnsureRoleSetting(ctx, repo.EnsureRoleSettingParams{OrganizationID: text(organizationID), RoleUrn: roleURN, Enabled: true, ProjectID: config.ProjectID}); err != nil {
		return Result{}, fmt.Errorf("ensure role setting: %w", err)
	}
	setting, err := q.GetRoleSetting(ctx, repo.GetRoleSettingParams{OrganizationID: text(organizationID), RoleUrn: roleURN})
	if err != nil {
		return Result{}, fmt.Errorf("get role setting: %w", err)
	}
	if !setting.Enabled {
		return Result{PluginID: uuid.Nil, Pending: "", Publication: "", Skipped: true}, nil
	}
	result := Result{PluginID: uuid.Nil, Pending: "", Skipped: false, Publication: ""}
	if !setting.ProjectID.Valid {
		result.Pending = "choose_project"
	} else {
		// A savepoint preserves desired intent and a pending diagnostic while rolling
		// back every partial destination/audience/name/association/audit/outbox write.
		transition, err := tx.Begin(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("begin: %w", err)
		}
		result, err = s.transition(ctx, transition, organizationID, roleURN, name, setting.ProjectID.UUID, actor, resolved)
		if err != nil {
			if rollbackErr := transition.Rollback(ctx); rollbackErr != nil {
				return Result{}, fmt.Errorf("rollback plugin transition: %w", rollbackErr)
			}
			switch {
			case errors.Is(err, admission.ErrApprovalRequired):
				result = Result{PluginID: uuid.Nil, Skipped: false, Publication: "", Pending: "audience_approval_required"}
			case errors.Is(err, admission.ErrPrivateGatewayAudience):
				result = Result{PluginID: uuid.Nil, Skipped: false, Publication: "", Pending: "private_gateway_audience"}
			case errors.Is(err, admission.ErrUnavailable), errors.Is(err, admission.ErrDistributionDisabled):
				result = Result{PluginID: uuid.Nil, Skipped: false, Publication: "", Pending: "admission_unavailable"}
			case errors.Is(err, pgx.ErrNoRows):
				result = Result{PluginID: uuid.Nil, Skipped: false, Publication: "", Pending: "choose_project"}
			default:
				return Result{}, err
			}
		} else if err = transition.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("commit plugin transition: %w", err)
		}
	}
	code := pgtype.Text{String: result.Pending, Valid: result.Pending != ""}
	if err = q.RecordAttempt(ctx, repo.RecordAttemptParams{OrganizationID: text(organizationID), RoleUrn: roleURN, ErrorCode: code}); err != nil {
		return Result{}, fmt.Errorf("record attempt: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit: %w", err)
	}
	return result, nil
}

func (s *Service) transition(ctx context.Context, tx pgx.Tx, org, role, name string, destination uuid.UUID, actor Actor, resolved resolvedAdmission) (Result, error) {
	q := repo.New(tx)
	associations, err := q.ListAssociations(ctx, repo.ListAssociationsParams{OrganizationID: text(org), RoleUrn: role})
	if err != nil {
		return Result{}, fmt.Errorf("list associations: %w", err)
	}
	// Read associations without locking: the organization row serializes
	// reconcilers. Human name writes lock plugin then marker, so never reverse it.
	projects := []uuid.UUID{destination}
	for _, a := range associations {
		if a.IsCurrent && a.ProjectID.Valid {
			projects = append(projects, a.ProjectID.UUID)
		}
	}
	slices.SortFunc(projects, compareID)
	projects = slices.Compact(projects)
	for _, project := range projects {
		if err = admission.LockProject(ctx, tx, project); err != nil {
			return Result{}, fmt.Errorf("lock project: %w", err)
		}
	}
	liveProjects := map[uuid.UUID]bool{}
	for _, project := range projects {
		_, err = q.LockProject(ctx, repo.LockProjectParams{OrganizationID: org, ProjectID: project})
		if err == nil {
			liveProjects[project] = true
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) || project == destination {
			return Result{}, fmt.Errorf("lock live project: %w", err)
		}
	}
	ids := []uuid.UUID{}
	byID := map[uuid.UUID]repo.RolePluginAssociation{}
	for _, a := range associations {
		if !a.ProjectID.Valid || !a.PluginID.Valid || !liveProjects[a.ProjectID.UUID] || (!a.IsCurrent && a.ProjectID.UUID != destination) {
			continue
		}
		ids = append(ids, a.PluginID.UUID)
		byID[a.PluginID.UUID] = a
	}
	slices.SortFunc(ids, compareID)
	locked := map[uuid.UUID]pluginsrepo.Plugin{}
	for _, id := range ids {
		a := byID[id]
		plugin, err := assignments.Lock(ctx, tx, org, a.ProjectID.UUID, id)
		if errors.Is(err, assignments.ErrNotFound) {
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("lock associated plugin: %w", err)
		}
		locked[id] = plugin
	}
	// Re-read markers after plugin locks: an administrator may have changed a
	// name away and back (clearing its marker) while we waited for the lock.
	associations, err = q.ListAssociations(ctx, repo.ListAssociationsParams{OrganizationID: text(org), RoleUrn: role})
	if err != nil {
		return Result{}, fmt.Errorf("list associations: %w", err)
	}
	// Association writes begin only after all existing plugin locks are held.
	var target repo.RolePluginAssociation
	var plugin pluginsrepo.Plugin
	for _, a := range associations {
		if !a.ProjectID.Valid || !liveProjects[a.ProjectID.UUID] || (!a.IsCurrent && a.ProjectID.UUID != destination) {
			continue
		}
		p, exists := locked[a.PluginID.UUID]
		if !exists {
			if err = q.RetireAssociation(ctx, repo.RetireAssociationParams{ID: a.ID, OrganizationID: text(org), ProjectID: a.ProjectID}); err != nil {
				return Result{}, fmt.Errorf("retire association: %w", err)
			}
		} else if a.ProjectID.UUID == destination {
			target = a
			plugin = p
		}
	}
	packageChanged := false
	if plugin.ID == uuid.Nil {
		for attempt := range 5 {
			created, createErr := q.CreateRolePlugin(ctx, repo.CreateRolePluginParams{OrganizationID: org, ProjectID: destination, Name: name, Slug: roleSlug(name, role, attempt)})
			if errors.Is(createErr, pgx.ErrNoRows) {
				continue
			}
			if createErr != nil {
				return Result{}, fmt.Errorf("create role plugin: %w", createErr)
			}
			plugin = pluginsrepo.Plugin(created)
			break
		}
		if plugin.ID == uuid.Nil {
			return Result{}, fmt.Errorf("role plugin slug collision limit reached")
		}
		target, err = q.CreateAssociation(ctx, repo.CreateAssociationParams{OrganizationID: text(org), RoleUrn: role, ProjectID: destination, PluginID: plugin.ID})
		if err != nil {
			return Result{}, fmt.Errorf("create association: %w", err)
		}
		if err = s.audit.LogPluginCreate(ctx, tx, audit.LogPluginCreateEvent{ActorDisplayName: nil, ActorSlug: nil, OrganizationID: org, ProjectID: destination, Actor: actor.Principal, PluginID: plugin.ID, PluginName: plugin.Name, PluginSlug: plugin.Slug}); err != nil {
			return Result{}, fmt.Errorf("log plugin create: %w", err)
		}
		packageChanged = true
	}
	guard := func(ctx context.Context, tx pgx.Tx, p pluginsrepo.Plugin, current, desired []string) error {
		// Only unchanged assignments and removals bypass admission. An existing
		// Everyone assignment must not hide a newly added role from gateway checks.
		if !slices.ContainsFunc(desired, func(principal string) bool {
			return !slices.Contains(current, principal)
		}) {
			return nil
		}
		if s.guard == nil {
			return admission.ErrUnavailable
		}
		config, policyErr := resolved.config, resolved.err
		if resolved.project != p.ProjectID {
			policyErr = admission.ErrUnavailable
		}
		return s.guard.CheckPluginAudience(ctx, tx, config, policyErr, org, p.ProjectID, p.ID, desired)
	}
	input := assignments.Input{ActorDisplayName: nil, ActorSlug: nil, OrganizationID: org, ProjectID: destination, PluginID: plugin.ID, PrincipalURNs: []string{role}, Actor: actor.Principal}
	if _, err = assignments.AddOrigin(ctx, tx, s.audit, plugin, input, guard); err != nil {
		return Result{}, fmt.Errorf("add origin: %w", err)
	}
	for _, a := range associations {
		if !a.IsCurrent || !a.PluginID.Valid || a.PluginID.UUID == plugin.ID {
			continue
		}
		old, ok := locked[a.PluginID.UUID]
		if !ok {
			continue
		}
		oldInput := input
		oldInput.PluginID = old.ID
		oldInput.ProjectID = old.ProjectID
		if _, err = assignments.RemoveOrigin(ctx, tx, s.audit, old, oldInput, guard); err != nil {
			return Result{}, fmt.Errorf("remove origin: %w", err)
		}
	}
	if target.LastAutomaticName.Valid {
		if target.LastAutomaticName.String != plugin.Name {
			if err = q.SetAutomaticName(ctx, repo.SetAutomaticNameParams{OrganizationID: org, ProjectID: destination, PluginID: plugin.ID, Name: pgtype.Text{String: "", Valid: false}}); err != nil {
				return Result{}, fmt.Errorf("set automatic name: %w", err)
			}
		} else if plugin.Name != name {
			if err = q.RenameRolePlugin(ctx, repo.RenameRolePluginParams{OrganizationID: org, ProjectID: destination, PluginID: plugin.ID, PreviousName: plugin.Name, Name: name}); err != nil {
				return Result{}, fmt.Errorf("rename role plugin: %w", err)
			}
			if err = q.SetAutomaticName(ctx, repo.SetAutomaticNameParams{OrganizationID: org, ProjectID: destination, PluginID: plugin.ID, Name: text(name)}); err != nil {
				return Result{}, fmt.Errorf("set automatic name: %w", err)
			}
			description := conv.FromPGText[string](plugin.Description)
			if err = s.audit.LogPluginUpdate(ctx, tx, audit.LogPluginUpdateEvent{ActorDisplayName: nil, ActorSlug: nil, OrganizationID: org, ProjectID: destination, Actor: actor.Principal, PluginID: plugin.ID, PluginName: name, PluginSlug: plugin.Slug, SnapshotBefore: &audit.PluginSnapshot{Name: plugin.Name, Slug: plugin.Slug, Description: description}, SnapshotAfter: &audit.PluginSnapshot{Name: name, Slug: plugin.Slug, Description: description}}); err != nil {
				return Result{}, fmt.Errorf("log plugin update: %w", err)
			}
			packageChanged = true
		}
	}
	if !target.IsCurrent {
		if err = q.ClearCurrentAssociation(ctx, repo.ClearCurrentAssociationParams{OrganizationID: text(org), RoleUrn: role}); err != nil {
			return Result{}, fmt.Errorf("clear current association: %w", err)
		}
		if err = q.SetCurrentAssociation(ctx, repo.SetCurrentAssociationParams{ID: target.ID, OrganizationID: text(org), ProjectID: nullableID(destination)}); err != nil {
			return Result{}, fmt.Errorf("set current association: %w", err)
		}
	}
	result := Result{Pending: "", Skipped: false, Publication: "", PluginID: plugin.ID}
	if packageChanged {
		result.Publication, err = s.publication.ProjectWithOutcome(ctx, tx, org, destination, actor.PublicationUserID)
		if err != nil {
			return Result{}, fmt.Errorf("request project publication: %w", err)
		}
	}
	return result, nil
}

func compareID(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }

func roleSlug(name, role string, attempt int) string {
	base := conv.ToSlug(name)
	if base == "" {
		base = "role"
	}
	// Canonical UUID identity is stable across names and organization/global roles.
	scope := "o"
	if strings.HasPrefix(role, "role:global:") {
		scope = "g"
	}
	suffix := scope + "-" + role[len(role)-36:]
	if attempt > 0 {
		suffix += "-" + strconv.Itoa(attempt)
	}
	limit := 60 - len(suffix) - 1
	if len(base) > limit {
		base = base[:limit]
	}
	return base + "-" + suffix
}

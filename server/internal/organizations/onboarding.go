package organizations

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// onboardingPreset is a named task selection. The onboarding survey resolves
// to one, and staff can apply one from the admin dashboard.
type onboardingPreset struct {
	Key   string
	Title string
	// TaskKeys are setupTaskCatalog keys; the wizard walks them in catalog order.
	TaskKeys []string
}

// onboardingPresets is the only list of presets. Adding an entry here is all a
// new preset needs: the admin editor, the Admin API, and submitOnboardingSurvey
// all read from it.
var onboardingPresets = []onboardingPreset{
	{Key: "gateway", Title: "Gateway", TaskKeys: []string{"create-marketplace", "distribute-servers"}},
	{Key: "security", Title: "Security", TaskKeys: []string{"domain-verification", "connect-idp", "directory-sync", "create-marketplace", "enable-logging", "anthropic-observability", "instrument-agents", "additional-agent-config", "confirm-traffic", "anthropic-admin-controls", "configure-policies"}},
}

func onboardingPresetByKey(key string) *onboardingPreset {
	for i := range onboardingPresets {
		if onboardingPresets[i].Key == key {
			return &onboardingPresets[i]
		}
	}
	return nil
}

// IsOnboardingPreset reports whether key names a preset.
func IsOnboardingPreset(key string) bool {
	return onboardingPresetByKey(key) != nil
}

func adminOnboardingPresets() []*gen.AdminOnboardingPreset {
	presets := make([]*gen.AdminOnboardingPreset, 0, len(onboardingPresets))
	for _, preset := range onboardingPresets {
		presets = append(presets, &gen.AdminOnboardingPreset{Key: preset.Key, Title: preset.Title, VisibleTaskKeys: slices.Clone(preset.TaskKeys)})
	}
	return presets
}

// LoadOnboardingConfiguration reads effective selection in one database snapshot.
// The caller must authorize access to the explicit organization.
func LoadOnboardingConfiguration(ctx context.Context, db repo.DBTX, organizationID string) (*gen.AdminOnboardingConfiguration, error) {
	rows, err := repo.New(db).GetOrganizationOnboardingSelection(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("load onboarding selection: %w", err)
	}
	if len(rows) == 0 {
		return nil, oops.C(oops.CodeNotFound)
	}
	hidden := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.TaskKey.Valid {
			hidden[row.TaskKey.String] = row.HiddenAt.Valid
		}
	}
	tasks := make([]*gen.AdminOnboardingTask, 0, len(setupTaskCatalog))
	for _, task := range setupTaskCatalog {
		value, exists := hidden[task.Key]
		if !exists {
			value = task.HiddenByDefault
		}
		tasks = append(tasks, &gen.AdminOnboardingTask{Key: task.Key, Title: task.Title, Description: task.Description, Hidden: value})
	}
	return &gen.AdminOnboardingConfiguration{OrganizationID: organizationID, Preset: conv.FromPGText[string](rows[0].OnboardingPreset), Tasks: tasks, Presets: adminOnboardingPresets()}, nil
}

// SaveOnboardingConfiguration changes selection only. Its caller authenticates
// staff; it never invokes the session-bound assignment or email path.
func SaveOnboardingConfiguration(ctx context.Context, db *pgxpool.Pool, logger *audit.Logger, organizationID string, visibleTaskKeys []string, preset *string, actor urn.Principal, displayName *string) (*gen.AdminOnboardingConfiguration, error) {
	if visibleTaskKeys == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "visible_task_keys must be an explicit array")
	}
	if preset != nil && !IsOnboardingPreset(*preset) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid onboarding preset")
	}
	for _, key := range visibleTaskKeys {
		if setupTaskDefinitionForKey(key) == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "unknown setup task")
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin onboarding configuration: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	org, err := queries.LockOrganizationForSetupTaskUpdate(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("lock onboarding organization: %w", err)
	}
	before, err := LoadOnboardingConfiguration(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	beforeTasks, err := projectSetupTasks(ctx, queries, organizationID)
	if err != nil {
		return nil, err
	}
	for _, task := range before.Tasks {
		hidden := !slices.Contains(visibleTaskKeys, task.Key)
		if err := queries.SetOrganizationSetupTaskVisibility(ctx, repo.SetOrganizationSetupTaskVisibilityParams{OrganizationID: organizationID, TaskKey: task.Key, Hidden: hidden}); err != nil {
			return nil, fmt.Errorf("save onboarding task visibility: %w", err)
		}
	}
	afterTasks, err := projectSetupTasks(ctx, queries, organizationID)
	if err != nil {
		return nil, err
	}
	for _, task := range beforeTasks {
		next := setupTaskByKey(afterTasks, task.Key)
		if task.Hidden == next.Hidden {
			continue
		}
		if err := logger.LogOrganizationSetupTaskUpdated(ctx, tx, audit.LogOrganizationSetupTaskUpdatedEvent{
			OrganizationID: organizationID, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil,
			OrganizationName: org.Name, OrganizationSlug: org.Slug, TaskKey: task.Key,
			SetupTaskSnapshotBefore: setupTaskAuditSnapshot(task), SetupTaskSnapshotAfter: setupTaskAuditSnapshot(next),
		}); err != nil {
			return nil, fmt.Errorf("audit onboarding task visibility: %w", err)
		}
	}
	if preset != nil {
		if err := queries.SetOrganizationOnboardingPreset(ctx, repo.SetOrganizationOnboardingPresetParams{OrganizationID: organizationID, Preset: conv.ToPGText(*preset)}); err != nil {
			return nil, fmt.Errorf("save onboarding preset: %w", err)
		}
	}
	after, err := LoadOnboardingConfiguration(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	if err := logger.LogOrganizationOnboardingUpdated(ctx, tx, audit.LogOrganizationOnboardingUpdatedEvent{
		OrganizationID: organizationID, Actor: actor, ActorDisplayName: displayName,
		OrganizationName: org.Name, OrganizationSlug: org.Slug,
		OnboardingSnapshotBefore: onboardingSnapshot(before), OnboardingSnapshotAfter: onboardingSnapshot(after),
	}); err != nil {
		return nil, fmt.Errorf("audit onboarding configuration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit onboarding configuration: %w", err)
	}
	return after, nil
}

func onboardingSnapshot(config *gen.AdminOnboardingConfiguration) *audit.OrganizationOnboardingSnapshot {
	keys := make([]string, 0, len(config.Tasks))
	for _, task := range config.Tasks {
		if !task.Hidden {
			keys = append(keys, task.Key)
		}
	}
	return &audit.OrganizationOnboardingSnapshot{Preset: config.Preset, VisibleTaskKeys: keys}
}

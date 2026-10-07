package roledelivery

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

type rolloutKey struct{}
type projectRollout struct {
	Config admission.RolloutConfig
	Err    error
}
type rollouts struct {
	OrganizationID string
	Projects       map[uuid.UUID]projectRollout
}

// PrepareAdmission resolves external rollout state before callers take mutation
// locks. Authorization and target identities are still revalidated in the write
// transaction. Missing preparation fails closed only for scoped direct remotes.
func PrepareAdmission(ctx context.Context, db pluginsrepo.DBTX, guard *admission.Guard, org string) (context.Context, error) {
	if guard == nil {
		return ctx, nil
	}
	if current, ok := ctx.Value(rolloutKey{}).(rollouts); ok && current.OrganizationID == org {
		return ctx, nil
	}
	projects, err := pluginsrepo.New(db).ListRoleDeliveryProjects(ctx, org)
	if err != nil {
		return ctx, fmt.Errorf("list role delivery projects: %w", err)
	}
	prepared := rollouts{OrganizationID: org, Projects: make(map[uuid.UUID]projectRollout, len(projects))}
	for _, p := range projects {
		config, resolveErr := guard.Resolve(ctx, org, p.OrganizationSlug, p.Slug)
		prepared.Projects[p.ID] = projectRollout{Config: config, Err: resolveErr}
	}
	return context.WithValue(ctx, rolloutKey{}, prepared), nil
}

func checkAttachment(ctx context.Context, tx pgx.Tx, guard *admission.Guard, org string, projectID, pluginID, serverID uuid.UUID) error {
	selected := projectRollout{Config: admission.RolloutConfig{Mode: "", DirectRemoteDistributionDisabled: false}, Err: admission.ErrUnavailable}
	if prepared, ok := ctx.Value(rolloutKey{}).(rollouts); ok && prepared.OrganizationID == org {
		if value, found := prepared.Projects[projectID]; found {
			selected = value
		}
	}
	if err := guard.CheckAttachment(ctx, tx, selected.Config, selected.Err, org, projectID, pluginID, serverID); err != nil {
		return fmt.Errorf("check role delivery attachment admission: %w", err)
	}
	return nil
}

// WithProjectAdmission reuses rollout resolved by the enclosing service before
// its transaction, without external reads while mutation locks are held.
func WithProjectAdmission(ctx context.Context, org string, projectID uuid.UUID, rollout admission.RolloutConfig, rolloutErr error) context.Context {
	prepared := rollouts{OrganizationID: org, Projects: map[uuid.UUID]projectRollout{projectID: {Config: rollout, Err: rolloutErr}}}
	return context.WithValue(ctx, rolloutKey{}, prepared)
}

package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

var (
	// ErrProjectRedeployTargetNotFound reports no live project with that ID in
	// that organization, or no organization row to own it.
	ErrProjectRedeployTargetNotFound = errors.New("project redeploy target not found")

	// ErrProjectHasNoActiveDeployment reports a project with nothing to redeploy.
	ErrProjectHasNoActiveDeployment = errors.New("project has no active deployment")

	// ErrProjectRedeployInProgress reports a newer deployment still processing.
	// Another clone would run every source again, then fail to take over.
	ErrProjectRedeployInProgress = errors.New("a newer deployment is still processing")
)

// ProjectRedeployer clones a project's deployment and starts processing it. The
// clone commits with the caller's transaction; Dispatch runs after the commit.
type ProjectRedeployer interface {
	Prepare(ctx context.Context, tx pgx.Tx, req deployments.StaffRedeploy) (*types.Deployment, error)
	Dispatch(ctx context.Context, projectID, deploymentID, expectedActive uuid.UUID) error
}

// ProjectRedeployState is what a redeploy would act on, pinned by row locks on
// the organization and project.
type ProjectRedeployState struct {
	OrganizationID     string
	OrganizationName   string
	OrganizationSlug   string
	ProjectID          uuid.UUID
	ProjectName        string
	ProjectSlug        string
	ActiveDeploymentID uuid.UUID
	OpenAPIAssets      int
	FunctionAssets     int
	Packages           int
	ExternalMCPs       int

	// InFlightDeploymentID is a newer deployment started within the deployment
	// workflow's run timeout that has not finished. One older than that is no
	// longer being processed, so it cannot block a project indefinitely.
	InFlightDeploymentID uuid.NullUUID
}

// LockProjectRedeployStateTx reads the exact project and its active deployment.
// It returns ErrProjectRedeployTargetNotFound for a soft-deleted project or one
// in another organization, and ErrProjectHasNoActiveDeployment when there is
// nothing to redeploy.
func LockProjectRedeployStateTx(ctx context.Context, tx pgx.Tx, logger *slog.Logger, organizationID string, projectID uuid.UUID, now time.Time) (ProjectRedeployState, error) {
	organization, err := LockOrganizationAccessTx(ctx, tx, organizationID)
	if errors.Is(err, ErrOrganizationAccessNotFound) {
		return ProjectRedeployState{}, ErrProjectRedeployTargetNotFound
	}
	if err != nil {
		return ProjectRedeployState{}, err
	}

	project, err := projectsrepo.New(tx).GetProjectByIDAndOrganizationIDForUpdate(ctx, projectsrepo.GetProjectByIDAndOrganizationIDForUpdateParams{ID: projectID, OrganizationID: organization.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRedeployState{}, ErrProjectRedeployTargetNotFound
	}
	if err != nil {
		return ProjectRedeployState{}, fmt.Errorf("lock project: %w", err)
	}

	deploymentQueries := deploymentsrepo.New(tx)
	activeID, err := deploymentQueries.GetActiveDeploymentID(ctx, project.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRedeployState{}, ErrProjectHasNoActiveDeployment
	}
	if err != nil {
		return ProjectRedeployState{}, fmt.Errorf("get active deployment: %w", err)
	}

	active, err := mv.DescribeDeployment(ctx, logger, deploymentQueries, mv.ProjectID(project.ID), mv.DeploymentID(activeID))
	if err != nil {
		return ProjectRedeployState{}, fmt.Errorf("describe active deployment: %w", err)
	}

	inFlightID, err := deploymentQueries.GetInFlightDeploymentAfter(ctx, deploymentsrepo.GetInFlightDeploymentAfterParams{
		ProjectID:         project.ID,
		AfterDeploymentID: activeID,
		CreatedAfter:      conv.ToPGTimestamptz(now.Add(-background.ProcessDeploymentRunTimeout)),
	})
	inFlight := uuid.NullUUID{UUID: inFlightID, Valid: err == nil}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ProjectRedeployState{}, fmt.Errorf("get in-flight deployment: %w", err)
	}

	return ProjectRedeployState{
		OrganizationID:     organization.OrganizationID,
		OrganizationName:   organization.Name,
		OrganizationSlug:   organization.Slug,
		ProjectID:          project.ID,
		ProjectName:        project.Name,
		ProjectSlug:        project.Slug,
		ActiveDeploymentID: activeID,
		OpenAPIAssets:      len(active.Openapiv3Assets),
		FunctionAssets:     len(active.FunctionsAssets),
		Packages:           len(active.Packages),
		ExternalMCPs:       len(active.ExternalMcps),

		InFlightDeploymentID: inFlight,
	}, nil
}

// SetProjectRedeployer enables project redeploys. Without one, as on an admin
// server with no Temporal connection, they report unsupported.
func (s *Service) SetProjectRedeployer(redeployer ProjectRedeployer) {
	s.projectRedeployer = redeployer
}

func (s *Service) mapProjectRedeployError(ctx context.Context, logger *slog.Logger, err error) error {
	switch {
	case errors.Is(err, ErrProjectRedeployTargetNotFound):
		return oops.C(oops.CodeNotFound)
	case errors.Is(err, ErrProjectHasNoActiveDeployment):
		return oops.E(oops.CodeConflict, err, "project has no active deployment to redeploy").LogInfo(ctx, logger)
	case errors.Is(err, ErrProjectRedeployInProgress):
		return oops.E(oops.CodeConflict, err, "a newer deployment is still processing; wait for it to finish").LogInfo(ctx, logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "read project redeploy target").LogError(ctx, logger)
	}
}

// GetProjectRedeployPreview returns what a redeploy of the project would act on.
func (s *Service) GetProjectRedeployPreview(ctx context.Context, payload *gen.GetProjectRedeployPreviewPayload) (*gen.AdminProjectRedeployPreview, error) {
	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogWarn(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectID(projectID.String()))

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin project redeploy preview").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	state, err := LockProjectRedeployStateTx(ctx, tx, logger, payload.OrganizationID, projectID, time.Now())
	if err != nil {
		return nil, s.mapProjectRedeployError(ctx, logger, err)
	}

	return &gen.AdminProjectRedeployPreview{
		OrganizationID:     state.OrganizationID,
		ProjectID:          state.ProjectID.String(),
		ProjectName:        state.ProjectName,
		ActiveDeploymentID: state.ActiveDeploymentID.String(),
		OpenapiAssets:      state.OpenAPIAssets,
		FunctionAssets:     state.FunctionAssets,
		Packages:           state.Packages,
		ExternalMcps:       state.ExternalMCPs,
	}, nil
}

// RedeployProject clones the project's active deployment and starts processing
// it. It refuses when the active deployment is no longer the one the operator
// reviewed, or while a newer deployment is still processing.
func (s *Service) RedeployProject(ctx context.Context, payload *gen.RedeployProjectPayload) (*gen.AdminProjectRedeployResult, error) {
	if s.projectRedeployer == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "project redeploys are not available on this server").LogWarn(ctx, s.logger)
	}
	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogWarn(ctx, s.logger)
	}
	expectedActive, err := uuid.Parse(payload.ExpectedActiveDeploymentID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid expected active deployment id").LogWarn(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectID(projectID.String()))

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin project redeploy").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	state, err := LockProjectRedeployStateTx(ctx, tx, logger, payload.OrganizationID, projectID, time.Now())
	if err != nil {
		return nil, s.mapProjectRedeployError(ctx, logger, err)
	}
	if state.ActiveDeploymentID != expectedActive {
		return nil, oops.E(oops.CodeConflict, nil, "the active deployment changed; review the project again").LogInfo(ctx, logger)
	}
	if state.InFlightDeploymentID.Valid {
		return nil, s.mapProjectRedeployError(ctx, logger, ErrProjectRedeployInProgress)
	}

	actor, actorDisplayName, _ := adminActor(ctx)
	clone, err := s.projectRedeployer.Prepare(ctx, tx, deployments.StaffRedeploy{
		OrganizationID:     state.OrganizationID,
		ProjectID:          state.ProjectID,
		SourceDeploymentID: state.ActiveDeploymentID,
		Actor:              actor,
		ActorDisplayName:   actorDisplayName,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare project redeploy").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit project redeploy").LogError(ctx, logger)
	}

	cloneID, err := uuid.Parse(clone.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse cloned deployment id").LogError(ctx, logger)
	}
	if err := s.projectRedeployer.Dispatch(ctx, state.ProjectID, cloneID, state.ActiveDeploymentID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "start project redeploy").LogError(ctx, logger)
	}

	return &gen.AdminProjectRedeployResult{
		ProjectID:          state.ProjectID.String(),
		SourceDeploymentID: state.ActiveDeploymentID.String(),
		DeploymentID:       clone.ID,
	}, nil
}

package deployments

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// statusCreated is a deployment that has not started processing.
const statusCreated = "created"

// StaffRedeployer redeploys a project for Speakeasy staff, who act across
// organizations and so have no project auth context. It performs no
// authorization: the admin API and Admin MCP authorize staff before calling it.
type StaffRedeployer struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	db          *pgxpool.Pool
	temporalEnv *temporal.Environment
	audit       *audit.Logger
}

// NewStaffRedeployer builds a StaffRedeployer.
func NewStaffRedeployer(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, temporalEnv *temporal.Environment, auditLogger *audit.Logger) *StaffRedeployer {
	return &StaffRedeployer{
		logger:      logger.With(attr.SlogComponent("deployments")),
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/deployments"),
		db:          db,
		temporalEnv: temporalEnv,
		audit:       auditLogger,
	}
}

// StaffRedeploy names the project and deployment staff are redeploying.
type StaffRedeploy struct {
	// OrganizationID owns the project.
	OrganizationID string

	// ProjectID must own SourceDeploymentID.
	ProjectID uuid.UUID

	// SourceDeploymentID is the deployment cloned.
	SourceDeploymentID uuid.UUID

	// Actor is the staff member the audit entry names.
	Actor urn.Principal

	// ActorDisplayName is how the audit entry renders Actor.
	ActorDisplayName *string
}

// Prepare clones the deployment in the caller's transaction. Call Dispatch
// after the transaction commits.
func (r *StaffRedeployer) Prepare(ctx context.Context, dbtx pgx.Tx, req StaffRedeploy) (*types.Deployment, error) {
	logger := r.logger.With(attr.SlogProjectID(req.ProjectID.String()), attr.SlogOrganizationID(req.OrganizationID))
	return prepareRedeploy(ctx, r.tracer, logger, r.audit, dbtx, redeployRequest(req))
}

// Dispatch starts processing a committed clone without waiting for it.
// Promotion only happens while expectedActive is still the active deployment.
// A clone that has already left the created status is left alone.
func (r *StaffRedeployer) Dispatch(ctx context.Context, projectID, deploymentID, expectedActive uuid.UUID) error {
	logger := r.logger.With(attr.SlogProjectID(projectID.String()), attr.SlogDeploymentID(deploymentID.String()))

	dep, err := mv.DescribeDeployment(ctx, logger, repo.New(r.db), mv.ProjectID(projectID), mv.DeploymentID(deploymentID))
	if err != nil {
		return fmt.Errorf("describe cloned deployment: %w", err)
	}
	if dep.Status != statusCreated {
		return nil
	}

	if _, err := startDeploymentWithExpectedActive(ctx, r.temporalEnv, logger, projectID, deploymentID, dep, true, &expectedActive); err != nil {
		return fmt.Errorf("start deployment: %w", err)
	}
	return nil
}

package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// DeploymentProcessingDeps are the dependencies of the activities run by
// ProcessDeploymentWorkflow.
type DeploymentProcessingDeps struct {
	GuardianPolicy    *guardian.Policy
	DB                *pgxpool.Pool
	FeatureProvider   feature.Provider
	AssetStorage      assets.BlobStore
	EncryptionClient  *encryption.Client
	FunctionsDeployer functions.Deployer
	MCPRegistryClient *externalmcp.RegistryClient
	BillingRepository billing.Repository
}

// NewDeploymentProcessingWorker builds a worker that runs only
// ProcessDeploymentWorkflow on env's queue. Test suites use it to drive
// deployments without wiring the dependencies of the whole fleet.
func NewDeploymentProcessingWorker(
	env *tenv.Environment,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	deps DeploymentProcessingDeps,
) (*Workers, error) {
	validator, err := mcpregistry.LoadValidator()
	if err != nil {
		return nil, fmt.Errorf("load catalog validator: %w", err)
	}
	catalog := externalmcp.NewCatalogService(deps.DB, deps.MCPRegistryClient, externalmcp.NewNativeRegistryReader(mcpregistry.New(deps.DB, validator)), deps.FeatureProvider)

	w := worker.New(env.Client(), string(env.Queue()), worker.Options{
		Interceptors:      newWorkerInterceptors(),
		WorkerStopTimeout: workerStopTimeout,
	})
	w.RegisterActivity(&deploymentActivities{
		transitionDeployment:     activities.NewTransitionDeployment(logger, deps.DB),
		validateDeployment:       activities.NewValidateDeployment(logger, deps.DB, deps.BillingRepository),
		processDeployment:        activities.NewProcessDeployment(logger, tracerProvider, meterProvider, deps.GuardianPolicy, deps.DB, deps.FeatureProvider, deps.AssetStorage, deps.BillingRepository, catalog),
		provisionFunctionsAccess: activities.NewProvisionFunctionsAccess(logger, deps.DB, deps.EncryptionClient),
		deployFunctionRunners:    activities.NewDeployFunctionRunners(logger, deps.DB, deps.FunctionsDeployer, "local", deps.EncryptionClient),
	})
	w.RegisterWorkflow(ProcessDeploymentWorkflow)

	return &Workers{
		main:                w,
		named:               []namedWorker{{name: "main", worker: w}},
		env:                 env,
		logger:              logger,
		db:                  deps.DB,
		schedules:           scheduleConfig{StartupSeeds: nil, AssistantRuntimeImageRef: "", CustomDomainHealth: false, PluginGeneratorRollout: false},
		networkIngressQueue: "",
	}, nil
}

// RegisterPluginPublishing adds the per-project plugin publish workflows to a
// worker built by NewDeploymentProcessingWorker.
func (w *Workers) RegisterPluginPublishing(publisher activities.PluginPublishClient) {
	w.main.RegisterActivity(&pluginPublishActivities{publisher: activities.NewPluginPublisher(w.logger, w.db, publisher)})
	w.main.RegisterWorkflow(PluginPublishWorkflow)
	w.main.RegisterWorkflow(PluginPublishWorkflowDebounced)
}

// deploymentActivities mirrors the deployment methods of Activities so they
// register under the names ProcessDeploymentWorkflow schedules.
type deploymentActivities struct {
	transitionDeployment     *activities.TransitionDeployment
	validateDeployment       *activities.ValidateDeployment
	processDeployment        *activities.ProcessDeployment
	provisionFunctionsAccess *activities.ProvisionFunctionsAccess
	deployFunctionRunners    *activities.DeployFunctionRunners
}

func (a *deploymentActivities) TransitionDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, status string) (*activities.TransitionDeploymentResult, error) {
	return a.transitionDeployment.Do(ctx, projectID, deploymentID, status)
}

func (a *deploymentActivities) ValidateDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.validateDeployment.Do(ctx, projectID, deploymentID)
}

func (a *deploymentActivities) ProcessDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.processDeployment.Do(ctx, projectID, deploymentID)
}

func (a *deploymentActivities) ProvisionFunctionsAccess(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.provisionFunctionsAccess.Do(ctx, projectID, deploymentID)
}

func (a *deploymentActivities) DeployFunctionRunners(ctx context.Context, req activities.DeployFunctionRunnersRequest) error {
	return a.deployFunctionRunners.Do(ctx, req)
}

// pluginPublishActivities mirrors Activities.PublishPluginProject.
type pluginPublishActivities struct {
	publisher *activities.PluginPublisher
}

func (a *pluginPublishActivities) PublishPluginProject(ctx context.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
	return a.publisher.PublishProject(ctx, input) //nolint:wrapcheck // see Activities.PublishPluginProject
}

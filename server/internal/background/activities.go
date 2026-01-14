package background

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	slacktypes "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
)

type Activities struct {
	collectPlatformUsageMetrics   *activities.CollectPlatformUsageMetrics
	customDomainIngress           *activities.CustomDomainIngress
	fallbackModelUsageTracking    *activities.FallbackModelUsageTracking
	firePlatformUsageMetrics      *activities.FirePlatformUsageMetrics
	freeTierReportingUsageMetrics *activities.FreeTierReportingUsageMetrics
	getAllOrganizations           *activities.GetAllOrganizations
	getSlackProjectContext        *activities.GetSlackProjectContext
	postSlackMessage              *activities.PostSlackMessage
	processDeployment             *activities.ProcessDeployment
	provisionFunctionsAccess      *activities.ProvisionFunctionsAccess
	deployFunctionRunners         *activities.DeployFunctionRunners
	reapFlyApps                   *activities.ReapFlyApps
	refreshBillingUsage           *activities.RefreshBillingUsage
	refreshOpenRouterKey          *activities.RefreshOpenRouterKey
	slackChatCompletion           *activities.SlackChatCompletion
	transitionDeployment          *activities.TransitionDeployment
	validateDeployment            *activities.ValidateDeployment
	verifyCustomDomain            *activities.VerifyCustomDomain
	generateToolsetEmbeddings     *activities.GenerateToolsetEmbeddings
	preprocessAgentsInput         *activities.PreprocessAgentsInput
	executeToolCall               *activities.ExecuteToolCall
	executeModelCall              *activities.ExecuteModelCall
	loadAgentTools                *activities.LoadAgentTools
	recordAgentExecution          *activities.RecordAgentExecution
}

func NewActivities(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	encryption *encryption.Client,
	features feature.Provider,
	assetStorage assets.BlobStore,
	slackClient *slack_client.SlackClient,
	chatClient *chat.ChatClient,
	openrouterProvisioner openrouter.Provisioner,
	k8sClient *k8s.KubernetesClients,
	expectedTargetCNAME string,
	billingTracker billing.Tracker,
	billingRepo billing.Repository,
	posthogClient *posthog.Posthog,
	functionsDeployer functions.Deployer,
	functionsVersion functions.RunnerVersion,
	ragService *rag.ToolsetVectorStore,
	agentsService *agents.Service,
	mcpRegistryClient *externalmcp.RegistryClient,
	temporalClient client.Client,
) *Activities {
	return &Activities{
		collectPlatformUsageMetrics:   activities.NewCollectPlatformUsageMetrics(logger, db),
		customDomainIngress:           activities.NewCustomDomainIngress(logger, db, k8sClient),
		fallbackModelUsageTracking:    activities.NewFallbackModelUsageTracking(logger, openrouterProvisioner),
		firePlatformUsageMetrics:      activities.NewFirePlatformUsageMetrics(logger, billingTracker),
		freeTierReportingUsageMetrics: activities.NewFreeTierReportingMetrics(logger, db, billingRepo, posthogClient),
		getAllOrganizations:           activities.NewGetAllOrganizations(logger, db),
		getSlackProjectContext:        activities.NewSlackProjectContextActivity(logger, db, slackClient),
		postSlackMessage:              activities.NewPostSlackMessageActivity(logger, slackClient),
		processDeployment:             activities.NewProcessDeployment(logger, tracerProvider, meterProvider, db, features, assetStorage, billingRepo, mcpRegistryClient),
		provisionFunctionsAccess:      activities.NewProvisionFunctionsAccess(logger, db, encryption),
		deployFunctionRunners:         activities.NewDeployFunctionRunners(logger, db, functionsDeployer, functionsVersion, encryption),
		reapFlyApps:                   activities.NewReapFlyApps(logger, meterProvider, db, functionsDeployer, 3),
		refreshBillingUsage:           activities.NewRefreshBillingUsage(logger, db, billingRepo),
		refreshOpenRouterKey:          activities.NewRefreshOpenRouterKey(logger, db, openrouterProvisioner),
		slackChatCompletion:           activities.NewSlackChatCompletionActivity(logger, slackClient, chatClient),
		transitionDeployment:          activities.NewTransitionDeployment(logger, db),
		validateDeployment:            activities.NewValidateDeployment(logger, db, billingRepo),
		verifyCustomDomain:            activities.NewVerifyCustomDomain(logger, db, expectedTargetCNAME),
		generateToolsetEmbeddings:     activities.NewGenerateToolsetEmbeddingsActivity(tracerProvider, db, ragService, logger),
		preprocessAgentsInput:         activities.NewPreprocessAgentsInput(logger, agentsService, temporalClient),
		executeToolCall:               activities.NewExecuteToolCall(logger, agentsService),
		executeModelCall:              activities.NewExecuteModelCall(logger, agentsService),
		loadAgentTools:                activities.NewLoadAgentTools(logger, agentsService),
		recordAgentExecution:          activities.NewRecordAgentExecution(logger, db),
	}
}

func (a *Activities) TransitionDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, status string) (*activities.TransitionDeploymentResult, error) {
	return a.transitionDeployment.Do(ctx, projectID, deploymentID, status)
}

func (a *Activities) ProcessDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.processDeployment.Do(ctx, projectID, deploymentID)
}

func (a *Activities) GetSlackProjectContext(ctx context.Context, event slacktypes.SlackEvent) (*activities.SlackProjectContextResponse, error) {
	return a.getSlackProjectContext.Do(ctx, event)
}

func (a *Activities) PostSlackMessage(ctx context.Context, input activities.PostSlackMessageInput) error {
	return a.postSlackMessage.Do(ctx, input)
}

func (a *Activities) SlackChatCompletion(ctx context.Context, input activities.SlackChatCompletionInput) (string, error) {
	return a.slackChatCompletion.Do(ctx, input)
}

func (a *Activities) RefreshOpenRouterKey(ctx context.Context, input activities.RefreshOpenRouterKeyArgs) error {
	return a.refreshOpenRouterKey.Do(ctx, input)
}

func (a *Activities) VerifyCustomDomain(ctx context.Context, input activities.VerifyCustomDomainArgs) error {
	return a.verifyCustomDomain.Do(ctx, input)
}

func (a *Activities) CustomDomainIngress(ctx context.Context, input activities.CustomDomainIngressArgs) error {
	return a.customDomainIngress.Do(ctx, input)
}

func (a *Activities) CollectPlatformUsageMetrics(ctx context.Context) ([]activities.PlatformUsageMetrics, error) {
	return a.collectPlatformUsageMetrics.Do(ctx)
}

func (a *Activities) FirePlatformUsageMetrics(ctx context.Context, metrics []activities.PlatformUsageMetrics) error {
	return a.firePlatformUsageMetrics.Do(ctx, metrics)
}

func (a *Activities) FreeTierReportingUsageMetrics(ctx context.Context, orgIDs []string) error {
	return a.freeTierReportingUsageMetrics.Do(ctx, orgIDs)
}

func (a *Activities) RefreshBillingUsage(ctx context.Context, orgIDs []string) error {
	return a.refreshBillingUsage.Do(ctx, orgIDs)
}

func (a *Activities) GetAllOrganizations(ctx context.Context) ([]string, error) {
	return a.getAllOrganizations.Do(ctx)
}

func (a *Activities) ProvisionFunctionsAccess(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.provisionFunctionsAccess.Do(ctx, projectID, deploymentID)
}

func (a *Activities) DeployFunctionRunners(ctx context.Context, req activities.DeployFunctionRunnersRequest) error {
	return a.deployFunctionRunners.Do(ctx, req)
}

func (a *Activities) ValidateDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.validateDeployment.Do(ctx, projectID, deploymentID)
}

func (a *Activities) GenerateToolsetEmbeddings(ctx context.Context, input activities.GenerateToolsetEmbeddingsInput) error {
	return a.generateToolsetEmbeddings.Do(ctx, input)
}

func (a *Activities) ReapFlyApps(ctx context.Context, req activities.ReapFlyAppsRequest) (*activities.ReapFlyAppsResult, error) {
	return a.reapFlyApps.Do(ctx, req)
}

func (a *Activities) PreprocessAgentsInput(ctx context.Context, input activities.PreprocessAgentsInputInput) (*activities.PreprocessAgentsInputOutput, error) {
	return a.preprocessAgentsInput.Do(ctx, input)
}

func (a *Activities) ExecuteToolCall(ctx context.Context, input activities.ExecuteToolCallInput) (*activities.ExecuteToolCallOutput, error) {
	return a.executeToolCall.Do(ctx, input)
}

func (a *Activities) ExecuteModelCall(ctx context.Context, input activities.ExecuteModelCallInput) (*activities.ExecuteModelCallOutput, error) {
	return a.executeModelCall.Do(ctx, input)
}

func (a *Activities) LoadAgentTools(ctx context.Context, input activities.LoadAgentToolsInput) (*activities.LoadAgentToolsOutput, error) {
	return a.loadAgentTools.Do(ctx, input)
}

func (a *Activities) FallbackModelUsageTracking(ctx context.Context, input activities.FallbackModelUsageTrackingArgs) error {
	return a.fallbackModelUsageTracking.Do(ctx, input)
}

func (a *Activities) RecordAgentExecution(ctx context.Context, input activities.RecordAgentExecutionInput) error {
	return a.recordAgentExecution.Do(ctx, input)
}

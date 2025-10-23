package background

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
)

type Activities struct {
	collectPlatformUsageMetrics   *activities.CollectPlatformUsageMetrics
	customDomainIngress           *activities.CustomDomainIngress
	firePlatformUsageMetrics      *activities.FirePlatformUsageMetrics
	freeTierReportingUsageMetrics *activities.FreeTierReportingUsageMetrics
	getAllOrganizations           *activities.GetAllOrganizations
	getSlackProjectContext        *activities.GetSlackProjectContext
	postSlackMessage              *activities.PostSlackMessage
	processDeployment             *activities.ProcessDeployment
	provisionFunctionsAccess      *activities.ProvisionFunctionsAccess
	deployFunctionRunners         *activities.DeployFunctionRunners
	refreshBillingUsage           *activities.RefreshBillingUsage
	refreshOpenRouterKey          *activities.RefreshOpenRouterKey
	slackChatCompletion           *activities.SlackChatCompletion
	transitionDeployment          *activities.TransitionDeployment
	verifyCustomDomain            *activities.VerifyCustomDomain
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
	openrouter openrouter.Provisioner,
	k8sClient *k8s.KubernetesClients,
	expectedTargetCNAME string,
	billingTracker billing.Tracker,
	billingRepo billing.Repository,
	posthogClient *posthog.Posthog,
	functionsDeployer functions.Deployer,
	functionsVersion functions.RunnerVersion,
) *Activities {
	return &Activities{
		collectPlatformUsageMetrics:   activities.NewCollectPlatformUsageMetrics(logger, db),
		customDomainIngress:           activities.NewCustomDomainIngress(logger, db, k8sClient),
		firePlatformUsageMetrics:      activities.NewFirePlatformUsageMetrics(logger, billingTracker),
		freeTierReportingUsageMetrics: activities.NewFreeTierReportingMetrics(logger, db, billingRepo, posthogClient),
		getAllOrganizations:           activities.NewGetAllOrganizations(logger, db),
		getSlackProjectContext:        activities.NewSlackProjectContextActivity(logger, db, slackClient),
		postSlackMessage:              activities.NewPostSlackMessageActivity(logger, slackClient),
		processDeployment:             activities.NewProcessDeployment(logger, tracerProvider, meterProvider, db, features, assetStorage),
		provisionFunctionsAccess:      activities.NewProvisionFunctionsAccess(logger, db, encryption),
		deployFunctionRunners:         activities.NewDeployFunctionRunners(logger, db, functionsDeployer, functionsVersion, encryption, billingRepo),
		refreshBillingUsage:           activities.NewRefreshBillingUsage(logger, db, billingRepo),
		refreshOpenRouterKey:          activities.NewRefreshOpenRouterKey(logger, db, openrouter),
		slackChatCompletion:           activities.NewSlackChatCompletionActivity(logger, slackClient, chatClient),
		transitionDeployment:          activities.NewTransitionDeployment(logger, db),
		verifyCustomDomain:            activities.NewVerifyCustomDomain(logger, db, expectedTargetCNAME),
	}
}

func (a *Activities) TransitionDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, status string) (*activities.TransitionDeploymentResult, error) {
	return a.transitionDeployment.Do(ctx, projectID, deploymentID, status)
}

func (a *Activities) ProcessDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.processDeployment.Do(ctx, projectID, deploymentID)
}

func (a *Activities) GetSlackProjectContext(ctx context.Context, event types.SlackEvent) (*activities.SlackProjectContextResponse, error) {
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

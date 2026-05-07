package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	resolution_activities "github.com/speakeasy-api/gram/server/internal/background/activities/chat_resolutions"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	slacktypes "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
	"github.com/workos/workos-go/v6/pkg/events"
)

type Activities struct {
	collectPlatformUsageMetrics     *activities.CollectPlatformUsageMetrics
	customDomainIngress             *activities.CustomDomainIngress
	fallbackModelUsageTracking      *activities.FallbackModelUsageTracking
	firePlatformUsageMetrics        *activities.FirePlatformUsageMetrics
	freeTierReportingUsageMetrics   *activities.FreeTierReportingUsageMetrics
	generateChatTitle               *activities.GenerateChatTitle
	getAllOrganizations             *activities.GetAllOrganizations
	getSlackProjectContext          *activities.GetSlackProjectContext
	postSlackMessage                *activities.PostSlackMessage
	processDeployment               *activities.ProcessDeployment
	provisionFunctionsAccess        *activities.ProvisionFunctionsAccess
	deployFunctionRunners           *activities.DeployFunctionRunners
	reapFlyApps                     *activities.ReapFlyApps
	refreshBillingUsage             *activities.RefreshBillingUsage
	refreshOpenRouterKey            *activities.RefreshOpenRouterKey
	slackChatCompletion             *activities.SlackChatCompletion
	transitionDeployment            *activities.TransitionDeployment
	validateDeployment              *activities.ValidateDeployment
	verifyCustomDomain              *activities.VerifyCustomDomain
	generateToolsetEmbeddings       *activities.GenerateToolsetEmbeddings
	dispatchTrigger                 *activities.DispatchTrigger
	processScheduledTrigger         *activities.ProcessScheduledTrigger
	segmentChat                     *resolution_activities.SegmentChat
	deleteChatResolutions           *resolution_activities.DeleteChatResolutions
	analyzeSegment                  *resolution_activities.AnalyzeSegment
	getUserFeedbackForChat          *resolution_activities.GetUserFeedbackForChat
	fetchUnanalyzedMessages         *risk_analysis.FetchUnanalyzed
	analyzeBatch                    *risk_analysis.AnalyzeBatch
	admitAssistantThreads           *activities.AdmitAssistantThreads
	processAssistantThread          *activities.ProcessAssistantThread
	expireAssistantThreadRuntime    *activities.ExpireAssistantThreadRuntime
	reapStuckAssistantRuntimes      *activities.ReapStuckAssistantRuntimes
	reapInactiveAssistantRuntimes   *activities.ReapInactiveAssistantRuntimes
	signalAssistantCoordinator      *activities.SignalAssistantCoordinator
	signalAssistantThread           *activities.SignalAssistantThread
	processWorkOSOrganizationEvents *activities.ProcessWorkOSOrganizationEvents
}

func NewActivities(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	encryption *encryption.Client,
	features feature.Provider,
	assetStorage assets.BlobStore,
	slackClient *slack_client.SlackClient,
	openrouterProvisioner openrouter.Provisioner,
	chatClient *chat.Client,
	k8sClient *k8s.KubernetesClients,
	expectedTargetCNAME string,
	billingTracker billing.Tracker,
	billingRepo billing.Repository,
	posthogClient *posthog.Posthog,
	functionsDeployer functions.Deployer,
	functionsVersion functions.RunnerVersion,
	ragService *rag.ToolsetVectorStore,
	mcpRegistryClient *externalmcp.RegistryClient,
	temporalEnv *tenv.Environment,
	telemetryLogger *telemetry.Logger,
	triggerApp *bgtriggers.App,
	cacheAdapter cache.Cache,
	assistantsCore *assistants.ServiceCore,
	piiScanner risk_analysis.PIIScanner,
	shadowMCPClient *shadowmcp.Client,
	auditLogger *audit.Logger,
	workosEventsClient *events.Client,
) *Activities {
	usageTrackingStrategy := chat.NewDefaultUsageTrackingStrategy(db, logger, openrouterProvisioner, billingTracker, nil)

	// Only construct the WorkOS sync activity when the events client is
	// configured. Local dev without a key passes nil; the wrapper method below
	// returns a clear error if the activity is invoked unconfigured.
	var processWorkOSOrgEvents *activities.ProcessWorkOSOrganizationEvents
	if workosEventsClient != nil {
		processWorkOSOrgEvents = activities.NewProcessWorkOSOrganizationEvents(logger, db, workosEventsClient)
	}

	return &Activities{
		collectPlatformUsageMetrics:     activities.NewCollectPlatformUsageMetrics(logger, db),
		customDomainIngress:             activities.NewCustomDomainIngress(logger, db, k8sClient),
		fallbackModelUsageTracking:      activities.NewFallbackModelUsageTracking(usageTrackingStrategy),
		firePlatformUsageMetrics:        activities.NewFirePlatformUsageMetrics(logger, billingTracker),
		freeTierReportingUsageMetrics:   activities.NewFreeTierReportingMetrics(logger, db, billingRepo, posthogClient),
		generateChatTitle:               activities.NewGenerateChatTitle(logger, db, chatClient),
		getAllOrganizations:             activities.NewGetAllOrganizations(logger, db),
		getSlackProjectContext:          activities.NewSlackProjectContextActivity(logger, db, slackClient),
		postSlackMessage:                activities.NewPostSlackMessageActivity(logger, slackClient),
		processDeployment:               activities.NewProcessDeployment(logger, tracerProvider, meterProvider, guardianPolicy, db, features, assetStorage, billingRepo, mcpRegistryClient),
		provisionFunctionsAccess:        activities.NewProvisionFunctionsAccess(logger, db, encryption),
		deployFunctionRunners:           activities.NewDeployFunctionRunners(logger, db, functionsDeployer, functionsVersion, encryption),
		reapFlyApps:                     activities.NewReapFlyApps(logger, meterProvider, db, functionsDeployer, 1),
		refreshBillingUsage:             activities.NewRefreshBillingUsage(logger, db, billingRepo),
		refreshOpenRouterKey:            activities.NewRefreshOpenRouterKey(logger, db, openrouterProvisioner),
		slackChatCompletion:             activities.NewSlackChatCompletionActivity(logger, slackClient, chatClient),
		transitionDeployment:            activities.NewTransitionDeployment(logger, db),
		validateDeployment:              activities.NewValidateDeployment(logger, db, billingRepo),
		verifyCustomDomain:              activities.NewVerifyCustomDomain(logger, db, auditLogger, expectedTargetCNAME),
		generateToolsetEmbeddings:       activities.NewGenerateToolsetEmbeddingsActivity(tracerProvider, db, ragService, logger),
		dispatchTrigger:                 activities.NewDispatchTrigger(triggerApp),
		processScheduledTrigger:         activities.NewProcessScheduledTrigger(triggerApp),
		segmentChat:                     resolution_activities.NewSegmentChat(logger, db, chatClient),
		deleteChatResolutions:           resolution_activities.NewDeleteChatResolutions(db),
		analyzeSegment:                  resolution_activities.NewAnalyzeSegment(logger, db, chatClient, telemetryLogger),
		getUserFeedbackForChat:          resolution_activities.NewGetUserFeedbackForChat(db),
		fetchUnanalyzedMessages:         risk_analysis.NewFetchUnanalyzed(logger, tracerProvider, db),
		analyzeBatch:                    risk_analysis.NewAnalyzeBatch(logger, tracerProvider, meterProvider, db, piiScanner, shadowMCPClient),
		admitAssistantThreads:           activities.NewAdmitAssistantThreads(assistantsCore),
		processAssistantThread:          activities.NewProcessAssistantThread(assistantsCore),
		expireAssistantThreadRuntime:    activities.NewExpireAssistantThreadRuntime(assistantsCore),
		reapStuckAssistantRuntimes:      activities.NewReapStuckAssistantRuntimes(assistantsCore),
		reapInactiveAssistantRuntimes:   activities.NewReapInactiveAssistantRuntimes(logger, assistantsCore),
		signalAssistantCoordinator:      activities.NewSignalAssistantCoordinator(&AssistantWorkflowSignaler{TemporalEnv: temporalEnv}),
		signalAssistantThread:           activities.NewSignalAssistantThread(&AssistantWorkflowSignaler{TemporalEnv: temporalEnv}),
		processWorkOSOrganizationEvents: processWorkOSOrgEvents,
	}
}

func (a *Activities) ProcessWorkOSOrganizationEvents(ctx context.Context, params activities.ProcessWorkOSOrganizationEventsParams) (*activities.ProcessWorkOSOrganizationEventsResult, error) {
	if a.processWorkOSOrganizationEvents == nil {
		return nil, fmt.Errorf("WorkOS events client is not configured")
	}
	return a.processWorkOSOrganizationEvents.Do(ctx, params)
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

func (a *Activities) FallbackModelUsageTracking(ctx context.Context, input activities.FallbackModelUsageTrackingArgs) error {
	return a.fallbackModelUsageTracking.Do(ctx, input)
}

func (a *Activities) GenerateChatTitle(ctx context.Context, input activities.GenerateChatTitleArgs) error {
	return a.generateChatTitle.Do(ctx, input)
}

func (a *Activities) SegmentChat(ctx context.Context, input resolution_activities.SegmentChatArgs) (*resolution_activities.SegmentChatOutput, error) {
	out, err := a.segmentChat.Do(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("segment chat: %w", err)
	}
	return out, nil
}

func (a *Activities) DeleteChatResolutions(ctx context.Context, input resolution_activities.DeleteChatResolutionsArgs) error {
	if err := a.deleteChatResolutions.Do(ctx, input); err != nil {
		return fmt.Errorf("delete chat resolutions: %w", err)
	}
	return nil
}

func (a *Activities) AnalyzeSegment(ctx context.Context, input resolution_activities.AnalyzeSegmentArgs) error {
	if err := a.analyzeSegment.Do(ctx, input); err != nil {
		return fmt.Errorf("analyze segment: %w", err)
	}
	return nil
}

func (a *Activities) GetUserFeedbackForChat(ctx context.Context, input resolution_activities.GetUserFeedbackForChatArgs) (*resolution_activities.GetUserFeedbackForChatResult, error) {
	result, err := a.getUserFeedbackForChat.Do(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get user feedback for chat: %w", err)
	}
	return result, nil
}

func (a *Activities) DispatchTrigger(ctx context.Context, input activities.DispatchTriggerInput) error {
	return a.dispatchTrigger.Do(ctx, input)
}

func (a *Activities) ProcessScheduledTrigger(ctx context.Context, input activities.ProcessScheduledTriggerInput) (*activities.ProcessScheduledTriggerResult, error) {
	return a.processScheduledTrigger.Do(ctx, input)
}

func (a *Activities) FetchUnanalyzedMessages(ctx context.Context, input risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
	result, err := a.fetchUnanalyzedMessages.Do(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed messages: %w", err)
	}
	return result, nil
}

func (a *Activities) AnalyzeBatch(ctx context.Context, input risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
	if a.analyzeBatch == nil {
		return nil, fmt.Errorf("analyze batch: gitleaks detector pool not initialized")
	}
	result, err := a.analyzeBatch.Do(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("analyze batch: %w", err)
	}
	return result, nil
}

func (a *Activities) AdmitAssistantThreads(ctx context.Context, input activities.AdmitAssistantThreadsInput) (*activities.AdmitAssistantThreadsResult, error) {
	return a.admitAssistantThreads.Do(ctx, input)
}

func (a *Activities) ProcessAssistantThread(ctx context.Context, input activities.ProcessAssistantThreadInput) (*activities.ProcessAssistantThreadResult, error) {
	return a.processAssistantThread.Do(ctx, input)
}

func (a *Activities) ExpireAssistantThreadRuntime(ctx context.Context, input activities.ExpireAssistantThreadRuntimeInput) (*activities.ExpireAssistantThreadRuntimeResult, error) {
	return a.expireAssistantThreadRuntime.Do(ctx, input)
}

func (a *Activities) ReapStuckAssistantRuntimes(ctx context.Context) (*activities.ReapStuckAssistantRuntimesResult, error) {
	return a.reapStuckAssistantRuntimes.Do(ctx)
}

func (a *Activities) ReapInactiveAssistantRuntimes(ctx context.Context, req activities.ReapInactiveAssistantRuntimesRequest) (*activities.ReapInactiveAssistantRuntimesResult, error) {
	return a.reapInactiveAssistantRuntimes.Do(ctx, req)
}

func (a *Activities) SignalAssistantCoordinator(ctx context.Context, input activities.SignalAssistantCoordinatorInput) error {
	return a.signalAssistantCoordinator.Do(ctx, input)
}

func (a *Activities) SignalAssistantThread(ctx context.Context, input activities.SignalAssistantThreadInput) error {
	return a.signalAssistantThread.Do(ctx, input)
}

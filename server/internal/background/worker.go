package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	svix "github.com/svix/svix-webhooks/go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/background/interceptors"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type WorkerOptions struct {
	GuardianPolicy      *guardian.Policy
	DB                  *pgxpool.Pool
	EncryptionClient    *encryption.Client
	FeatureProvider     feature.Provider
	AssetStorage        assets.BlobStore
	SlackClient         *slack_client.SlackClient
	ChatClient          *chat.Client
	OpenRouter          openrouter.Provisioner
	K8sClient           *k8s.KubernetesClients
	ExpectedTargetCNAME string
	BillingTracker      billing.Tracker
	BillingRepository   billing.Repository
	RedisClient         *redis.Client
	CacheAdapter        cache.Cache
	PosthogClient       *posthog.Posthog
	FunctionsDeployer   functions.Deployer
	FunctionsVersion    functions.RunnerVersion
	RagService          *rag.ToolsetVectorStore
	MCPRegistryClient   *externalmcp.RegistryClient
	TelemetryLogger     *telemetry.Logger
	TriggersApp         *bgtriggers.App
	AssistantsCore      *assistants.ServiceCore
	TemporalEnv         *tenv.Environment
	PIIScanner          risk_analysis.PIIScanner
	PIScanner           *risk_analysis.PromptInjectionScanner
	ShadowMCPClient     *shadowmcp.Client
	AuditLogger         *audit.Logger
	WorkOSClient        activities.WorkOSClient
	SvixClient          *svix.Svix
	ProductFeatures     *productfeatures.Client
}

func ForDeploymentProcessing(
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	f feature.Provider,
	assetStorage assets.BlobStore,
	enc *encryption.Client,
	deployer functions.Deployer,
	mcpRegistryClient *externalmcp.RegistryClient,
	auditLogger *audit.Logger,
) *WorkerOptions {
	return &WorkerOptions{
		DB:                  db,
		GuardianPolicy:      guardianPolicy,
		EncryptionClient:    enc,
		FeatureProvider:     f,
		AssetStorage:        assetStorage,
		FunctionsDeployer:   deployer,
		FunctionsVersion:    "local", // Test deployers don't use baked versions
		MCPRegistryClient:   mcpRegistryClient,
		AuditLogger:         auditLogger,
		SlackClient:         nil,
		ChatClient:          nil,
		OpenRouter:          nil,
		K8sClient:           nil,
		ExpectedTargetCNAME: "",
		BillingTracker:      nil,
		BillingRepository:   nil,
		RagService:          nil,
		RedisClient:         nil,
		PosthogClient:       nil,
		TelemetryLogger:     nil,
		TriggersApp:         nil,
		CacheAdapter:        nil,
		AssistantsCore:      nil,
		TemporalEnv:         nil,
		PIIScanner:          nil,
		PIScanner:           nil,
		ShadowMCPClient:     nil,
		WorkOSClient:        workos.NewStubClient(),
		SvixClient:          nil,
		ProductFeatures:     nil,
	}
}

func NewTemporalWorker(
	env *tenv.Environment,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	options ...*WorkerOptions,
) *Workers {
	opts := &WorkerOptions{
		GuardianPolicy:      nil,
		DB:                  nil,
		EncryptionClient:    nil,
		FeatureProvider:     nil,
		AssetStorage:        nil,
		SlackClient:         nil,
		ChatClient:          nil,
		OpenRouter:          nil,
		K8sClient:           nil,
		ExpectedTargetCNAME: "",
		BillingTracker:      nil,
		BillingRepository:   nil,
		RedisClient:         nil,
		PosthogClient:       nil,
		FunctionsDeployer:   nil,
		FunctionsVersion:    "",
		RagService:          nil,
		MCPRegistryClient:   nil,
		TelemetryLogger:     nil,
		TriggersApp:         nil,
		CacheAdapter:        nil,
		AssistantsCore:      nil,
		TemporalEnv:         env,
		PIIScanner:          nil,
		PIScanner:           nil,
		ShadowMCPClient:     nil,
		AuditLogger:         nil,
		WorkOSClient:        workos.NewStubClient(),
		SvixClient:          nil,
		ProductFeatures:     nil,
	}

	for _, o := range options {
		opts = &WorkerOptions{
			GuardianPolicy:      conv.Default(o.GuardianPolicy, opts.GuardianPolicy),
			DB:                  conv.Default(o.DB, opts.DB),
			EncryptionClient:    conv.Default(o.EncryptionClient, opts.EncryptionClient),
			FeatureProvider:     conv.Default(o.FeatureProvider, opts.FeatureProvider),
			AssetStorage:        conv.Default(o.AssetStorage, opts.AssetStorage),
			SlackClient:         conv.Default(o.SlackClient, opts.SlackClient),
			OpenRouter:          conv.Default(o.OpenRouter, opts.OpenRouter),
			ChatClient:          conv.Default(o.ChatClient, opts.ChatClient),
			K8sClient:           conv.Default(o.K8sClient, opts.K8sClient),
			ExpectedTargetCNAME: conv.Default(o.ExpectedTargetCNAME, opts.ExpectedTargetCNAME),
			BillingTracker:      conv.Default(o.BillingTracker, opts.BillingTracker),
			BillingRepository:   conv.Default(o.BillingRepository, opts.BillingRepository),
			RedisClient:         conv.Default(o.RedisClient, opts.RedisClient),
			PosthogClient:       conv.Default(o.PosthogClient, opts.PosthogClient),
			FunctionsDeployer:   conv.Default(o.FunctionsDeployer, opts.FunctionsDeployer),
			FunctionsVersion:    conv.Default(o.FunctionsVersion, opts.FunctionsVersion),
			RagService:          conv.Default(o.RagService, opts.RagService),
			MCPRegistryClient:   conv.Default(o.MCPRegistryClient, opts.MCPRegistryClient),
			TelemetryLogger:     conv.Default(o.TelemetryLogger, opts.TelemetryLogger),
			TriggersApp:         conv.Default(o.TriggersApp, opts.TriggersApp),
			CacheAdapter:        conv.Default(o.CacheAdapter, opts.CacheAdapter),
			AssistantsCore:      conv.Default(o.AssistantsCore, opts.AssistantsCore),
			TemporalEnv:         conv.Default(o.TemporalEnv, opts.TemporalEnv),
			PIIScanner:          conv.Default(o.PIIScanner, opts.PIIScanner),
			PIScanner:           conv.Default(o.PIScanner, opts.PIScanner),
			ShadowMCPClient:     conv.Default(o.ShadowMCPClient, opts.ShadowMCPClient),
			AuditLogger:         conv.Default(o.AuditLogger, opts.AuditLogger),
			WorkOSClient:        conv.Default(o.WorkOSClient, opts.WorkOSClient),
			SvixClient:          conv.Default(o.SvixClient, opts.SvixClient),
			ProductFeatures:     conv.Default(o.ProductFeatures, opts.ProductFeatures),
		}
	}

	workerInterceptors := []interceptor.WorkerInterceptor{
		&interceptors.Recovery{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
		&interceptors.InjectExecutionInfo{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
		&interceptors.Logging{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
	}

	temporalWorker := worker.New(env.Client(), string(env.Queue()), worker.Options{
		Interceptors: workerInterceptors,
	})

	riskWorker := worker.New(env.Client(), RiskAnalysisTaskQueue(env.Queue()), worker.Options{
		Interceptors:                       workerInterceptors,
		MaxConcurrentActivityExecutionSize: perPodAnalyzeBatchConcurrency,
	})

	activities := NewActivities(
		logger,
		tracerProvider,
		meterProvider,
		opts.GuardianPolicy,
		opts.DB,
		opts.EncryptionClient,
		opts.FeatureProvider,
		opts.AssetStorage,
		opts.SlackClient,
		opts.OpenRouter,
		opts.ChatClient,
		opts.K8sClient,
		opts.ExpectedTargetCNAME,
		opts.BillingTracker,
		opts.BillingRepository,
		opts.PosthogClient,
		opts.FunctionsDeployer,
		opts.FunctionsVersion,
		opts.RagService,
		opts.MCPRegistryClient,
		opts.TemporalEnv,
		opts.TelemetryLogger,
		opts.TriggersApp,
		opts.CacheAdapter,
		opts.AssistantsCore,
		opts.PIIScanner,
		opts.PIScanner,
		opts.ShadowMCPClient,
		opts.AuditLogger,
		opts.WorkOSClient,
		opts.SvixClient,
		opts.ProductFeatures,
	)

	temporalWorker.RegisterActivity(activities.ProcessDeployment)
	temporalWorker.RegisterActivity(activities.TransitionDeployment)
	temporalWorker.RegisterActivity(activities.ProvisionFunctionsAccess)
	temporalWorker.RegisterActivity(activities.DeployFunctionRunners)
	temporalWorker.RegisterActivity(activities.AutoSyncToolsets)
	temporalWorker.RegisterActivity(activities.ReapFlyApps)
	temporalWorker.RegisterActivity(activities.GetSlackProjectContext)
	temporalWorker.RegisterActivity(activities.PostSlackMessage)
	temporalWorker.RegisterActivity(activities.SlackChatCompletion)
	temporalWorker.RegisterActivity(activities.RefreshOpenRouterKey)
	temporalWorker.RegisterActivity(activities.VerifyCustomDomain)
	temporalWorker.RegisterActivity(activities.CustomDomainIngress)
	temporalWorker.RegisterActivity(activities.CollectPlatformUsageMetrics)
	temporalWorker.RegisterActivity(activities.FirePlatformUsageMetrics)
	temporalWorker.RegisterActivity(activities.FreeTierReportingUsageMetrics)
	temporalWorker.RegisterActivity(activities.RefreshBillingUsage)
	temporalWorker.RegisterActivity(activities.GetAllOrganizations)
	temporalWorker.RegisterActivity(activities.ValidateDeployment)
	temporalWorker.RegisterActivity(activities.GenerateToolsetEmbeddings)
	temporalWorker.RegisterActivity(activities.FallbackModelUsageTracking)
	temporalWorker.RegisterActivity(activities.GenerateChatTitle)
	temporalWorker.RegisterActivity(activities.SegmentChat)
	temporalWorker.RegisterActivity(activities.DeleteChatResolutions)
	temporalWorker.RegisterActivity(activities.AnalyzeSegment)
	temporalWorker.RegisterActivity(activities.GetUserFeedbackForChat)
	// Trigger related activities
	temporalWorker.RegisterActivity(activities.DispatchTrigger)
	temporalWorker.RegisterActivity(activities.ProcessScheduledTrigger)
	temporalWorker.RegisterActivity(activities.MarkTriggerFired)
	// Risk analysis activities — AnalyzeBatch on the dedicated worker.
	temporalWorker.RegisterActivity(activities.FetchUnanalyzedMessages)
	riskWorker.RegisterActivity(activities.AnalyzeBatch)
	// Assistant activities
	temporalWorker.RegisterActivity(activities.AdmitAssistantThreads)
	temporalWorker.RegisterActivity(activities.ProcessAssistantThread)
	temporalWorker.RegisterActivity(activities.ExpireAssistantThreadRuntime)
	temporalWorker.RegisterActivity(activities.ReapStuckAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.ReapInactiveAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.ReapSoftDeletedAssistantMemories)
	temporalWorker.RegisterActivity(activities.SignalAssistantCoordinator)
	temporalWorker.RegisterActivity(activities.SignalAssistantThread)
	temporalWorker.RegisterActivity(activities.CancelAssistantsSubscription)
	// WorkOS sync activities
	temporalWorker.RegisterActivity(activities.ListWorkOSOrganizations)
	temporalWorker.RegisterActivity(activities.BackfillWorkOSOrganization)
	temporalWorker.RegisterActivity(activities.BackfillWorkOSGlobalRoles)
	temporalWorker.RegisterActivity(activities.ProcessWorkOSOrganizationEvents)
	temporalWorker.RegisterActivity(activities.ProcessWorkOSGlobalRoleEvents)
	temporalWorker.RegisterActivity(activities.ProcessWorkOSUserEvents)
	// Outbox relay activities
	temporalWorker.RegisterActivity(activities.FetchPendingOutboxEvents)
	temporalWorker.RegisterActivity(activities.FilterNoopOutboxEvents)
	temporalWorker.RegisterActivity(activities.RelayOutboxEvents)
	temporalWorker.RegisterActivity(activities.GCOutboxProcessedRows)

	temporalWorker.RegisterWorkflow(ProcessDeploymentWorkflow)
	temporalWorker.RegisterWorkflow(FunctionsReaperWorkflow)
	temporalWorker.RegisterWorkflow(SlackEventWorkflow)
	temporalWorker.RegisterWorkflow(OpenrouterKeyRefreshWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainRegistrationWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainDeletionWorkflow)
	temporalWorker.RegisterWorkflow(CollectPlatformUsageMetricsWorkflow)
	temporalWorker.RegisterWorkflow(RefreshBillingUsageWorkflow)
	temporalWorker.RegisterWorkflow(IndexToolsetWorkflow)
	temporalWorker.RegisterWorkflow(FallbackModelUsageTrackingWorkflow)
	temporalWorker.RegisterWorkflow(GenerateChatTitleWorkflow)
	temporalWorker.RegisterWorkflow(AnalyzeChatResolutionsWorkflow)
	temporalWorker.RegisterWorkflow(DelayedChatResolutionAnalysisWorkflow)
	// Trigger workflows
	temporalWorker.RegisterWorkflow(TriggerCronWorkflow)
	temporalWorker.RegisterWorkflow(TriggerDispatchWorkflow)
	temporalWorker.RegisterWorkflow(TriggerWakeWorkflow)
	// Risk analysis workflow
	temporalWorker.RegisterWorkflow(DrainRiskAnalysisWorkflow)
	temporalWorker.RegisterWorkflow(AssistantCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(AssistantThreadWorkflow)
	temporalWorker.RegisterWorkflow(AssistantReaperWorkflow)
	temporalWorker.RegisterWorkflow(AssistantRuntimeJanitorWorkflow)
	temporalWorker.RegisterWorkflow(AssistantMemoriesReaperWorkflow)
	// WorkOS sync workflows
	temporalWorker.RegisterWorkflow(ProcessWorkOSOrganizationEventsWorkflow)
	temporalWorker.RegisterWorkflow(ProcessWorkOSOrganizationEventsWorkflowDebounced)
	temporalWorker.RegisterWorkflow(ProcessWorkOSGlobalRoleEventsWorkflow)
	temporalWorker.RegisterWorkflow(ProcessWorkOSGlobalRoleEventsWorkflowDebounced)
	temporalWorker.RegisterWorkflow(ProcessWorkOSUserEventsWorkflow)
	temporalWorker.RegisterWorkflow(ProcessWorkOSUserEventsWorkflowDebounced)
	temporalWorker.RegisterWorkflow(BackfillWorkOSWorkflow)
	// Assistants signup followups
	temporalWorker.RegisterWorkflow(CancelAssistantsSubscriptionWorkflow)
	// Outbox -> Relay workflow and GC
	temporalWorker.RegisterWorkflow(ProcessOutboxWorkflow)
	temporalWorker.RegisterWorkflow(OutboxGCWorkflow)

	if err := AddPlatformUsageMetricsSchedule(context.Background(), env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add platform usage metrics schedule", attr.SlogError(err))
		}
	}

	if err := AddRefreshBillingUsageSchedule(context.Background(), env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add refresh billing usage schedule", attr.SlogError(err))
		}
	}

	if err := AddProcessOutboxSchedule(context.Background(), env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add relay outbox to svix schedule", attr.SlogError(err))
		}
	}

	if err := AddAssistantReaperSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add assistant reaper schedule", attr.SlogError(err))
	}

	if err := AddAssistantRuntimeJanitorSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add assistant runtime janitor schedule", attr.SlogError(err))
	}

	if err := AddAssistantMemoriesReaperSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add assistant memories reaper schedule", attr.SlogError(err))
	}

	if err := AddOutboxGCSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add outbox gc schedule", attr.SlogError(err))
	}

	return &Workers{main: temporalWorker, riskAnalysis: riskWorker}
}

// Fleet-wide cap on in-flight AnalyzeBatch per worker pod — the only knob
// in the chain that doesn't multiply with N policies or N batches.
const perPodAnalyzeBatchConcurrency = 20

func RiskAnalysisTaskQueue(mainQueue tenv.TaskQueueName) string {
	return string(mainQueue) + "-risk-analysis"
}

// Workers bundles the main and risk-analysis Temporal workers.
type Workers struct {
	main         worker.Worker
	riskAnalysis worker.Worker
}

// Run starts the risk-analysis worker, then blocks running the main worker
// until interruptCh receives.
func (w *Workers) Run(interruptCh <-chan any) error {
	if err := w.riskAnalysis.Start(); err != nil {
		return fmt.Errorf("start risk analysis worker: %w", err)
	}
	defer w.riskAnalysis.Stop()

	if err := w.main.Run(interruptCh); err != nil {
		return fmt.Errorf("run main worker: %w", err)
	}
	return nil
}

// Start starts both workers without blocking. Pair with Stop (used by tests).
func (w *Workers) Start() error {
	if err := w.main.Start(); err != nil {
		return fmt.Errorf("start main worker: %w", err)
	}
	if err := w.riskAnalysis.Start(); err != nil {
		w.main.Stop()
		return fmt.Errorf("start risk analysis worker: %w", err)
	}
	return nil
}

func (w *Workers) Stop() {
	w.riskAnalysis.Stop()
	w.main.Stop()
}

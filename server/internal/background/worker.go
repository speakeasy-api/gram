package background

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
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
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/workos/workos-go/v6/pkg/events"
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
	ShadowMCPClient     *shadowmcp.Client
	AuditLogger         *audit.Logger
	WorkOSEventsClient  *events.Client
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
		ShadowMCPClient:     nil,
		WorkOSEventsClient:  nil,
	}
}

func NewTemporalWorker(
	env *tenv.Environment,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	options ...*WorkerOptions,
) worker.Worker {
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
		ShadowMCPClient:     nil,
		AuditLogger:         nil,
		WorkOSEventsClient:  nil,
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
			ShadowMCPClient:     conv.Default(o.ShadowMCPClient, opts.ShadowMCPClient),
			AuditLogger:         conv.Default(o.AuditLogger, opts.AuditLogger),
			WorkOSEventsClient:  conv.Default(o.WorkOSEventsClient, opts.WorkOSEventsClient),
		}
	}

	temporalWorker := worker.New(env.Client(), string(env.Queue()), worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{
			&interceptors.Recovery{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
			&interceptors.InjectExecutionInfo{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
			&interceptors.Logging{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
		},
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
		opts.ShadowMCPClient,
		opts.AuditLogger,
		opts.WorkOSEventsClient,
	)

	temporalWorker.RegisterActivity(activities.ProcessDeployment)
	temporalWorker.RegisterActivity(activities.TransitionDeployment)
	temporalWorker.RegisterActivity(activities.ProvisionFunctionsAccess)
	temporalWorker.RegisterActivity(activities.DeployFunctionRunners)
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
	// Risk analysis activities
	temporalWorker.RegisterActivity(activities.FetchUnanalyzedMessages)
	temporalWorker.RegisterActivity(activities.AnalyzeBatch)
	// Assistant activities
	temporalWorker.RegisterActivity(activities.AdmitAssistantThreads)
	temporalWorker.RegisterActivity(activities.ProcessAssistantThread)
	temporalWorker.RegisterActivity(activities.ExpireAssistantThreadRuntime)
	temporalWorker.RegisterActivity(activities.ReapStuckAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.ReapInactiveAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.SignalAssistantCoordinator)
	temporalWorker.RegisterActivity(activities.SignalAssistantThread)
	// WorkOS sync activities
	temporalWorker.RegisterActivity(activities.ProcessWorkOSOrganizationEvents)

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
	// Risk analysis workflow
	temporalWorker.RegisterWorkflow(DrainRiskAnalysisWorkflow)
	temporalWorker.RegisterWorkflow(AssistantCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(AssistantThreadWorkflow)
	temporalWorker.RegisterWorkflow(AssistantReaperWorkflow)
	temporalWorker.RegisterWorkflow(AssistantRuntimeJanitorWorkflow)
	// WorkOS sync workflows
	temporalWorker.RegisterWorkflow(ProcessWorkOSOrganizationEventsWorkflow)
	temporalWorker.RegisterWorkflow(ProcessWorkOSOrganizationEventsWorkflowDebounced)

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

	if err := AddAssistantReaperSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add assistant reaper schedule", attr.SlogError(err))
	}

	if err := AddAssistantRuntimeJanitorSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add assistant runtime janitor schedule", attr.SlogError(err))
	}

	return temporalWorker
}

package background

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/interceptors"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
)

type WorkerOptions struct {
	DB                   *pgxpool.Pool
	EncryptionClient     *encryption.Client
	FeatureProvider      feature.Provider
	AssetStorage         assets.BlobStore
	SlackClient          *slack_client.SlackClient
	ChatClient           *chat.ChatClient
	OpenRouterChatClient *openrouter.ChatClient
	OpenRouter           openrouter.Provisioner
	K8sClient            *k8s.KubernetesClients
	ExpectedTargetCNAME  string
	BillingTracker       billing.Tracker
	BillingRepository    billing.Repository
	RedisClient          *redis.Client
	PosthogClient        *posthog.Posthog
	FunctionsDeployer    functions.Deployer
	FunctionsVersion     functions.RunnerVersion
	RagService           *rag.ToolsetVectorStore
	AgentsService        *agents.Service
	MCPRegistryClient    *externalmcp.RegistryClient
}

func ForDeploymentProcessing(
	db *pgxpool.Pool,
	f feature.Provider,
	assetStorage assets.BlobStore,
	enc *encryption.Client,
	deployer functions.Deployer,
	mcpRegistryClient *externalmcp.RegistryClient,
) *WorkerOptions {
	return &WorkerOptions{
		DB:                   db,
		EncryptionClient:     enc,
		FeatureProvider:      f,
		AssetStorage:         assetStorage,
		FunctionsDeployer:    deployer,
		FunctionsVersion:     "local", // Test deployers don't use baked versions
		MCPRegistryClient:    mcpRegistryClient,
		SlackClient:          nil,
		ChatClient:           nil,
		OpenRouterChatClient: nil,
		OpenRouter:           nil,
		K8sClient:            nil,
		ExpectedTargetCNAME:  "",
		BillingTracker:       nil,
		BillingRepository:    nil,
		RagService:           nil,
		RedisClient:          nil,
		PosthogClient:        nil,
		AgentsService:        nil,
	}
}

func NewTemporalWorker(
	client client.Client,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	options ...*WorkerOptions,
) worker.Worker {
	opts := &WorkerOptions{
		DB:                   nil,
		EncryptionClient:     nil,
		FeatureProvider:      nil,
		AssetStorage:         nil,
		SlackClient:          nil,
		ChatClient:           nil,
		OpenRouterChatClient: nil,
		OpenRouter:           nil,
		K8sClient:            nil,
		ExpectedTargetCNAME:  "",
		BillingTracker:       nil,
		BillingRepository:    nil,
		RedisClient:          nil,
		PosthogClient:        nil,
		FunctionsDeployer:    nil,
		FunctionsVersion:     "",
		RagService:           nil,
		AgentsService:        nil,
		MCPRegistryClient:    nil,
	}

	for _, o := range options {
		opts = &WorkerOptions{
			DB:                   conv.Default(o.DB, opts.DB),
			EncryptionClient:     conv.Default(o.EncryptionClient, opts.EncryptionClient),
			FeatureProvider:      conv.Default(o.FeatureProvider, opts.FeatureProvider),
			AssetStorage:         conv.Default(o.AssetStorage, opts.AssetStorage),
			SlackClient:          conv.Default(o.SlackClient, opts.SlackClient),
			ChatClient:           conv.Default(o.ChatClient, opts.ChatClient),
			OpenRouter:           conv.Default(o.OpenRouter, opts.OpenRouter),
			OpenRouterChatClient: conv.Default(o.OpenRouterChatClient, opts.OpenRouterChatClient),
			K8sClient:            conv.Default(o.K8sClient, opts.K8sClient),
			ExpectedTargetCNAME:  conv.Default(o.ExpectedTargetCNAME, opts.ExpectedTargetCNAME),
			BillingTracker:       conv.Default(o.BillingTracker, opts.BillingTracker),
			BillingRepository:    conv.Default(o.BillingRepository, opts.BillingRepository),
			RedisClient:          conv.Default(o.RedisClient, opts.RedisClient),
			PosthogClient:        conv.Default(o.PosthogClient, opts.PosthogClient),
			FunctionsDeployer:    conv.Default(o.FunctionsDeployer, opts.FunctionsDeployer),
			FunctionsVersion:     conv.Default(o.FunctionsVersion, opts.FunctionsVersion),
			RagService:           conv.Default(o.RagService, opts.RagService),
			AgentsService:        conv.Default(o.AgentsService, opts.AgentsService),
			MCPRegistryClient:    conv.Default(o.MCPRegistryClient, opts.MCPRegistryClient),
		}
	}

	temporalWorker := worker.New(client, string(TaskQueueMain), worker.Options{
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
		opts.DB,
		opts.EncryptionClient,
		opts.FeatureProvider,
		opts.AssetStorage,
		opts.SlackClient,
		opts.ChatClient,
		opts.OpenRouter,
		opts.K8sClient,
		opts.ExpectedTargetCNAME,
		opts.BillingTracker,
		opts.BillingRepository,
		opts.PosthogClient,
		opts.FunctionsDeployer,
		opts.FunctionsVersion,
		opts.RagService,
		opts.AgentsService,
		opts.MCPRegistryClient,
		client,
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
	// Agent runner related activities
	temporalWorker.RegisterActivity(activities.PreprocessAgentsInput)
	temporalWorker.RegisterActivity(activities.ExecuteToolCall)
	temporalWorker.RegisterActivity(activities.ExecuteModelCall)
	temporalWorker.RegisterActivity(activities.LoadAgentTools)
	temporalWorker.RegisterActivity(activities.RecordAgentExecution)
	temporalWorker.RegisterActivity(activities.ListActiveCustomDomains)
	temporalWorker.RegisterActivity(activities.EnsureCustomDomainIngress)

	temporalWorker.RegisterWorkflow(ProcessDeploymentWorkflow)
	temporalWorker.RegisterWorkflow(FunctionsReaperWorkflow)
	temporalWorker.RegisterWorkflow(SlackEventWorkflow)
	temporalWorker.RegisterWorkflow(OpenrouterKeyRefreshWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainRegistrationWorkflow)
	temporalWorker.RegisterWorkflow(CollectPlatformUsageMetricsWorkflow)
	temporalWorker.RegisterWorkflow(RefreshBillingUsageWorkflow)
	temporalWorker.RegisterWorkflow(IndexToolsetWorkflow)
	temporalWorker.RegisterWorkflow(FallbackModelUsageTrackingWorkflow)
	// Agent runner related workflows
	temporalWorker.RegisterWorkflow(AgentsResponseWorkflow)
	temporalWorker.RegisterWorkflow(SubAgentWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainReconcileWorkflow)

	if err := AddPlatformUsageMetricsSchedule(context.Background(), client); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add platform usage metrics schedule", attr.SlogError(err))
		}
	}

	if err := AddRefreshBillingUsageSchedule(context.Background(), client); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add refresh billing usage schedule", attr.SlogError(err))
		}
	}

	if err := AddCustomDomainReconcileSchedule(context.Background(), client); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add custom domain reconcile schedule", attr.SlogError(err))
		}
	}

	return temporalWorker
}

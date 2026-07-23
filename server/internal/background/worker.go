package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	svix "github.com/svix/svix-webhooks/go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
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
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type WorkerOptions struct {
	GuardianPolicy                 *guardian.Policy
	DB                             *pgxpool.Pool
	EncryptionClient               *encryption.Client
	FeatureProvider                feature.Provider
	AssetStorage                   assets.BlobStore
	SlackClient                    *slack_client.SlackClient
	ChatMessageWriter              *chat.ChatMessageWriter
	ChatClient                     *chat.Client
	OpenRouter                     openrouter.Provisioner
	K8sClient                      *k8s.KubernetesClients
	DefaultCustomDomainProvisioner k8s.ProvisionerKind
	ExpectedTargetCNAME            string
	BillingTracker                 billing.Tracker
	BillingRepository              billing.Repository
	RedisClient                    *redis.Client
	CacheAdapter                   cache.Cache
	EmailService                   *email.Service
	PosthogClient                  *posthog.Posthog
	FunctionsDeployer              functions.Deployer
	FunctionsVersion               functions.RunnerVersion
	RagService                     *rag.ToolsetVectorStore
	MCPRegistryClient              *externalmcp.RegistryClient
	TelemetryLogger                *telemetry.Logger
	ClickhouseConn                 clickhouse.Conn
	TelemetryRepo                  *telemetryrepo.Queries
	TriggersApp                    *bgtriggers.App
	AssistantsCore                 *assistants.ServiceCore
	TemporalEnv                    *tenv.Environment
	PIIScanner                     risk_analysis.PIIScanner
	PIScanner                      *promptinjection.Scanner
	CustomRuleScanner              *customruleanalyzer.Scanner
	BuiltinPresets                 *presetlib.Library
	ShadowMCPClient                *shadowmcp.Client
	AuditLogger                    *audit.Logger
	WorkOSClient                   activities.WorkOSClient
	SvixClient                     *svix.Svix
	ProductFeatures                *productfeatures.Client
	PluginPublisher                *plugins.Service
	Publishers                     *Publishers
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
		DB:                             db,
		GuardianPolicy:                 guardianPolicy,
		EncryptionClient:               enc,
		FeatureProvider:                f,
		AssetStorage:                   assetStorage,
		FunctionsDeployer:              deployer,
		FunctionsVersion:               "local", // Test deployers don't use baked versions
		MCPRegistryClient:              mcpRegistryClient,
		AuditLogger:                    auditLogger,
		SlackClient:                    nil,
		ChatMessageWriter:              nil,
		ChatClient:                     nil,
		OpenRouter:                     nil,
		K8sClient:                      nil,
		DefaultCustomDomainProvisioner: k8s.ProvisionerKindIngress,
		ExpectedTargetCNAME:            "",
		BillingTracker:                 nil,
		BillingRepository:              nil,
		RagService:                     nil,
		RedisClient:                    nil,
		PosthogClient:                  nil,
		TelemetryLogger:                nil,
		TelemetryRepo:                  nil,
		TriggersApp:                    nil,
		CacheAdapter:                   nil,
		EmailService:                   nil,
		AssistantsCore:                 nil,
		TemporalEnv:                    nil,
		PIIScanner:                     nil,
		PIScanner:                      nil,
		CustomRuleScanner:              nil,
		BuiltinPresets:                 nil,
		ShadowMCPClient:                nil,
		WorkOSClient:                   workos.NewStubClient(),
		SvixClient:                     nil,
		ProductFeatures:                nil,
		ClickhouseConn:                 nil,
		PluginPublisher:                nil,
		Publishers: &Publishers{
			PresidioAnalysis:        gcp.NewNoopPublisher[*riskv1.PresidioAnalysis](),
			GitleaksAnalysis:        gcp.NewNoopPublisher[*riskv1.GitleaksAnalysis](),
			PromptInjectionAnalysis: gcp.NewNoopPublisher[*riskv1.PromptInjectionAnalysis](),
			PromptPolicyAnalysis:    gcp.NewNoopPublisher[*riskv1.PromptPolicyAnalysis](),
			CustomRulesAnalysis:     gcp.NewNoopPublisher[*riskv1.CustomRulesAnalysis](),
			TelemetryLogs:           gcp.NewNoopPublisher[*telemetryv1.LogRecord](),
		},
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
		GuardianPolicy:                 nil,
		DB:                             nil,
		EncryptionClient:               nil,
		FeatureProvider:                nil,
		AssetStorage:                   nil,
		SlackClient:                    nil,
		ChatMessageWriter:              nil,
		ChatClient:                     nil,
		OpenRouter:                     nil,
		K8sClient:                      nil,
		DefaultCustomDomainProvisioner: k8s.ProvisionerKindIngress,
		ExpectedTargetCNAME:            "",
		BillingTracker:                 nil,
		BillingRepository:              nil,
		RedisClient:                    nil,
		PosthogClient:                  nil,
		FunctionsDeployer:              nil,
		FunctionsVersion:               "",
		RagService:                     nil,
		MCPRegistryClient:              nil,
		TelemetryLogger:                nil,
		TelemetryRepo:                  nil,
		TriggersApp:                    nil,
		CacheAdapter:                   nil,
		EmailService:                   nil,
		AssistantsCore:                 nil,
		TemporalEnv:                    env,
		PIIScanner:                     nil,
		PIScanner:                      nil,
		CustomRuleScanner:              nil,
		BuiltinPresets:                 nil,
		ShadowMCPClient:                nil,
		AuditLogger:                    nil,
		WorkOSClient:                   workos.NewStubClient(),
		SvixClient:                     nil,
		ProductFeatures:                nil,
		ClickhouseConn:                 nil,
		PluginPublisher:                nil,
		Publishers:                     nil,
	}

	for _, o := range options {
		opts = &WorkerOptions{
			GuardianPolicy:                 conv.Default(o.GuardianPolicy, opts.GuardianPolicy),
			DB:                             conv.Default(o.DB, opts.DB),
			EncryptionClient:               conv.Default(o.EncryptionClient, opts.EncryptionClient),
			FeatureProvider:                conv.Default(o.FeatureProvider, opts.FeatureProvider),
			AssetStorage:                   conv.Default(o.AssetStorage, opts.AssetStorage),
			SlackClient:                    conv.Default(o.SlackClient, opts.SlackClient),
			ChatMessageWriter:              conv.Default(o.ChatMessageWriter, opts.ChatMessageWriter),
			OpenRouter:                     conv.Default(o.OpenRouter, opts.OpenRouter),
			ChatClient:                     conv.Default(o.ChatClient, opts.ChatClient),
			K8sClient:                      conv.Default(o.K8sClient, opts.K8sClient),
			DefaultCustomDomainProvisioner: conv.Default(o.DefaultCustomDomainProvisioner, opts.DefaultCustomDomainProvisioner),
			ExpectedTargetCNAME:            conv.Default(o.ExpectedTargetCNAME, opts.ExpectedTargetCNAME),
			BillingTracker:                 conv.Default(o.BillingTracker, opts.BillingTracker),
			BillingRepository:              conv.Default(o.BillingRepository, opts.BillingRepository),
			RedisClient:                    conv.Default(o.RedisClient, opts.RedisClient),
			PosthogClient:                  conv.Default(o.PosthogClient, opts.PosthogClient),
			FunctionsDeployer:              conv.Default(o.FunctionsDeployer, opts.FunctionsDeployer),
			FunctionsVersion:               conv.Default(o.FunctionsVersion, opts.FunctionsVersion),
			RagService:                     conv.Default(o.RagService, opts.RagService),
			MCPRegistryClient:              conv.Default(o.MCPRegistryClient, opts.MCPRegistryClient),
			TelemetryLogger:                conv.Default(o.TelemetryLogger, opts.TelemetryLogger),
			TelemetryRepo:                  conv.Default(o.TelemetryRepo, opts.TelemetryRepo),
			TriggersApp:                    conv.Default(o.TriggersApp, opts.TriggersApp),
			CacheAdapter:                   conv.Default(o.CacheAdapter, opts.CacheAdapter),
			EmailService:                   conv.Default(o.EmailService, opts.EmailService),
			AssistantsCore:                 conv.Default(o.AssistantsCore, opts.AssistantsCore),
			TemporalEnv:                    conv.Default(o.TemporalEnv, opts.TemporalEnv),
			PIIScanner:                     conv.Default(o.PIIScanner, opts.PIIScanner),
			PIScanner:                      conv.Default(o.PIScanner, opts.PIScanner),
			CustomRuleScanner:              conv.Default(o.CustomRuleScanner, opts.CustomRuleScanner),
			BuiltinPresets:                 conv.Default(o.BuiltinPresets, opts.BuiltinPresets),
			ShadowMCPClient:                conv.Default(o.ShadowMCPClient, opts.ShadowMCPClient),
			AuditLogger:                    conv.Default(o.AuditLogger, opts.AuditLogger),
			WorkOSClient:                   conv.Default(o.WorkOSClient, opts.WorkOSClient),
			SvixClient:                     conv.Default(o.SvixClient, opts.SvixClient),
			ProductFeatures:                conv.Default(o.ProductFeatures, opts.ProductFeatures),
			ClickhouseConn:                 conv.Default(o.ClickhouseConn, opts.ClickhouseConn),
			PluginPublisher:                conv.Default(o.PluginPublisher, opts.PluginPublisher),
			Publishers:                     conv.Default(o.Publishers, opts.Publishers),
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

	aiUsageWorker := worker.New(env.Client(), AIUsagePollerTaskQueue(env.Queue()), worker.Options{
		Interceptors:                       workerInterceptors,
		MaxConcurrentActivityExecutionSize: perPodAIUsagePollerConcurrency,
	})

	skillEfficacyWorker := worker.New(env.Client(), SkillEfficacyTaskQueue(env.Queue()), worker.Options{
		Interceptors:                       workerInterceptors,
		MaxConcurrentActivityExecutionSize: perPodSkillEfficacyPublishConcurrency,
	})

	// The CEL engine is immutable + thread-safe; build one for this worker's
	// risk activities and pass it down. Construction is deterministic and only
	// fails on a malformed descriptor (a bug caught by tests), so log and carry
	// on rather than failing worker startup.
	celEng, celErr := celenv.New()
	if celErr != nil {
		logger.ErrorContext(context.Background(), "build CEL engine for risk activities", attr.SlogError(celErr))
	}

	judgeRateLimiter := openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(opts.RedisClient))

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
		opts.DefaultCustomDomainProvisioner,
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
		opts.ClickhouseConn,
		opts.TelemetryRepo,
		opts.TriggersApp,
		opts.CacheAdapter,
		opts.EmailService,
		opts.AssistantsCore,
		opts.PIIScanner,
		opts.PIScanner,
		opts.CustomRuleScanner,
		opts.ShadowMCPClient,
		opts.AuditLogger,
		opts.WorkOSClient,
		opts.SvixClient,
		opts.ProductFeatures,
		opts.PluginPublisher,
		opts.ChatMessageWriter,
		opts.Publishers,
		celEng,
		judgeRateLimiter,
		opts.BuiltinPresets,
	)

	temporalWorker.RegisterActivity(activities.ProcessDeployment)
	temporalWorker.RegisterActivity(activities.TransitionDeployment)
	temporalWorker.RegisterActivity(activities.ProvisionFunctionsAccess)
	temporalWorker.RegisterActivity(activities.DeployFunctionRunners)
	temporalWorker.RegisterActivity(activities.ReapFlyApps)
	temporalWorker.RegisterActivity(activities.RefreshOpenRouterKey)
	temporalWorker.RegisterActivity(activities.VerifyCustomDomain)
	temporalWorker.RegisterActivity(activities.CustomDomainIngress)
	temporalWorker.RegisterActivity(activities.CollectOpenRouterCreditsMetrics)
	temporalWorker.RegisterActivity(activities.FireOpenRouterCreditsMetrics)
	temporalWorker.RegisterActivity(activities.MaybeSendOpenRouterCreditsAlerts)
	temporalWorker.RegisterActivity(activities.CollectPlatformUsageMetrics)
	temporalWorker.RegisterActivity(activities.FirePlatformUsageMetrics)
	temporalWorker.RegisterActivity(activities.GetAIIntegrationsCandidates)
	temporalWorker.RegisterActivity(activities.RefreshBillingUsage)
	temporalWorker.RegisterActivity(activities.SnapshotBillingCycleUsage)
	temporalWorker.RegisterActivity(activities.ForwardTokenUsageToPostHog)
	temporalWorker.RegisterActivity(activities.GetAllOrganizations)
	temporalWorker.RegisterActivity(activities.ValidateDeployment)
	temporalWorker.RegisterActivity(activities.GenerateToolsetEmbeddings)
	temporalWorker.RegisterActivity(activities.GenerateChatTitle)
	temporalWorker.RegisterActivity(activities.CorrelateClaudePrompts)
	temporalWorker.RegisterActivity(activities.PromoteStagedTelemetry)
	temporalWorker.RegisterActivity(activities.ListStagedTelemetryProjects)
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
	temporalWorker.RegisterActivity(activities.MarkMessagesAnalyzed)
	temporalWorker.RegisterActivity(activities.ReconcileExclusion)
	temporalWorker.RegisterActivity(activities.ReconcileSkillObservations)
	temporalWorker.RegisterActivity(activities.SyncSkillSessionVersions)
	temporalWorker.RegisterActivity(activities.ListProjectsWithPendingSkillObservations)
	temporalWorker.RegisterActivity(activities.CleanRiskPolicyResults)
	riskWorker.RegisterActivity(activities.AnalyzeBatch)
	// Assistant activities
	temporalWorker.RegisterActivity(activities.AdmitAssistantThreads)
	temporalWorker.RegisterActivity(activities.ProcessAssistantThread)
	temporalWorker.RegisterActivity(activities.ExpireAssistantThreadRuntime)
	temporalWorker.RegisterActivity(activities.ReapStuckAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.ReapInactiveAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.ReapStoppedAssistantRuntimes)
	temporalWorker.RegisterActivity(activities.RecycleAssistantRuntimeImages)
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
	temporalWorker.RegisterActivity(activities.ListPluginPublishCandidates)
	temporalWorker.RegisterActivity(activities.PublishPluginProject)
	// Skill efficacy activities — the database steps run on the main queue and
	// only the judged publication goes to the dedicated worker.
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.EnqueueSkillEfficacyPage)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.ReserveSkillEfficacyEvaluations)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.LoadReservedSkillEfficacyEvaluations)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.ListSkillEfficacyProjects)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.ResetStaleSkillEfficacyReservations)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.SignalSkillEfficacyCoordinator)
	skillEfficacyWorker.RegisterActivity(activities.skillEfficacyScorer.PublishSkillEfficacyBatch)

	// AI integration usage syncing runs on its own worker and task queue.
	aiUsageWorker.RegisterActivity(activities.PollAIData)
	// Legacy alias for workflow histories started before the
	// PollAIUsage -> PollAIData rename. Remove once drained.
	aiUsageWorker.RegisterActivityWithOptions(activities.PollAIData, activity.RegisterOptions{
		Name: "PollAIUsage",
	})

	temporalWorker.RegisterWorkflow(ProcessDeploymentWorkflow)
	temporalWorker.RegisterWorkflow(FunctionsReaperWorkflow)
	temporalWorker.RegisterWorkflow(OpenrouterKeyRefreshWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainRegistrationWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainDeletionWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainUpdateWorkflow)
	temporalWorker.RegisterWorkflow(CollectOpenRouterCreditsMetricsWorkflow)
	temporalWorker.RegisterWorkflow(CollectPlatformUsageMetricsWorkflow)
	temporalWorker.RegisterWorkflow(AIUsagePollerCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(AIUsagePollerWorkflow)
	temporalWorker.RegisterWorkflow(RefreshBillingUsageWorkflow)
	temporalWorker.RegisterWorkflow(IndexToolsetWorkflow)
	temporalWorker.RegisterWorkflow(GenerateChatTitleWorkflow)
	temporalWorker.RegisterWorkflow(CorrelateClaudePromptsWorkflow)
	temporalWorker.RegisterWorkflow(PromoteStagedTelemetryWorkflow)
	temporalWorker.RegisterWorkflow(StagedTelemetrySweepWorkflow)
	temporalWorker.RegisterWorkflow(AnalyzeChatResolutionsWorkflow)
	temporalWorker.RegisterWorkflow(DelayedChatResolutionAnalysisWorkflow)
	// Trigger workflows
	temporalWorker.RegisterWorkflow(TriggerCronWorkflow)
	temporalWorker.RegisterWorkflow(TriggerDispatchWorkflow)
	temporalWorker.RegisterWorkflow(TriggerWakeWorkflow)
	// Risk analysis coordinator workflow
	temporalWorker.RegisterWorkflow(RiskAnalysisCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(RiskExclusionReconcileWorkflow)
	temporalWorker.RegisterWorkflow(ReconcileSkillObservationsWorkflow)
	temporalWorker.RegisterWorkflow(SkillObservationReconciliationSweepWorkflow)
	temporalWorker.RegisterWorkflow(RiskPolicyCleanupWorkflow)
	temporalWorker.RegisterWorkflow(AssistantCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(AssistantThreadWorkflow)
	temporalWorker.RegisterWorkflow(AssistantReaperWorkflow)
	temporalWorker.RegisterWorkflow(AssistantRuntimeJanitorWorkflow)
	temporalWorker.RegisterWorkflow(AssistantRuntimeImageRecycleWorkflow)
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
	temporalWorker.RegisterWorkflow(PluginGeneratorRolloutWorkflow)
	temporalWorker.RegisterWorkflow(PluginInitialPublishWorkflow)
	// Skill efficacy workflows
	temporalWorker.RegisterWorkflow(SkillEfficacyCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(SkillEfficacySweepWorkflow)

	if err := AddPlatformUsageMetricsSchedule(context.Background(), env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add platform usage metrics schedule", attr.SlogError(err))
		}
	}

	if err := AddOpenRouterCreditsMetricsSchedule(context.Background(), env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add openrouter credits metrics schedule", attr.SlogError(err))
		}
	}

	if err := AddAIUsagePollerCoordinatorSchedule(context.Background(), env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(context.Background(), "failed to add ai integration usage polling schedule", attr.SlogError(err))
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

	// One image recycle sweep per deployed runtime image: a new worker build
	// carries a new image ref, so kicking on startup is the deploy signal.
	// Best-effort — a failed kick just leaves runtimes to the lazy
	// per-admission recycle.
	if opts.AssistantsCore != nil {
		if imageRef := opts.AssistantsCore.RuntimeImageRef(); imageRef != "" {
			if err := KickAssistantRuntimeImageRecycle(context.Background(), env, imageRef); err != nil {
				logger.ErrorContext(context.Background(), "failed to kick assistant runtime image recycle", attr.SlogError(err))
			}
		}
	}

	if err := AddOutboxGCSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add outbox gc schedule", attr.SlogError(err))
	}

	if err := AddStagedTelemetrySweepSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add staged telemetry sweep schedule", attr.SlogError(err))
	}

	if err := AddSkillObservationReconciliationSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add skill observation reconciliation schedule", attr.SlogError(err))
	}

	if err := AddSkillEfficacySweepSchedule(context.Background(), env); err != nil {
		logger.ErrorContext(context.Background(), "failed to add skill efficacy sweep schedule", attr.SlogError(err))
	}

	if opts.PluginPublisher != nil {
		if err := AddPluginGeneratorRolloutSchedule(context.Background(), env); err != nil {
			logger.ErrorContext(context.Background(), "failed to add plugin generator rollout schedule", attr.SlogError(err))
		}
	}

	return &Workers{main: temporalWorker, riskAnalysis: riskWorker, aiUsage: aiUsageWorker, skillEfficacy: skillEfficacyWorker}
}

// Fleet-wide cap on in-flight AnalyzeBatch per worker pod — the only knob
// in the chain that doesn't multiply with N policies or N batches.
const perPodAnalyzeBatchConcurrency = 20

func RiskAnalysisTaskQueue(mainQueue tenv.TaskQueueName) string {
	return string(mainQueue) + "-risk-analysis"
}

const perPodAIUsagePollerConcurrency = 5

func AIUsagePollerTaskQueue(mainQueue tenv.TaskQueueName) string {
	return string(mainQueue) + "-ai-integration-usage"
}

// Fleet-wide cap on in-flight skill efficacy publications per worker pod. One
// publication judges a whole reserved batch sequentially, so this is the number
// of concurrent judge conversations a pod can hold open.
const perPodSkillEfficacyPublishConcurrency = 5

func SkillEfficacyTaskQueue(mainQueue tenv.TaskQueueName) string {
	return string(mainQueue) + "-skill-efficacy"
}

// Workers bundles the main and dedicated Temporal workers.
type Workers struct {
	main          worker.Worker
	riskAnalysis  worker.Worker
	aiUsage       worker.Worker
	skillEfficacy worker.Worker
}

// Run starts dedicated workers, then blocks running the main worker until
// interruptCh receives.
func (w *Workers) Run(interruptCh <-chan any) error {
	if err := w.riskAnalysis.Start(); err != nil {
		return fmt.Errorf("start risk analysis worker: %w", err)
	}
	defer w.riskAnalysis.Stop()

	if err := w.aiUsage.Start(); err != nil {
		return fmt.Errorf("start ai integration usage worker: %w", err)
	}
	defer w.aiUsage.Stop()

	if err := w.skillEfficacy.Start(); err != nil {
		return fmt.Errorf("start skill efficacy worker: %w", err)
	}
	defer w.skillEfficacy.Stop()

	if err := w.main.Run(interruptCh); err != nil {
		return fmt.Errorf("run main worker: %w", err)
	}
	return nil
}

// Start starts all workers without blocking. Pair with Stop (used by tests).
func (w *Workers) Start() error {
	if err := w.main.Start(); err != nil {
		return fmt.Errorf("start main worker: %w", err)
	}
	if err := w.riskAnalysis.Start(); err != nil {
		w.main.Stop()
		return fmt.Errorf("start risk analysis worker: %w", err)
	}
	if err := w.aiUsage.Start(); err != nil {
		w.riskAnalysis.Stop()
		w.main.Stop()
		return fmt.Errorf("start ai integration usage worker: %w", err)
	}
	if err := w.skillEfficacy.Start(); err != nil {
		w.aiUsage.Stop()
		w.riskAnalysis.Stop()
		w.main.Stop()
		return fmt.Errorf("start skill efficacy worker: %w", err)
	}
	return nil
}

func (w *Workers) Stop() {
	w.skillEfficacy.Stop()
	w.aiUsage.Stop()
	w.riskAnalysis.Stop()
	w.main.Stop()
}

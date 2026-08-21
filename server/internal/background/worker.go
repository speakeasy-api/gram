package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

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
	"go.temporal.io/sdk/workflow"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/infra/pkg/topics"
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
	"github.com/speakeasy-api/gram/server/internal/risk"
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
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
)

type WorkerOptions struct {
	GuardianPolicy      *guardian.Policy
	DB                  *pgxpool.Pool
	EncryptionClient    *encryption.Client
	FeatureProvider     feature.Provider
	AssetStorage        assets.BlobStore
	SlackClient         *slack_client.SlackClient
	ChatMessageWriter   *chat.ChatMessageWriter
	ChatClient          *chat.Client
	OpenRouter          openrouter.Provisioner
	OpenRouterSpend     openrouter.SpendClient
	K8sClient           *k8s.KubernetesClients
	ExpectedTargetCNAME string

	// GitHubEvidenceToken authenticates the recheck sweep's repository
	// lookups; empty falls back to GitHub's small unauthenticated budget.
	GitHubEvidenceToken string
	SiteURL             *url.URL
	BillingTracker      billing.Tracker
	BillingRepository   billing.Repository
	StripeClient        stripeclient.Client
	RedisClient         *redis.Client
	CacheAdapter        cache.Cache
	EmailService        *email.Service
	PosthogClient       *posthog.Posthog
	FunctionsDeployer   functions.Deployer
	FunctionsVersion    functions.RunnerVersion
	RagService          *rag.ToolsetVectorStore
	MCPRegistryClient   *externalmcp.RegistryClient
	TelemetryLogger     *telemetry.Logger
	ClickhouseConn      clickhouse.Conn
	TelemetryRepo       *telemetryrepo.Queries
	TriggersApp         *bgtriggers.App
	AssistantsCore      *assistants.ServiceCore
	TemporalEnv         *tenv.Environment
	PIIScanner          risk_analysis.PIIScanner
	PIScanner           *promptinjection.Scanner
	CustomRuleScanner   *customruleanalyzer.Scanner
	BuiltinPresets      *presetlib.Library
	ShadowMCPClient     *shadowmcp.Client
	AuditLogger         *audit.Logger
	WorkOSClient        activities.WorkOSClient
	SvixClient          *svix.Svix
	ProductFeatures     *productfeatures.Client
	PluginPublisher     *plugins.Service
	Publishers          *Publishers

	// TrialEmailsService synchronizes trial lifecycle changes with Loops.
	TrialEmailsService *trialemails.Service

	// RiskFingerprinter matches exact-value exclusions against the tenant
	// fingerprints stored on ClickHouse findings during the retroactive
	// exclusion reconcile. Zero value = pepper keyring not configured; that
	// reconcile phase degrades with a loud log.
	RiskFingerprinter risk.Fingerprinter

	// DisableRiskRetroReconcile is the kill switch for propagating
	// retroactive exclusion changes into ClickHouse: the reconcile activity
	// gets no ClickHouse repo and degrades to its Postgres phases.
	DisableRiskRetroReconcile bool
}

// defaultFingerprinter merges WorkerOptions fingerprinters: the override wins
// when it carries any keys (Fingerprinter holds a map, so conv.Default's
// comparable constraint cannot apply).
func defaultFingerprinter(override, base risk.Fingerprinter) risk.Fingerprinter {
	if len(override.Versions()) > 0 {
		return override
	}
	return base
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
		ChatMessageWriter:   nil,
		ChatClient:          nil,
		OpenRouter:          nil,
		OpenRouterSpend:     nil,
		K8sClient:           nil,
		ExpectedTargetCNAME: "",
		GitHubEvidenceToken: "",
		SiteURL:             nil,
		BillingTracker:      nil,
		BillingRepository:   nil,
		StripeClient:        nil,
		RagService:          nil,
		RedisClient:         nil,
		PosthogClient:       nil,
		TelemetryLogger:     nil,
		TelemetryRepo:       nil,
		TriggersApp:         nil,
		CacheAdapter:        nil,
		EmailService:        nil,
		AssistantsCore:      nil,
		TemporalEnv:         nil,
		PIIScanner:          nil,
		PIScanner:           nil,
		CustomRuleScanner:   nil,
		BuiltinPresets:      nil,
		ShadowMCPClient:     nil,
		WorkOSClient:        workos.NewStubClient(),
		SvixClient:          nil,
		ProductFeatures:     nil,
		ClickhouseConn:      nil,
		PluginPublisher:     nil,
		Publishers: &Publishers{
			PresidioAnalysis:        gcp.NewNoopPublisher[*riskv1.PresidioAnalysis](),
			GitleaksAnalysis:        gcp.NewNoopPublisher[*riskv1.GitleaksAnalysis](),
			PromptInjectionAnalysis: gcp.NewNoopPublisher[*riskv1.PromptInjectionAnalysis](),
			PromptPolicyAnalysis:    gcp.NewNoopPublisher[*riskv1.PromptPolicyAnalysis](),
			CustomRulesAnalysis:     gcp.NewNoopPublisher[*riskv1.CustomRulesAnalysis](),
			RiskFindings:            gcp.NewNoopPublisher[*riskv1.Finding](),
			TelemetryLogs:           gcp.NewNoopPublisher[*telemetryv1.LogRecord](),
			OTELLogs:                gcp.NewNoopPublisher[*otelv1.InboundLogRecord](),
			OTELSpans:               gcp.NewNoopPublisher[*otelv1.InboundSpan](),
			Outbox:                  topics.NewNoopPublisher(),
		},
		TrialEmailsService:        nil,
		RiskFingerprinter:         risk.Fingerprinter{},
		DisableRiskRetroReconcile: false,
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
		GuardianPolicy:            nil,
		DB:                        nil,
		EncryptionClient:          nil,
		FeatureProvider:           nil,
		AssetStorage:              nil,
		SlackClient:               nil,
		ChatMessageWriter:         nil,
		ChatClient:                nil,
		OpenRouter:                nil,
		OpenRouterSpend:           nil,
		K8sClient:                 nil,
		ExpectedTargetCNAME:       "",
		GitHubEvidenceToken:       "",
		SiteURL:                   nil,
		BillingTracker:            nil,
		BillingRepository:         nil,
		StripeClient:              nil,
		RedisClient:               nil,
		PosthogClient:             nil,
		FunctionsDeployer:         nil,
		FunctionsVersion:          "",
		RagService:                nil,
		MCPRegistryClient:         nil,
		TelemetryLogger:           nil,
		TelemetryRepo:             nil,
		TriggersApp:               nil,
		CacheAdapter:              nil,
		EmailService:              nil,
		AssistantsCore:            nil,
		TemporalEnv:               env,
		PIIScanner:                nil,
		PIScanner:                 nil,
		CustomRuleScanner:         nil,
		BuiltinPresets:            nil,
		ShadowMCPClient:           nil,
		AuditLogger:               nil,
		WorkOSClient:              workos.NewStubClient(),
		SvixClient:                nil,
		ProductFeatures:           nil,
		ClickhouseConn:            nil,
		PluginPublisher:           nil,
		Publishers:                nil,
		TrialEmailsService:        nil,
		RiskFingerprinter:         risk.Fingerprinter{},
		DisableRiskRetroReconcile: false,
	}

	for _, o := range options {
		opts = &WorkerOptions{
			GuardianPolicy:            conv.Default(o.GuardianPolicy, opts.GuardianPolicy),
			DB:                        conv.Default(o.DB, opts.DB),
			EncryptionClient:          conv.Default(o.EncryptionClient, opts.EncryptionClient),
			FeatureProvider:           conv.Default(o.FeatureProvider, opts.FeatureProvider),
			AssetStorage:              conv.Default(o.AssetStorage, opts.AssetStorage),
			SlackClient:               conv.Default(o.SlackClient, opts.SlackClient),
			ChatMessageWriter:         conv.Default(o.ChatMessageWriter, opts.ChatMessageWriter),
			OpenRouter:                conv.Default(o.OpenRouter, opts.OpenRouter),
			OpenRouterSpend:           conv.Default(o.OpenRouterSpend, opts.OpenRouterSpend),
			ChatClient:                conv.Default(o.ChatClient, opts.ChatClient),
			K8sClient:                 conv.Default(o.K8sClient, opts.K8sClient),
			ExpectedTargetCNAME:       conv.Default(o.ExpectedTargetCNAME, opts.ExpectedTargetCNAME),
			GitHubEvidenceToken:       conv.Default(o.GitHubEvidenceToken, opts.GitHubEvidenceToken),
			SiteURL:                   conv.Default(o.SiteURL, opts.SiteURL),
			BillingTracker:            conv.Default(o.BillingTracker, opts.BillingTracker),
			BillingRepository:         conv.Default(o.BillingRepository, opts.BillingRepository),
			StripeClient:              conv.Default(o.StripeClient, opts.StripeClient),
			RedisClient:               conv.Default(o.RedisClient, opts.RedisClient),
			PosthogClient:             conv.Default(o.PosthogClient, opts.PosthogClient),
			FunctionsDeployer:         conv.Default(o.FunctionsDeployer, opts.FunctionsDeployer),
			FunctionsVersion:          conv.Default(o.FunctionsVersion, opts.FunctionsVersion),
			RagService:                conv.Default(o.RagService, opts.RagService),
			MCPRegistryClient:         conv.Default(o.MCPRegistryClient, opts.MCPRegistryClient),
			TelemetryLogger:           conv.Default(o.TelemetryLogger, opts.TelemetryLogger),
			TelemetryRepo:             conv.Default(o.TelemetryRepo, opts.TelemetryRepo),
			TriggersApp:               conv.Default(o.TriggersApp, opts.TriggersApp),
			CacheAdapter:              conv.Default(o.CacheAdapter, opts.CacheAdapter),
			EmailService:              conv.Default(o.EmailService, opts.EmailService),
			AssistantsCore:            conv.Default(o.AssistantsCore, opts.AssistantsCore),
			TemporalEnv:               conv.Default(o.TemporalEnv, opts.TemporalEnv),
			PIIScanner:                conv.Default(o.PIIScanner, opts.PIIScanner),
			PIScanner:                 conv.Default(o.PIScanner, opts.PIScanner),
			CustomRuleScanner:         conv.Default(o.CustomRuleScanner, opts.CustomRuleScanner),
			BuiltinPresets:            conv.Default(o.BuiltinPresets, opts.BuiltinPresets),
			ShadowMCPClient:           conv.Default(o.ShadowMCPClient, opts.ShadowMCPClient),
			AuditLogger:               conv.Default(o.AuditLogger, opts.AuditLogger),
			WorkOSClient:              conv.Default(o.WorkOSClient, opts.WorkOSClient),
			SvixClient:                conv.Default(o.SvixClient, opts.SvixClient),
			ProductFeatures:           conv.Default(o.ProductFeatures, opts.ProductFeatures),
			ClickhouseConn:            conv.Default(o.ClickhouseConn, opts.ClickhouseConn),
			PluginPublisher:           conv.Default(o.PluginPublisher, opts.PluginPublisher),
			Publishers:                conv.Default(o.Publishers, opts.Publishers),
			TrialEmailsService:        conv.Default(o.TrialEmailsService, opts.TrialEmailsService),
			RiskFingerprinter:         defaultFingerprinter(o.RiskFingerprinter, opts.RiskFingerprinter),
			DisableRiskRetroReconcile: conv.Default(o.DisableRiskRetroReconcile, opts.DisableRiskRetroReconcile),
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
		opts.OpenRouterSpend,
		opts.ChatClient,
		opts.K8sClient,
		opts.ExpectedTargetCNAME,
		opts.SiteURL,
		opts.BillingTracker,
		opts.BillingRepository,
		opts.StripeClient,
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
		opts.TrialEmailsService,
		opts.GitHubEvidenceToken,
		opts.RiskFingerprinter,
		opts.DisableRiskRetroReconcile,
	)

	temporalWorker.RegisterActivity(activities.ProcessDeployment)
	temporalWorker.RegisterActivity(activities.TransitionDeployment)
	temporalWorker.RegisterActivity(activities.ProvisionFunctionsAccess)
	temporalWorker.RegisterActivity(activities.DeployFunctionRunners)
	temporalWorker.RegisterActivity(activities.ReapFlyApps)
	temporalWorker.RegisterActivity(activities.RefreshOpenRouterKey)
	temporalWorker.RegisterActivity(activities.SetOpenRouterSpendCap)
	temporalWorker.RegisterActivity(activities.ReconcilePaygOpenRouterChatKey)
	temporalWorker.RegisterActivity(activities.VerifyCustomDomain)
	temporalWorker.RegisterActivity(activities.CustomDomainIngress)
	temporalWorker.RegisterActivity(activities.ReconcileCustomDomain)
	temporalWorker.RegisterActivity(activities.SignalCustomDomainReconcile)
	temporalWorker.RegisterActivity(activities.ListCustomDomainsForHealthCheck)
	temporalWorker.RegisterActivity(activities.CheckCustomDomainHealth)
	temporalWorker.RegisterActivity(activities.NotifyCustomDomainUnhealthy)
	temporalWorker.RegisterActivity(activities.FindOrphanCustomDomainResources)
	temporalWorker.RegisterActivity(activities.CollectOpenRouterCreditsMetrics)
	temporalWorker.RegisterActivity(activities.CollectOpenRouterDailySpend)
	temporalWorker.RegisterActivity(activities.SettleStripeInvoiceAllocations)
	temporalWorker.RegisterActivity(activities.FireOpenRouterCreditsMetrics)
	temporalWorker.RegisterActivity(activities.MaybeSendOpenRouterCreditsAlerts)
	temporalWorker.RegisterActivity(activities.CollectPlatformUsageMetrics)
	temporalWorker.RegisterActivity(activities.FirePlatformUsageMetrics)
	temporalWorker.RegisterActivity(activities.GetAIIntegrationsCandidates)
	temporalWorker.RegisterActivity(activities.GetDeviceIntegrationSyncCandidates)
	temporalWorker.RegisterActivity(activities.RunDeviceIntegrationSync)
	temporalWorker.RegisterActivity(activities.RefreshBillingUsage)
	temporalWorker.RegisterActivity(activities.SnapshotBillingCycleUsage)
	temporalWorker.RegisterActivity(activities.ReportTUMUsageToStripe)
	temporalWorker.RegisterActivity(activities.ListWeeklyUsageSummaryTargets)
	temporalWorker.RegisterActivity(activities.SendWeeklyUsageSummary)
	temporalWorker.RegisterActivity(activities.ForwardTokenUsageToPostHog)
	temporalWorker.RegisterActivity(activities.GetAllOrganizations)
	temporalWorker.RegisterActivity(activities.ValidateDeployment)
	temporalWorker.RegisterActivity(activities.GenerateToolsetEmbeddings)
	temporalWorker.RegisterActivity(activities.GenerateChatTitle)
	temporalWorker.RegisterActivity(activities.SyncIdentityMap)
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
	temporalWorker.RegisterActivity(activities.ProcessWorkOSOrganizationEvents)
	temporalWorker.RegisterActivity(activities.ProcessWorkOSGlobalRoleEvents)
	temporalWorker.RegisterActivity(activities.ProcessWorkOSUserEvents)
	// Outbox relay activities
	temporalWorker.RegisterActivity(activities.FetchPendingOutboxEvents)
	temporalWorker.RegisterActivity(activities.FilterNoopOutboxEvents)
	temporalWorker.RegisterActivity(activities.RelayOutboxEvents)
	temporalWorker.RegisterActivity(activities.GCOutboxProcessedRows)
	// Publish outbox relay activities
	temporalWorker.RegisterActivity(activities.DrainPublishOutbox)
	temporalWorker.RegisterActivity(activities.GCPublishOutboxDeadLetters)
	// Plugin publishing activities
	temporalWorker.RegisterActivity(activities.ListPluginPublishCandidates)
	temporalWorker.RegisterActivity(activities.PublishPluginProject)
	// Spend rule evaluation activities
	temporalWorker.RegisterActivity(activities.ListSpendRuleOrgs)
	temporalWorker.RegisterActivity(activities.EvaluateOrgSpendRules)
	temporalWorker.RegisterActivity(activities.RefreshSpendRuleActor)
	// Pre-emptive remote session refresh activities
	temporalWorker.RegisterActivity(activities.ClaimDueRemoteSessionRefreshCandidates)
	temporalWorker.RegisterActivity(activities.RefreshRemoteSession)
	// Trial expiry activities
	temporalWorker.RegisterActivity(activities.ListExpiredTrials)
	temporalWorker.RegisterActivity(activities.DemoteExpiredTrial)
	temporalWorker.RegisterActivity(activities.RunMcpResearch)
	temporalWorker.RegisterActivity(activities.MarkMcpResearchInterrupted)
	temporalWorker.RegisterActivity(activities.SendTrialLifecycleEmail)
	temporalWorker.RegisterActivity(activities.ResolveTrialEndingReminder)
	temporalWorker.RegisterActivity(activities.SendTrialEndingSoonEmail)
	temporalWorker.RegisterActivity(activities.SendAccessPausedEmail)
	temporalWorker.RegisterActivity(activities.SendPaygActivatedEmail)
	// Skill efficacy activities — the database steps run on the main queue and
	// only the judged publication goes to the dedicated worker.
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.EnqueueSkillEfficacyPage)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.ReserveSkillEfficacyEvaluations)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.LoadReservedSkillEfficacyEvaluations)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.ListSkillEfficacyProjects)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.ResetStaleSkillEfficacyReservations)
	temporalWorker.RegisterActivity(activities.skillEfficacyScorer.SignalSkillEfficacyCoordinator)
	skillEfficacyWorker.RegisterActivity(activities.skillEfficacyScorer.PublishSkillEfficacyBatch)
	// Chat analysis activities — same split as skill efficacy: database steps on
	// the main queue, the judged publication on the shared judged-publication
	// worker so one per-pod cap bounds both pipelines' model conversations.
	temporalWorker.RegisterActivity(activities.chatAnalysisScorer.EnqueueChatAnalysisPage)
	temporalWorker.RegisterActivity(activities.chatAnalysisScorer.ReserveChatAnalysisEvaluations)
	temporalWorker.RegisterActivity(activities.chatAnalysisScorer.LoadReservedChatAnalysisEvaluations)
	temporalWorker.RegisterActivity(activities.chatAnalysisScorer.ListChatAnalysisProjects)
	temporalWorker.RegisterActivity(activities.chatAnalysisScorer.ResetStaleChatAnalysisReservations)
	temporalWorker.RegisterActivity(activities.chatAnalysisScorer.SignalChatAnalysisCoordinator)
	skillEfficacyWorker.RegisterActivity(activities.chatAnalysisScorer.PublishChatAnalysisBatch)
	if activities.skillSuggestionAnalyzer != nil {
		temporalWorker.RegisterActivity(activities.skillSuggestionAnalyzer.ListSkillSuggestionProjects)
		temporalWorker.RegisterActivity(activities.skillSuggestionAnalyzer.ListRecentlyActiveSuggestionSkills)
		temporalWorker.RegisterActivity(activities.skillSuggestionAnalyzer.SignalSkillSuggestions)
		skillEfficacyWorker.RegisterActivity(activities.skillSuggestionAnalyzer.AnalyzeSkillSuggestion)
		temporalWorker.RegisterWorkflow(SkillSuggestionWorkflow)
		temporalWorker.RegisterWorkflow(SkillSuggestionAnalysisWorkflow)
		temporalWorker.RegisterWorkflow(SkillSuggestionSweepWorkflow)
	}

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
	temporalWorker.RegisterWorkflow(OpenRouterSpendCapWorkflow)
	temporalWorker.RegisterWorkflow(PaygOpenRouterChatKeyReconcileWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainRegistrationWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainDeletionWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainUpdateWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainReconcileWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainHealthCheckWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainUnhealthyNotifyWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainHealthSweepWorkflow)
	temporalWorker.RegisterWorkflow(CollectOpenRouterCreditsMetricsWorkflow)
	temporalWorker.RegisterWorkflow(CollectOpenRouterDailySpendWorkflow)
	temporalWorker.RegisterWorkflow(CollectPlatformUsageMetricsWorkflow)
	temporalWorker.RegisterWorkflow(AIUsagePollerCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(DeviceIntegrationSyncCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(DeviceIntegrationSyncWorkflow)
	temporalWorker.RegisterWorkflow(AIUsagePollerWorkflow)
	temporalWorker.RegisterWorkflow(RefreshBillingUsageWorkflow)
	temporalWorker.RegisterWorkflow(WeeklyUsageSummaryWorkflow)
	temporalWorker.RegisterWorkflow(IndexToolsetWorkflow)
	temporalWorker.RegisterWorkflow(GenerateChatTitleWorkflow)
	temporalWorker.RegisterWorkflow(SyncIdentityMapWorkflow)
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
	// Retire per-policy executions created before the coordinator migration.
	temporalWorker.RegisterWorkflowWithOptions(legacyDrainRiskAnalysisWorkflow, workflow.RegisterOptions{
		Name:                          legacyDrainRiskAnalysisWorkflowName,
		DisableAlreadyRegisteredCheck: false,
	})
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
	// Assistants signup followups
	temporalWorker.RegisterWorkflow(CancelAssistantsSubscriptionWorkflow)
	// Outbox -> Relay workflow and GC
	temporalWorker.RegisterWorkflow(ProcessOutboxWorkflow)
	temporalWorker.RegisterWorkflow(OutboxGCWorkflow)
	// Publish outbox -> Pub/Sub workflow and dead letter GC
	temporalWorker.RegisterWorkflow(PublishOutboxWorkflow)
	temporalWorker.RegisterWorkflow(PublishOutboxGCWorkflow)
	temporalWorker.RegisterWorkflow(PluginGeneratorRolloutWorkflow)
	temporalWorker.RegisterWorkflow(PluginInitialPublishWorkflow)
	// Spend rule evaluation workflows
	temporalWorker.RegisterWorkflow(SpendRuleEvaluationWorkflow)
	temporalWorker.RegisterWorkflow(SpendRuleOrgEvaluationWorkflow)
	temporalWorker.RegisterWorkflow(SpendRuleOrgEvaluationWorkflowDebounced)
	temporalWorker.RegisterWorkflow(SpendRuleActorEvaluationWorkflow)
	temporalWorker.RegisterWorkflow(SpendRuleActorEvaluationWorkflowDebounced)
	// Skill efficacy workflows
	temporalWorker.RegisterWorkflow(SkillEfficacyCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(SkillEfficacySweepWorkflow)
	// Chat analysis workflows
	temporalWorker.RegisterWorkflow(McpResearchWorkflow)
	temporalWorker.RegisterWorkflow(ChatAnalysisCoordinatorWorkflow)
	temporalWorker.RegisterWorkflow(ChatAnalysisSweepWorkflow)
	// Pre-emptive remote session refresh workflows
	temporalWorker.RegisterWorkflow(RemoteSessionRefreshWorkflow)
	// Trial expiry workflows
	temporalWorker.RegisterWorkflow(DemoteExpiredTrialsWorkflow)
	temporalWorker.RegisterWorkflow(TrialLifecycleEmailWorkflow)
	temporalWorker.RegisterWorkflow(AccessPausedEmailWorkflow)
	temporalWorker.RegisterWorkflow(PaygActivatedEmailWorkflow)

	return &Workers{
		main:              temporalWorker,
		riskAnalysis:      riskWorker,
		aiUsage:           aiUsageWorker,
		skillEfficacy:     skillEfficacyWorker,
		env:               env,
		logger:            logger,
		opts:              opts,
		hasSkillSuggester: activities.skillSuggestionAnalyzer != nil,
	}
}

// registerSchedules installs the fleet's recurring Temporal schedules and the
// one-shot startup kicks that belong with them. Every schedule is best-effort:
// a failure is logged and the worker still comes up, because a missing sweep
// degrades a background pipeline rather than the request path.
func (w *Workers) registerSchedules(ctx context.Context) {
	env, logger, opts := w.env, w.logger, w.opts

	if err := AddPlatformUsageMetricsSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add platform usage metrics schedule", attr.SlogError(err))
		}
	}

	if err := AddOpenRouterCreditsMetricsSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add openrouter credits metrics schedule", attr.SlogError(err))
		}
	}

	if err := AddOpenRouterDailySpendSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add openrouter daily spend schedule", attr.SlogError(err))
	}

	if err := AddDeviceIntegrationSyncCoordinatorSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add device integration sync schedule", attr.SlogError(err))
		}
	}

	if err := AddAIUsagePollerCoordinatorSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add ai integration usage polling schedule", attr.SlogError(err))
		}
	}

	if err := AddWeeklyUsageSummarySchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add weekly usage summary schedule", attr.SlogError(err))
		}
	}

	if err := AddRefreshBillingUsageSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add refresh billing usage schedule", attr.SlogError(err))
		}
	}

	if err := AddProcessOutboxSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add relay outbox to svix schedule", attr.SlogError(err))
		}
	}

	if err := AddAssistantReaperSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add assistant reaper schedule", attr.SlogError(err))
	}

	if err := AddAssistantRuntimeJanitorSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add assistant runtime janitor schedule", attr.SlogError(err))
	}

	if err := AddAssistantMemoriesReaperSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add assistant memories reaper schedule", attr.SlogError(err))
	}

	// One image recycle sweep per deployed runtime image: a new worker build
	// carries a new image ref, so kicking on startup is the deploy signal.
	// Best-effort — a failed kick just leaves runtimes to the lazy
	// per-admission recycle.
	if opts.AssistantsCore != nil {
		if imageRef := opts.AssistantsCore.RuntimeImageRef(); imageRef != "" {
			if err := KickAssistantRuntimeImageRecycle(ctx, env, imageRef); err != nil {
				logger.ErrorContext(ctx, "failed to kick assistant runtime image recycle", attr.SlogError(err))
			}
		}
	}

	if err := AddOutboxGCSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add outbox gc schedule", attr.SlogError(err))
	}

	if err := AddPublishOutboxSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add publish outbox schedule", attr.SlogError(err))
	}

	if err := AddPublishOutboxGCSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add publish outbox gc schedule", attr.SlogError(err))
	}

	if err := AddStagedTelemetrySweepSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add staged telemetry sweep schedule", attr.SlogError(err))
	}

	if err := AddIdentityMapSyncSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add identity map sync schedule", attr.SlogError(err))
	}

	if err := AddSpendRuleEvaluationSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add spend rule evaluation schedule", attr.SlogError(err))
		}
	}

	if err := AddSkillObservationReconciliationSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add skill observation reconciliation schedule", attr.SlogError(err))
	}

	if err := AddSkillEfficacySweepSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add skill efficacy sweep schedule", attr.SlogError(err))
	}

	if err := AddChatAnalysisSweepSchedule(ctx, env); err != nil {
		logger.ErrorContext(ctx, "failed to add chat analysis sweep schedule", attr.SlogError(err))
	}

	if err := AddRemoteSessionRefreshSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add remote session refresh schedule", attr.SlogError(err))
		}
	}

	if err := AddTrialDemotionSchedule(ctx, env); err != nil {
		if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
			logger.ErrorContext(ctx, "failed to add trial demotion schedule", attr.SlogError(err))
		}
	}

	if w.hasSkillSuggester {
		if err := AddSkillSuggestionSweepSchedule(ctx, env); err != nil {
			logger.ErrorContext(ctx, "failed to add skill suggestion sweep schedule", attr.SlogError(err))
		}
	}

	if opts.DB != nil && opts.K8sClient != nil && opts.ExpectedTargetCNAME != "" {
		if err := AddCustomDomainHealthSchedule(ctx, env); err != nil {
			logger.ErrorContext(ctx, "failed to add custom domain health schedule", attr.SlogError(err))
		}
	}

	if opts.PluginPublisher != nil {
		if err := AddPluginGeneratorRolloutSchedule(ctx, env); err != nil {
			logger.ErrorContext(ctx, "failed to add plugin generator rollout schedule", attr.SlogError(err))
		}
	}
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

	// Retained so Run can install the recurring schedules; see
	// registerSchedules.
	env               *tenv.Environment
	logger            *slog.Logger
	opts              *WorkerOptions
	hasSkillSuggester bool
}

// Run registers the recurring schedules, starts the dedicated workers, then
// blocks running the main worker until interruptCh receives.
func (w *Workers) Run(interruptCh <-chan any) error {
	w.registerSchedules(context.Background())

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
//
// Unlike Run, this deliberately does not register the recurring schedules. A
// test builds a throwaway namespace per test case and only exercises the
// workflow it started; installing ~20 scheduler workflows in each of those
// namespaces made the shared dev server, not the code under test, the
// bottleneck.
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

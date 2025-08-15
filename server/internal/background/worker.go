package background

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"

	polargo "github.com/polarsource/polar-go"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/background/interceptors"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
)

type WorkerOptions struct {
	DB                  *pgxpool.Pool
	FeatureProvider     feature.Provider
	AssetStorage        assets.BlobStore
	SlackClient         *slack_client.SlackClient
	ChatClient          *chat.ChatClient
	OpenRouter          openrouter.Provisioner
	K8sClient           *k8s.KubernetesClients
	ExpectedTargetCNAME string
	Polar               *polargo.Polar
}

func ForDeploymentProcessing(db *pgxpool.Pool, f feature.Provider, assetStorage assets.BlobStore) *WorkerOptions {
	return &WorkerOptions{
		DB:                  db,
		FeatureProvider:     f,
		AssetStorage:        assetStorage,
		SlackClient:         nil,
		ChatClient:          nil,
		OpenRouter:          nil,
		K8sClient:           nil,
		ExpectedTargetCNAME: "",
		Polar:               nil,
	}
}

func NewTemporalWorker(
	client client.Client,
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	options ...*WorkerOptions,
) worker.Worker {
	opts := &WorkerOptions{
		DB:                  nil,
		FeatureProvider:     nil,
		AssetStorage:        nil,
		SlackClient:         nil,
		ChatClient:          nil,
		OpenRouter:          nil,
		K8sClient:           nil,
		ExpectedTargetCNAME: "",
		Polar:               nil,
	}

	for _, o := range options {
		opts = &WorkerOptions{
			DB:                  conv.Default(o.DB, opts.DB),
			FeatureProvider:     conv.Default(o.FeatureProvider, opts.FeatureProvider),
			AssetStorage:        conv.Default(o.AssetStorage, opts.AssetStorage),
			SlackClient:         conv.Default(o.SlackClient, opts.SlackClient),
			ChatClient:          conv.Default(o.ChatClient, opts.ChatClient),
			OpenRouter:          conv.Default(o.OpenRouter, opts.OpenRouter),
			K8sClient:           conv.Default(o.K8sClient, opts.K8sClient),
			ExpectedTargetCNAME: conv.Default(o.ExpectedTargetCNAME, opts.ExpectedTargetCNAME),
			Polar:               conv.Default(o.Polar, opts.Polar),
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
		meterProvider,
		opts.DB,
		opts.FeatureProvider,
		opts.AssetStorage,
		opts.SlackClient,
		opts.ChatClient,
		opts.OpenRouter,
		opts.K8sClient,
		opts.ExpectedTargetCNAME,
		opts.Polar,
	)

	temporalWorker.RegisterActivity(activities.ProcessDeployment)
	temporalWorker.RegisterActivity(activities.TransitionDeployment)
	temporalWorker.RegisterActivity(activities.GetSlackProjectContext)
	temporalWorker.RegisterActivity(activities.PostSlackMessage)
	temporalWorker.RegisterActivity(activities.SlackChatCompletion)
	temporalWorker.RegisterActivity(activities.RefreshOpenRouterKey)
	temporalWorker.RegisterActivity(activities.VerifyCustomDomain)
	temporalWorker.RegisterActivity(activities.CustomDomainIngress)
	temporalWorker.RegisterActivity(activities.CollectPlatformUsageMetrics)

	temporalWorker.RegisterWorkflow(ProcessDeploymentWorkflow)
	temporalWorker.RegisterWorkflow(SlackEventWorkflow)
	temporalWorker.RegisterWorkflow(OpenrouterKeyRefreshWorkflow)
	temporalWorker.RegisterWorkflow(CustomDomainRegistrationWorkflow)
	temporalWorker.RegisterWorkflow(CollectPlatformUsageMetricsWorkflow)

	return temporalWorker
}

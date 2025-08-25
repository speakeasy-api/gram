package background

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	polargo "github.com/polarsource/polar-go"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
)

type Activities struct {
	processDeployment      *activities.ProcessDeployment
	transitionDeployment   *activities.TransitionDeployment
	getSlackProjectContext *activities.GetSlackProjectContext
	postSlackMessage       *activities.PostSlackMessage
	slackChatCompletion    *activities.SlackChatCompletion
	refreshOpenRouterKey   *activities.RefreshOpenRouterKey
	verifyCustomDomain     *activities.VerifyCustomDomain
	customDomainIngress    *activities.CustomDomainIngress
	collectPlatformUsageMetrics *activities.CollectPlatformUsageMetrics
}

func NewActivities(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	features feature.Provider,
	assetStorage assets.BlobStore,
	slackClient *slack_client.SlackClient,
	chatClient *chat.ChatClient,
	openrouter openrouter.Provisioner,
	k8sClient *k8s.KubernetesClients,
	expectedTargetCNAME string,
	polarClientRaw *polargo.Polar,
	redisClient *redis.Client,
) *Activities {
	polarClient := polar.NewClient(polarClientRaw, logger, redisClient)
	return &Activities{
		processDeployment:      activities.NewProcessDeployment(logger, meterProvider, db, features, assetStorage),
		transitionDeployment:   activities.NewTransitionDeployment(logger, db),
		getSlackProjectContext: activities.NewSlackProjectContextActivity(logger, db, slackClient),
		postSlackMessage:       activities.NewPostSlackMessageActivity(logger, slackClient),
		slackChatCompletion:    activities.NewSlackChatCompletionActivity(logger, slackClient, chatClient),
		refreshOpenRouterKey:   activities.NewRefreshOpenRouterKey(logger, db, openrouter),
		verifyCustomDomain:     activities.NewVerifyCustomDomain(logger, db, expectedTargetCNAME),
		customDomainIngress:    activities.NewCustomDomainIngress(logger, db, k8sClient),
		collectPlatformUsageMetrics: activities.NewCollectPlatformUsageMetrics(logger, db, polarClient),
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

func (a *Activities) CollectPlatformUsageMetrics(ctx context.Context) error {
	return a.collectPlatformUsageMetrics.Do(ctx)
}

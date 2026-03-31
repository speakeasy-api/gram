package chat

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func NewBaseChatClient(
	logger *slog.Logger,
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	openRouter openrouter.Provisioner,
	temporalEnv *temporal.Environment,
	telemSvc *telemetry.Service,
	assetStorage assets.BlobStore,
	tracking billing.Tracker,
	fallbackTracker FallbackModelUsageTracker,
	titleGenerator openrouter.ChatTitleGenerator,
	resolutionAnalyzer openrouter.ChatResolutionAnalyzer,
) *openrouter.ChatClient {

	// Create message capture strategy for chat messages
	messageCaptureStrategy := NewChatMessageCaptureStrategy(
		logger,
		db,
		assetStorage,
	)

	// Create usage tracking strategy with fallback support
	usageTrackingStrategy := NewDefaultUsageTrackingStrategy(
		db,
		logger,
		openRouter,
		tracking,
		fallbackTracker,
	)

	// Create UnifiedClient with strategies (after telemSvc is available)
	return openrouter.NewUnifiedClient(
		logger,
		guardianPolicy,
		openRouter,
		messageCaptureStrategy,
		usageTrackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemSvc,
	)
}

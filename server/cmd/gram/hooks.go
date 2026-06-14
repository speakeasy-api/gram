package gram

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/providers/cursor"
	agenteventruntime "github.com/speakeasy-api/gram/server/internal/agentevents/runtime"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

func newAgentEvents(
	db *pgxpool.Pool,
	telemLogger *telemetry.Logger,
	chatWriter *chat.ChatMessageWriter,
	productFeatures *productfeatures.Client,
	chatTitleGenerator *background.TemporalChatTitleGenerator,
) (*agentevents.Mux, error) {
	config := agenteventruntime.Config{
		DB:                 db,
		TelemetryLogger:    telemLogger,
		ChatWriter:         chatWriter,
		ProductFeatures:    productFeatures,
		ChatTitleGenerator: chatTitleGenerator,
	}
	return agenteventruntime.New(config,
		agenteventruntime.Provider(cursoragent.Spec()),
		//agenteventruntime.Provider(codexagent.Spec()),
	)
}

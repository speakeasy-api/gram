package runtime

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformslack "github.com/speakeasy-api/gram/server/internal/platformtools/slack"
)

type assistantSlackStoreAdapter struct {
	store *authclients.SlackAppCredentialStore
}

func (a assistantSlackStoreAdapter) ResolveForCaller(ctx context.Context, organizationID string, projectID uuid.UUID) (platformslack.AssistantSlackCredentials, error) {
	creds, err := a.store.ResolveForCaller(ctx, organizationID, projectID)
	if err != nil {
		return platformslack.AssistantSlackCredentials{}, fmt.Errorf("resolve assistant Slack credentials: %w", err)
	}
	return platformslack.AssistantSlackCredentials{
		BotToken:  creds.BotToken,
		UserToken: creds.UserToken,
		Token:     creds.Token,
	}, nil
}

// AssistantSlackTools returns the built-in assistant-slack tools backed by
// slack_app typed auth clients resolved per caller via mcp:auth.
func AssistantSlackTools(
	httpClient *guardian.HTTPClient,
	router platformslack.ThreadRouter,
	enc *encryption.Client,
	serverURL *url.URL,
	store *authclients.SlackAppCredentialStore,
) []platformtools.ExternalTool {
	var source platformslack.AssistantSlackCredentialSource
	if store != nil {
		source = assistantSlackStoreAdapter{store: store}
	}
	executors := platformslack.NewAssistantSlackTools(httpClient, router, enc, serverURL, source)
	tools := make([]platformtools.ExternalTool, 0, len(executors))
	for _, executor := range executors {
		tools = append(tools, platformtools.ExternalTool{Executor: executor, RequiredFeature: "", RequiredScope: ""})
	}
	return tools
}

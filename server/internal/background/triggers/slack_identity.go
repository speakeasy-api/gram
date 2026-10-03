package triggers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// slackBotIdentityTTL is how long a resolved bot identity is reused. A bot
// token's identity never changes, so the TTL only bounds how long entries for
// revoked tokens linger.
const slackBotIdentityTTL = 24 * time.Hour

// slackBotIdentity is the cached auth.test result for one bot token, keyed by
// a hash so the token never reaches the cache.
type slackBotIdentity struct {
	TokenHash string `json:"token_hash"`
	UserID    string `json:"user_id"`
	BotID     string `json:"bot_id"`
}

func (i slackBotIdentity) CacheKey() string {
	return slackBotIdentityCacheKey(i.TokenHash)
}

func (i slackBotIdentity) TTL() time.Duration {
	return slackBotIdentityTTL
}

func slackBotIdentityCacheKey(tokenHash string) string {
	return "slack-bot-identity:" + tokenHash
}

// withSlackBotIdentity stamps the app's bot user and bot on a Slack event so
// routing can recognize mentions of the app and the app's own messages.
// Failing to resolve them leaves the event unchanged.
func (a *App) withSlackBotIdentity(ctx context.Context, instance triggerrepo.TriggerInstance, env map[string]string, event any) any {
	evt, ok := event.(slackTriggerEvent)
	if !ok || instance.DefinitionSlug != DefinitionSlugSlack {
		return event
	}
	token := toolconfig.CIEnvFrom(env).Get("SLACK_BOT_TOKEN")
	if token == "" {
		return event
	}

	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])
	identity, err := a.slackBotIdentities.Get(ctx, slackBotIdentityCacheKey(tokenHash))
	if err != nil {
		resolved, err := a.slackClient.BotIdentity(ctx, token)
		if err != nil {
			a.logger.WarnContext(ctx, "resolve slack bot identity", attr.SlogTriggerInstanceID(instance.ID.String()), attr.SlogError(err))
			return event
		}
		identity = slackBotIdentity{TokenHash: tokenHash, UserID: resolved.UserID, BotID: resolved.BotID}
		if err := a.slackBotIdentities.Store(ctx, identity); err != nil {
			a.logger.WarnContext(ctx, "cache slack bot identity", attr.SlogError(err))
		}
	}

	evt.BotUserID = identity.UserID
	evt.SelfBotID = identity.BotID
	return evt
}

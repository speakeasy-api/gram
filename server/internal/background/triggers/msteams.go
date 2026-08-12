package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/cel-go/cel"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// msteamsAppIDEnv holds the bot's Microsoft App ID. It is the JWT audience
// Bot Framework tokens are issued for, so it doubles as the webhook
// authentication anchor for a trigger instance.
const msteamsAppIDEnv = "MSTEAMS_APP_ID"

// Bot Framework connector-to-bot authentication constants. See
// https://learn.microsoft.com/azure/bot-service/rest-api/bot-framework-rest-connector-authentication
// — the metadata URL is documented as static and safe to hardcode; the issuer
// is fixed for protocol v3.1/v3.2.
const (
	botFrameworkIssuer          = "https://api.botframework.com"
	botFrameworkOpenIDConfigURL = "https://login.botframework.com/v1/.well-known/openidconfiguration"
)

// msteamsTriggerConfig is the instance config for the MS Teams trigger,
// exposing the shared webhook filter knobs: a CEL filter expression and an
// activity-type allowlist narrowing the default-deny
// supportedMSTeamsEventTypes set.
type msteamsTriggerConfig struct {
	FilterExpr string   `json:"filter,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`

	// compiledFilter is set during DecodeConfig when FilterExpr is non-empty.
	compiledFilter cel.Program
}

func (c msteamsTriggerConfig) Filter(event any) (bool, error) {
	teamsEvent, ok := event.(msteamsTriggerEvent)
	if !ok {
		return false, fmt.Errorf("expected msteamsTriggerEvent, got %T", event)
	}
	return evalWebhookFilter(c.compiledFilter, c.EventTypes, event, teamsEvent.EventType, supportedMSTeamsEventTypes)
}

// msteamsActivity is the subset of the Bot Framework Activity object posted to
// the bot's webhook endpoint. The `type` field discriminates the activity
// shape (message, messageReaction, conversationUpdate, installationUpdate, …).
// See https://learn.microsoft.com/azure/bot-service/rest-api/bot-framework-rest-connector-api-reference#activity-object
type msteamsActivity struct {
	Type         string                     `json:"type"`
	ID           string                     `json:"id,omitempty"`
	Timestamp    string                     `json:"timestamp,omitempty"`
	ServiceURL   string                     `json:"serviceUrl,omitempty"`
	From         msteamsChannelAccount      `json:"from"`
	Recipient    msteamsChannelAccount      `json:"recipient"`
	Conversation msteamsConversationAccount `json:"conversation"`
	Text         string                     `json:"text,omitempty"`
	ReplyToID    string                     `json:"replyToId,omitempty"`
	// Action is set on installationUpdate activities: "add" or "remove".
	Action           string                  `json:"action,omitempty"`
	ReactionsAdded   []msteamsReaction       `json:"reactionsAdded,omitempty"`
	ReactionsRemoved []msteamsReaction       `json:"reactionsRemoved,omitempty"`
	MembersAdded     []msteamsChannelAccount `json:"membersAdded,omitempty"`
	MembersRemoved   []msteamsChannelAccount `json:"membersRemoved,omitempty"`
	ChannelData      *msteamsChannelData     `json:"channelData,omitempty"`
}

type msteamsChannelAccount struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	AADObjectID string `json:"aadObjectId,omitempty"`
}

type msteamsConversationAccount struct {
	ID               string `json:"id,omitempty"`
	ConversationType string `json:"conversationType,omitempty"`
	TenantID         string `json:"tenantId,omitempty"`
}

type msteamsReaction struct {
	Type string `json:"type,omitempty"`
}

// msteamsChannelData carries the Teams-specific envelope Bot Framework tucks
// under channelData: the human-facing team/channel identity (Activity's own
// channelId is always the literal platform id "msteams") and the tenant.
type msteamsChannelData struct {
	Channel *msteamsChannelDataRef `json:"channel,omitempty"`
	Team    *msteamsChannelDataRef `json:"team,omitempty"`
	Tenant  *msteamsChannelDataRef `json:"tenant,omitempty"`
}

type msteamsChannelDataRef struct {
	ID string `json:"id,omitempty"`
}

// msteamsTriggerEvent is the normalized event surfaced to CEL filters and the
// assistant adapter. EventType is the Bot Framework activity type.
type msteamsTriggerEvent struct {
	EventType        string `json:"event_type" cel:"event_type"`
	ActivityID       string `json:"activity_id,omitempty" cel:"activity_id"`
	ConversationID   string `json:"conversation_id,omitempty" cel:"conversation_id"`
	ConversationType string `json:"conversation_type,omitempty" cel:"conversation_type"`
	TenantID         string `json:"tenant_id,omitempty" cel:"tenant_id"`
	// ServiceURL is the Bot Framework connector endpoint replies must be sent
	// to; outbound tools need it to address the conversation.
	ServiceURL string `json:"service_url,omitempty" cel:"service_url"`
	// TeamsChannelID / TeamsTeamID identify the Teams channel + team for
	// channel conversations (empty in personal and group chats).
	TeamsChannelID string `json:"teams_channel_id,omitempty" cel:"teams_channel_id"`
	TeamsTeamID    string `json:"teams_team_id,omitempty" cel:"teams_team_id"`
	// UserID is the sender's channel account id; UserAADObjectID its Entra ID
	// object id when Teams supplies one.
	UserID          string `json:"user_id,omitempty" cel:"user_id"`
	UserName        string `json:"user_name,omitempty" cel:"user_name"`
	UserAADObjectID string `json:"user_aad_object_id,omitempty" cel:"user_aad_object_id"`
	// BotID is the recipient bot's channel account id.
	BotID     string `json:"bot_id,omitempty" cel:"bot_id"`
	Text      string `json:"text,omitempty" cel:"text"`
	ReplyToID string `json:"reply_to_id,omitempty" cel:"reply_to_id"`
	Timestamp string `json:"timestamp,omitempty" cel:"timestamp"`
	// Action is set on installationUpdate activities: "add" or "remove".
	Action string `json:"action,omitempty" cel:"action"`

	ReactionsAdded   []string `json:"reactions_added,omitempty" cel:"reactions_added"`
	ReactionsRemoved []string `json:"reactions_removed,omitempty" cel:"reactions_removed"`
	MembersAdded     []string `json:"members_added,omitempty" cel:"members_added"`
	MembersRemoved   []string `json:"members_removed,omitempty" cel:"members_removed"`
}

// supportedMSTeamsEventTypes lists the Bot Framework activity types the
// trigger dispatches. Teams also delivers "typing" activities, deliberately
// excluded: an empty event_types config defaults to this whole list, and
// typing indicators would flood every default-config trigger.
var supportedMSTeamsEventTypes = []string{
	"conversationUpdate",
	"endOfConversation",
	"installationUpdate",
	"message",
	"messageDelete",
	"messageReaction",
	"messageUpdate",
}

func msteamsIngest(body []byte, _ http.Header) (*WebhookIngest, error) {
	var activity msteamsActivity
	if err := json.Unmarshal(body, &activity); err != nil {
		return nil, fmt.Errorf("decode msteams activity: %w", err)
	}
	if activity.Type == "" {
		return nil, fmt.Errorf("decode msteams activity: missing type")
	}

	tenantID := activity.Conversation.TenantID
	var teamsChannelID, teamsTeamID string
	if cd := activity.ChannelData; cd != nil {
		if cd.Channel != nil {
			teamsChannelID = cd.Channel.ID
		}
		if cd.Team != nil {
			teamsTeamID = cd.Team.ID
		}
		if tenantID == "" && cd.Tenant != nil {
			tenantID = cd.Tenant.ID
		}
	}

	normalized := msteamsTriggerEvent{
		EventType:        activity.Type,
		ActivityID:       activity.ID,
		ConversationID:   activity.Conversation.ID,
		ConversationType: activity.Conversation.ConversationType,
		TenantID:         tenantID,
		ServiceURL:       activity.ServiceURL,
		TeamsChannelID:   teamsChannelID,
		TeamsTeamID:      teamsTeamID,
		UserID:           activity.From.ID,
		UserName:         activity.From.Name,
		UserAADObjectID:  activity.From.AADObjectID,
		BotID:            activity.Recipient.ID,
		Text:             activity.Text,
		ReplyToID:        activity.ReplyToID,
		Timestamp:        activity.Timestamp,
		Action:           activity.Action,
		ReactionsAdded:   lo.Map(activity.ReactionsAdded, func(r msteamsReaction, _ int) string { return r.Type }),
		ReactionsRemoved: lo.Map(activity.ReactionsRemoved, func(r msteamsReaction, _ int) string { return r.Type }),
		MembersAdded:     lo.Map(activity.MembersAdded, func(m msteamsChannelAccount, _ int) string { return m.ID }),
		MembersRemoved:   lo.Map(activity.MembersRemoved, func(m msteamsChannelAccount, _ int) string { return m.ID }),
	}

	// Teams conversation ids are already thread-scoped where threads exist —
	// replies in a channel carry the thread root in the id's ";messageid=…"
	// suffix — and stable per chat in personal/group conversations, so the
	// conversation id alone is the correct correlation key. Conversation-less
	// activities fall back to the tenant.
	return &WebhookIngest{
		Response:      nil,
		EventID:       activity.ID,
		CorrelationID: conv.Default(activity.Conversation.ID, tenantID),
		Event:         normalized,
	}, nil
}

// botFrameworkAuthenticator validates the signed JWT the Bot Framework
// connector attaches to every request it sends a bot, per
// https://learn.microsoft.com/azure/bot-service/rest-api/bot-framework-rest-connector-authentication:
// RS256 signature against the JWKS advertised by the OpenID metadata document,
// issuer https://api.botframework.com, audience = the bot's Microsoft App ID,
// and a serviceurl claim matching the activity's serviceUrl. The remote key
// set is fetched lazily and cached across requests (go-oidc re-fetches on
// unknown key ids, covering Microsoft's key rotation).
type botFrameworkAuthenticator struct {
	metadataURL string
	issuer      string
	// clientFn lazily builds the guardian-backed client. Deferred past package
	// init (the registry constructs definitions as a package-level var) so the
	// global tracer provider is real by the time the first webhook arrives.
	clientFn func() *guardian.HTTPClient
	// fetchGroup coalesces concurrent metadata fetches so cold-start or
	// post-outage delivery bursts share one request instead of queueing.
	fetchGroup singleflight.Group

	mu     sync.Mutex
	keySet oidc.KeySet
	// retryAfter negative-caches metadata fetch failures. Deliveries arriving
	// before it return immediately instead of waiting out a 10s fetch each —
	// Teams retries failed deliveries, so a metadata endpoint blip would
	// otherwise self-amplify.
	retryAfter time.Time
}

func newBotFrameworkAuthenticator(metadataURL, issuer string) *botFrameworkAuthenticator {
	return &botFrameworkAuthenticator{
		metadataURL: metadataURL,
		issuer:      issuer,
		clientFn: sync.OnceValue(func() *guardian.HTTPClient {
			client := guardian.NewDefaultPolicy(otel.GetTracerProvider()).PooledClient()
			client.Timeout = 10 * time.Second
			return client
		}),
		fetchGroup: singleflight.Group{},
		mu:         sync.Mutex{},
		keySet:     nil,
		retryAfter: time.Time{},
	}
}

func (b *botFrameworkAuthenticator) remoteKeySet() (oidc.KeySet, error) {
	b.mu.Lock()
	cached, retryAfter := b.keySet, b.retryAfter
	b.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	if time.Now().Before(retryAfter) {
		return nil, fmt.Errorf("openid metadata unavailable, retrying later")
	}

	// The fetch runs outside the mutex so deliveries never queue behind the
	// network round-trip; singleflight collapses a concurrent burst into one
	// request whose result (or failure) they all share.
	keySet, err, _ := b.fetchGroup.Do("keyset", func() (any, error) {
		// Re-check under the flight: a caller that read the empty cache
		// before a flight completed enters here after it, and without this it
		// would start a redundant fetch — or bypass the failure backoff.
		b.mu.Lock()
		cached, retryAfter := b.keySet, b.retryAfter
		b.mu.Unlock()
		if cached != nil {
			return cached, nil
		}
		if time.Now().Before(retryAfter) {
			return nil, fmt.Errorf("openid metadata unavailable, retrying later")
		}

		client := b.clientFn()
		jwksURI, err := b.fetchJWKSURI(client)
		if err != nil {
			b.mu.Lock()
			b.retryAfter = time.Now().Add(30 * time.Second)
			b.mu.Unlock()
			return nil, err
		}
		// The key set outlives this request: it caches Microsoft's signing
		// keys and re-fetches them on rotation, so it is bound to a background
		// context carrying our bounded-timeout client rather than the request
		// context.
		keySet := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), jwksURI)
		b.mu.Lock()
		b.keySet = keySet
		b.mu.Unlock()
		return keySet, nil
	})
	if err != nil {
		return nil, err
	}
	set, ok := keySet.(oidc.KeySet)
	if !ok {
		return nil, fmt.Errorf("unexpected key set type %T", keySet)
	}
	return set, nil
}

// fetchJWKSURI deliberately detaches from the inbound delivery's context: the
// metadata result is process-global, and a fetch cancelled by one client's
// disconnect would otherwise write the 30s negative cache and reject every
// subsequent delivery for a failure we caused ourselves.
func (b *botFrameworkAuthenticator) fetchJWKSURI(client *guardian.HTTPClient) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.metadataURL, nil)
	if err != nil {
		return "", fmt.Errorf("build openid metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch openid metadata: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch openid metadata: status %d", resp.StatusCode)
	}
	var metadata struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode openid metadata: %w", err)
	}
	if metadata.JWKSURI == "" {
		return "", fmt.Errorf("openid metadata missing jwks_uri")
	}
	return metadata.JWKSURI, nil
}

// authenticate verifies the bearer token for a webhook delivery whose raw body
// is the activity JSON. appID is the bot's Microsoft App ID (the expected
// token audience).
func (b *botFrameworkAuthenticator) authenticate(ctx context.Context, body []byte, headers http.Header, appID string) error {
	authorization := headers.Get("Authorization")
	scheme, rawToken, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || rawToken == "" {
		return fmt.Errorf("missing bearer token")
	}

	keySet, err := b.remoteKeySet()
	if err != nil {
		return err
	}

	verifier := oidc.NewVerifier(b.issuer, keySet, &oidc.Config{ //nolint:exhaustruct // third-party struct; remaining fields keep default verification behavior
		ClientID:             appID,
		SupportedSigningAlgs: []string{"RS256"},
	})
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return fmt.Errorf("verify bot framework token: %w", err)
	}

	// Bot Framework binds the token to the connector endpoint the activity
	// claims to originate from; a mismatch means the activity was replayed
	// against a different service URL. The claim key is lowercase "serviceurl"
	// (per botbuilder's AuthenticationConstants); encoding/json's
	// case-insensitive fallback also absorbs the camelCase spelling the
	// protocol docs use.
	var claims struct {
		ServiceURL string `json:"serviceurl"`
	}
	if err := token.Claims(&claims); err != nil {
		return fmt.Errorf("decode bot framework token claims: %w", err)
	}
	if claims.ServiceURL == "" {
		return fmt.Errorf("bot framework token missing serviceurl claim")
	}

	var activity struct {
		ServiceURL string `json:"serviceUrl"`
	}
	if err := json.Unmarshal(body, &activity); err != nil {
		return fmt.Errorf("decode msteams activity: %w", err)
	}
	if strings.TrimRight(claims.ServiceURL, "/") != strings.TrimRight(activity.ServiceURL, "/") {
		return fmt.Errorf("activity serviceUrl does not match token serviceurl claim")
	}

	return nil
}

func newMSTeamsDefinition() Definition {
	schema := buildInputSchema[msteamsTriggerConfig](
		withArrayItemsEnum("event_types", toAnySlice(supportedMSTeamsEventTypes)...),
	)
	compiled := mustCompileSchema(schema)
	authenticator := newBotFrameworkAuthenticator(botFrameworkOpenIDConfigURL, botFrameworkIssuer)
	vendor := WebhookVendor{
		Slug:        DefinitionSlugMSTeams,
		Title:       "Microsoft Teams",
		Description: "Receive Bot Framework activities from Microsoft Teams and map them to Gram trigger events.",
		EventType:   reflect.TypeFor[msteamsTriggerEvent](),
		EnvRequirements: []EnvRequirement{{
			Name:        msteamsAppIDEnv,
			Description: "Microsoft App ID of the bot; incoming Bot Framework tokens are validated against it.",
			Required:    true,
		}},
		SecretEnv: "",
		Signature: HMACScheme{NewHash: nil, Header: "", Encoding: "", Prefix: "", Template: "", TimestampHeader: "", TimestampSkew: 0},
		// Bot Framework authenticates with a Microsoft-signed JWT, not an HMAC
		// over the body.
		Authenticate: func(ctx context.Context, body []byte, headers http.Header, env map[string]string) error {
			appID := toolconfig.CIEnvFrom(env).Get(msteamsAppIDEnv)
			if appID == "" {
				return fmt.Errorf("missing %s", msteamsAppIDEnv)
			}
			return authenticator.authenticate(ctx, body, headers, appID)
		},
		SupportedEventTypes: supportedMSTeamsEventTypes,
		PreVerify:           nil,
		Ingest:              msteamsIngest,
	}
	return NewWebhookDefinition(vendor, schema, compiled, func(raw map[string]any) (Config, error) {
		cfg, err := decodeConfig[msteamsTriggerConfig](raw, compiled)
		if err != nil {
			return nil, err
		}
		for _, eventType := range cfg.EventTypes {
			if !slices.Contains(supportedMSTeamsEventTypes, eventType) {
				return nil, fmt.Errorf("unsupported msteams event type %q", eventType)
			}
		}
		prog, err := compileCELFilter(reflect.TypeFor[msteamsTriggerEvent](), cfg.FilterExpr)
		if err != nil {
			return nil, err
		}
		cfg.compiledFilter = prog
		return cfg, nil
	})
}

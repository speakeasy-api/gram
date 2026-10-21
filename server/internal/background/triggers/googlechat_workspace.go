package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"go.opentelemetry.io/otel"
)

const (
	// googleChatMessageCreated selects user posts, excluding edits and deletes.
	googleChatMessageCreated = "google.workspace.chat.message.v1.created"
	// googleChatMessagesCreated carries automatically batched message creations.
	googleChatMessagesCreated = "google.workspace.chat.message.v1.batchCreated"
	// maxGoogleChatBatchMessages bounds work accepted in one delivery.
	maxGoogleChatBatchMessages = 1000
	// googleChatPushIssuer signs authenticated Pub/Sub push identity tokens.
	googleChatPushIssuer = "https://accounts.google.com"
	// googleChatPushTimeout bounds public key retrieval during authentication.
	googleChatPushTimeout = 10 * time.Second
)

var googleChatSpaceResource = regexp.MustCompile(`^spaces/[A-Za-z0-9_-]+$`)
var googleChatUserResource = regexp.MustCompile(`^users/[A-Za-z0-9_-]+$`)
var googleChatPushSubscription = regexp.MustCompile(`^projects/[A-Za-z0-9_-]+/subscriptions/[A-Za-z][A-Za-z0-9._~+%-]*$`)
var googleChatWorkspaceSubscription = regexp.MustCompile(`^subscriptions/[A-Za-z0-9_-]+$`)

// googlechatWorkspaceConfig authorizes one space's message stream and limits
// which participants can invoke the assistant. No field contains credentials.
type googlechatWorkspaceConfig struct {
	// Subscription is the exact Pub/Sub push subscription delivering messages.
	Subscription string `json:"subscription"`

	// WorkspaceSubscription identifies the expected CloudEvents source.
	WorkspaceSubscription string `json:"workspace_subscription"`

	// Audience must match the Pub/Sub push token's configured HTTPS audience.
	Audience string `json:"audience"`

	// ServiceAccountEmail identifies the expected, verified push token identity.
	ServiceAccountEmail string `json:"service_account_email"`

	// UserNames allowlists exact Chat user resources, including external users.
	UserNames []string `json:"user_names"`

	// TextPrefix is a literal, case-sensitive command prefix after whitespace.
	TextPrefix string `json:"text_prefix"`
}

func (c googlechatWorkspaceConfig) validate(spaces []string) error {
	if len(spaces) != 1 || !googleChatSpaceResource.MatchString(spaces[0]) {
		return fmt.Errorf("workspace_events requires exactly one valid space_names entry")
	}
	if !googleChatPushSubscription.MatchString(c.Subscription) || !googleChatWorkspaceSubscription.MatchString(c.WorkspaceSubscription) {
		return fmt.Errorf("workspace_events requires exact Pub/Sub and Workspace subscription resource names")
	}
	audience, err := url.Parse(c.Audience)
	if err != nil || audience.Scheme != "https" || audience.Host == "" || audience.User != nil || audience.Fragment != "" {
		return fmt.Errorf("workspace_events audience must be an absolute HTTPS URL")
	}
	if !strings.HasSuffix(c.ServiceAccountEmail, ".iam.gserviceaccount.com") || strings.Count(c.ServiceAccountEmail, "@") != 1 || strings.ContainsAny(c.ServiceAccountEmail, " \t\r\n") {
		return fmt.Errorf("workspace_events requires a push service account email")
	}
	if len(c.UserNames) == 0 {
		return fmt.Errorf("workspace_events requires an explicit user_names allowlist")
	}
	for _, user := range c.UserNames {
		if !googleChatUserResource.MatchString(user) || user == "users/all" || user == "users/app" {
			return fmt.Errorf("workspace_events requires exact Chat user resource names")
		}
	}
	if strings.TrimSpace(c.TextPrefix) == "" || strings.TrimSpace(c.TextPrefix) != c.TextPrefix {
		return fmt.Errorf("workspace_events text_prefix must be nonempty without surrounding whitespace")
	}
	return nil
}

// googlechatPushEnvelope is the wrapped Pub/Sub push format. JSON decoding of
// Data performs base64 decoding; unwrapped or native Chat payloads are rejected.
type googlechatPushEnvelope struct {
	// Subscription is the Pub/Sub resource that delivered this envelope.
	Subscription string `json:"subscription"`

	// Message carries the published CloudEvent.
	Message struct {
		// Data is the base64-encoded Workspace resource payload.
		Data []byte `json:"data"`

		// Attributes carry the CloudEvents metadata.
		Attributes map[string]string `json:"attributes"`
	} `json:"message"`
}

func ingestGoogleChatWorkspace(body []byte, cfg googlechatTriggerConfig) (*WebhookIngressResult, error) {
	if cfg.WorkspaceEvents == nil {
		return nil, fmt.Errorf("missing workspace_events configuration")
	}
	var push googlechatPushEnvelope
	if err := json.Unmarshal(body, &push); err != nil {
		return nil, fmt.Errorf("decode Google Chat Pub/Sub envelope: %w", err)
	}
	settings := cfg.WorkspaceEvents
	if push.Subscription != settings.Subscription || push.Message.Attributes["ce-source"] != "//workspaceevents.googleapis.com/"+settings.WorkspaceSubscription {
		return nil, fmt.Errorf("unexpected Google Chat subscription")
	}
	kind := push.Message.Attributes["ce-type"]
	if kind != googleChatMessageCreated && kind != googleChatMessagesCreated {
		// Lifecycle, edit, and delete events do not invoke the assistant.
		return &WebhookIngressResult{Events: nil, Response: nil, Event: nil, Task: nil}, nil
	}
	if push.Message.Attributes["ce-specversion"] != "1.0" || push.Message.Attributes["ce-subject"] != "//chat.googleapis.com/"+cfg.SpaceNames[0] {
		return nil, fmt.Errorf("unexpected Google Chat CloudEvent metadata")
	}
	if kind == googleChatMessagesCreated {
		var batch struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(push.Message.Data, &batch); err != nil {
			return nil, fmt.Errorf("decode Google Chat message batch: %w", err)
		}
		if len(batch.Messages) == 0 || len(batch.Messages) > maxGoogleChatBatchMessages {
			return nil, fmt.Errorf("invalid Google Chat message batch size")
		}
		events := make([]*EventEnvelope, 0, len(batch.Messages))
		for _, raw := range batch.Messages {
			event, err := decodeGoogleChatWorkspaceMessage(raw, cfg.SpaceNames[0])
			if err != nil {
				return nil, err
			}
			if event != nil {
				events = append(events, event)
			}
		}
		return &WebhookIngressResult{Events: events, Response: nil, Event: nil, Task: nil}, nil
	}
	event, err := decodeGoogleChatWorkspaceMessage(push.Message.Data, cfg.SpaceNames[0])
	if err != nil {
		return nil, err
	}
	return &WebhookIngressResult{Events: nil, Response: nil, Event: event, Task: nil}, nil
}

func decodeGoogleChatWorkspaceMessage(raw []byte, space string) (*EventEnvelope, error) {
	var interaction googlechatInteraction
	if err := json.Unmarshal(raw, &interaction); err != nil {
		return nil, fmt.Errorf("decode Google Chat message event: %w", err)
	}
	// Each batched entry wraps its own Message, just like a singular event.
	// Never attach the complete batch to one participant's assistant turn.
	message := interaction.Message
	if message.Sender.Type == "BOT" {
		return nil, nil
	}
	if message.Sender.Type != "HUMAN" {
		return nil, fmt.Errorf("google chat event requires message resource data including sender")
	}
	if !googleChatUserResource.MatchString(message.Sender.Name) ||
		!validGoogleChatChild(message.Name, space+"/messages/") ||
		!validGoogleChatChild(message.Thread.Name, space+"/threads/") {
		return nil, fmt.Errorf("invalid Google Chat message, thread or sender resource")
	}
	event := googlechatTriggerEvent{
		EventType: "MESSAGE", MessageName: message.Name, SpaceName: space,
		SpaceType: "SPACE", ThreadName: message.Thread.Name, UserName: message.Sender.Name,
		Text: message.Text, Timestamp: message.CreateTime, Addressed: true,
	}
	return &EventEnvelope{
		EventID: message.Name, CorrelationID: message.Thread.Name,
		TriggerInstanceID: "", DefinitionSlug: DefinitionSlugGoogleChat,
		Event: event, RawPayload: raw, ReceivedAt: time.Now().UTC(),
	}, nil
}

func validGoogleChatChild(resource, prefix string) bool {
	id, ok := strings.CutPrefix(resource, prefix)
	return ok && id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/?#%\\ \t\r\n")
}

func authenticateGoogleChatPush(ctx context.Context, keys oidc.KeySet, headers http.Header, cfg googlechatWorkspaceConfig, now time.Time) error {
	scheme, raw, ok := strings.Cut(headers.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return fmt.Errorf("missing Pub/Sub bearer token")
	}
	verifier := oidc.NewVerifier(googleChatPushIssuer, keys, &oidc.Config{
		ClientID: cfg.Audience, SupportedSigningAlgs: []string{"RS256"}, Now: func() time.Time { return now },
		SkipClientIDCheck: false, SkipExpiryCheck: false, SkipIssuerCheck: false, InsecureSkipSignatureCheck: false,
	})
	token, err := verifier.Verify(ctx, raw)
	if err != nil {
		return fmt.Errorf("verify Google Chat Pub/Sub token: %w", err)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return fmt.Errorf("decode Google Chat Pub/Sub identity: %w", err)
	}
	if !claims.EmailVerified || claims.Email != cfg.ServiceAccountEmail {
		return fmt.Errorf("unexpected Google Chat Pub/Sub identity")
	}
	if token.IssuedAt.IsZero() || token.IssuedAt.After(now.Add(time.Minute)) || !token.Expiry.After(token.IssuedAt) {
		return fmt.Errorf("invalid Google Chat Pub/Sub token lifetime")
	}
	return nil
}

func withGoogleChatWorkspaceEvents(definition Definition) Definition {
	keys := sync.OnceValue(func() oidc.KeySet {
		client := guardian.NewDefaultPolicy(otel.GetTracerProvider()).PooledClient()
		client.Timeout = googleChatPushTimeout
		return oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), "https://www.googleapis.com/oauth2/v3/certs")
	})
	authenticateInteraction, handleInteraction := definition.AuthenticateWebhook, definition.HandleWebhook
	definition.Description += " Optionally receive allowlisted text commands in one shared space through an authenticated Workspace Events Pub/Sub subscription."
	definition.AuthenticateWebhook = func(ctx context.Context, body []byte, headers http.Header, env map[string]string, config Config) error {
		cfg, ok := config.(googlechatTriggerConfig)
		if !ok {
			return fmt.Errorf("invalid Google Chat configuration")
		}
		if cfg.WorkspaceEvents == nil {
			return authenticateInteraction(ctx, body, headers, env, config)
		}
		ctx, cancel := context.WithTimeout(ctx, googleChatPushTimeout)
		defer cancel()
		return authenticateGoogleChatPush(ctx, keys(), headers, *cfg.WorkspaceEvents, time.Now())
	}
	definition.HandleWebhook = func(body []byte, headers http.Header, config Config) (*WebhookIngressResult, error) {
		cfg, ok := config.(googlechatTriggerConfig)
		if !ok {
			return nil, fmt.Errorf("invalid Google Chat configuration")
		}
		if cfg.WorkspaceEvents == nil {
			return handleInteraction(body, headers, config)
		}
		return ingestGoogleChatWorkspace(body, cfg)
	}
	return definition
}

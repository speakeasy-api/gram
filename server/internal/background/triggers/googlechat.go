package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/cel-go/cel"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"go.opentelemetry.io/otel"
)

const googleChatProjectNumberEnv = "GOOGLE_CHAT_PROJECT_NUMBER"
const googleChatIssuer = "chat@system.gserviceaccount.com"

// v1 supports non-add-on Chat apps configured with Project Number authentication
// audience, not Workspace add-on OIDC or Workspace Events subscriptions. See
// https://developers.google.com/workspace/chat/verify-requests-from-chat#authenticate_requests_with_a_project_number_jwt
// Google publishes the same service-account public keys in JWK and X509 forms.
const googleChatKeysURL = "https://www.googleapis.com/service_accounts/v1/jwk/" + googleChatIssuer

var googleChatProjectNumberPattern = regexp.MustCompile(`^[0-9]+$`)

type googlechatTriggerConfig struct {
	FilterExpr     string   `json:"filter,omitempty"`
	SpaceNames     []string `json:"space_names,omitempty"`
	compiledFilter cel.Program
}

func (c googlechatTriggerConfig) Filter(event any) (bool, error) {
	e, ok := event.(googlechatTriggerEvent)
	if !ok {
		return false, fmt.Errorf("expected googlechatTriggerEvent, got %T", event)
	}
	if len(c.SpaceNames) > 0 && !slices.Contains(c.SpaceNames, e.SpaceName) {
		return false, nil
	}
	return evalWebhookFilter(c.compiledFilter, nil, event, e.EventType, []string{"MESSAGE"})
}

// Chat only sends MESSAGE interactions for DMs and explicit app mentions. Never
// infer mentions from untrusted text or ingest ambient Workspace Events here.
type googlechatTriggerEvent struct {
	EventType   string `json:"event_type" cel:"event_type"`
	MessageName string `json:"message_name" cel:"message_name"`
	SpaceName   string `json:"space_name" cel:"space_name"`
	SpaceType   string `json:"space_type" cel:"space_type"`
	ThreadName  string `json:"thread_name" cel:"thread_name"`
	UserName    string `json:"user_name" cel:"user_name"`
	Text        string `json:"text" cel:"text"`
	Timestamp   string `json:"timestamp" cel:"timestamp"`
	Addressed   bool   `json:"addressed" cel:"addressed"`
}

type googlechatInteraction struct {
	Type      string `json:"type"`
	EventTime string `json:"eventTime"`
	Space     struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		SpaceType string `json:"spaceType"`
	} `json:"space"`
	Message struct {
		Name       string `json:"name"`
		Text       string `json:"text"`
		CreateTime string `json:"createTime"`
		Sender     struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"sender"`
		Thread struct {
			Name string `json:"name"`
		} `json:"thread"`
	} `json:"message"`
}

func googlechatIngest(body []byte, _ http.Header) (*WebhookIngest, error) {
	// Chat expects an empty JSON object when a reply will arrive asynchronously.
	// Carry this response even for ignored events so the generic webhook HTTP
	// handler never falls back to its provider-neutral {"ok":true} response.
	response := &WebhookResponse{Status: http.StatusOK, ContentType: "application/json", Body: []byte(`{}`)}
	var interaction googlechatInteraction
	if err := json.Unmarshal(body, &interaction); err != nil {
		return nil, fmt.Errorf("decode Google Chat interaction: %w", err)
	}
	if interaction.Type != "MESSAGE" || interaction.Message.Sender.Type != "HUMAN" {
		return &WebhookIngest{Response: response, Event: nil, EventID: "", CorrelationID: ""}, nil
	}
	m := interaction.Message
	space := interaction.Space
	spaceType := space.SpaceType
	if spaceType == "" {
		switch space.Type {
		case "DM":
			spaceType = "DIRECT_MESSAGE"
		case "ROOM", "SPACE":
			spaceType = "SPACE"
		default:
			spaceType = space.Type
		}
	}
	if spaceType != "DIRECT_MESSAGE" && spaceType != "GROUP_CHAT" && spaceType != "SPACE" {
		return &WebhookIngest{Response: response, Event: nil, EventID: "", CorrelationID: ""}, nil
	}
	if !strings.HasPrefix(space.Name, "spaces/") || !strings.HasPrefix(m.Name, space.Name+"/messages/") || !strings.HasPrefix(m.Thread.Name, space.Name+"/threads/") || m.Sender.Name == "" {
		return nil, fmt.Errorf("google chat message missing valid space, message, thread or sender resource name")
	}
	return &WebhookIngest{
		Response:      response,
		EventID:       m.Name,
		CorrelationID: m.Thread.Name,
		Event:         googlechatTriggerEvent{EventType: "MESSAGE", MessageName: m.Name, SpaceName: space.Name, SpaceType: spaceType, ThreadName: m.Thread.Name, UserName: m.Sender.Name, Text: m.Text, Timestamp: m.CreateTime, Addressed: true},
	}, nil
}

func authenticateGoogleChat(ctx context.Context, keys oidc.KeySet, headers http.Header, projectNumber string) error {
	if !googleChatProjectNumberPattern.MatchString(projectNumber) {
		return fmt.Errorf("missing or invalid %s", googleChatProjectNumberEnv)
	}
	scheme, raw, ok := strings.Cut(headers.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return fmt.Errorf("missing bearer token")
	}
	verifier := oidc.NewVerifier(googleChatIssuer, keys, &oidc.Config{ClientID: projectNumber, SupportedSigningAlgs: []string{"RS256"}, SkipClientIDCheck: false, SkipExpiryCheck: false, SkipIssuerCheck: false, Now: nil, InsecureSkipSignatureCheck: false})
	token, err := verifier.Verify(ctx, raw)
	if err != nil {
		return fmt.Errorf("verify Google Chat token: %w", err)
	}
	// Require a valid issuance time and expiration, without imposing an
	// undocumented maximum lifetime on Google-issued tokens.
	if token.IssuedAt.IsZero() || token.IssuedAt.After(time.Now().Add(time.Minute)) || !token.Expiry.After(token.IssuedAt) {
		return fmt.Errorf("invalid Google Chat token lifetime")
	}
	return nil
}

func newGoogleChatDefinition() Definition {
	schema := buildInputSchema[googlechatTriggerConfig]()
	compiled := mustCompileSchema(schema)
	keys := sync.OnceValue(func() oidc.KeySet {
		client := guardian.NewDefaultPolicy(otel.GetTracerProvider()).PooledClient()
		client.Timeout = 10 * time.Second
		return oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), googleChatKeysURL)
	})
	vendor := WebhookVendor{
		SecretEnv: "",
		Signature: HMACScheme{NewHash: nil, Header: "", Encoding: "", Prefix: "", Template: "", TimestampHeader: "", TimestampSkew: 0},
		PreVerify: nil,
		Slug:      DefinitionSlugGoogleChat, Title: "Google Chat",
		Description:         "Receive direct messages and @mentions from a non-add-on Google Chat app configured with Project Number authentication audience.",
		EventType:           reflect.TypeFor[googlechatTriggerEvent](),
		EnvRequirements:     []EnvRequirement{{Name: googleChatProjectNumberEnv, Description: "Cloud project number of the non-add-on Chat app; select Project Number as its authentication audience.", Required: true}},
		SupportedEventTypes: []string{"MESSAGE"},
		Authenticate: func(ctx context.Context, _ []byte, headers http.Header, env map[string]string) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return authenticateGoogleChat(ctx, keys(), headers, toolconfig.CIEnvFrom(env).Get(googleChatProjectNumberEnv))
		},
		Ingest: googlechatIngest,
	}
	return NewWebhookDefinition(vendor, schema, compiled, func(raw map[string]any) (Config, error) {
		cfg, err := decodeConfig[googlechatTriggerConfig](raw, compiled)
		if err != nil {
			return nil, err
		}
		for _, space := range cfg.SpaceNames {
			if !strings.HasPrefix(space, "spaces/") || len(strings.TrimPrefix(space, "spaces/")) == 0 {
				return nil, fmt.Errorf("invalid Google Chat space name %q", space)
			}
		}
		cfg.compiledFilter, err = compileCELFilter(reflect.TypeFor[googlechatTriggerEvent](), cfg.FilterExpr)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	})
}

func scopeGoogleChatCorrelation(instanceID, threadName string) string {
	return "googlechat:" + instanceID + ":" + threadName
}

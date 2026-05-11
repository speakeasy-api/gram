package triggers

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // sha-1 is supported because vendors still use it; gate behind explicit config.
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// webhookTriggerConfig powers the generic "webhook" trigger. Vendors (Linear,
// GitHub, Stripe, …) describe their signing scheme + field extraction via
// configuration rather than vendor-specific Go.
type webhookTriggerConfig struct {
	Signature         webhookSignatureConfig `json:"signature"`
	Extractors        webhookExtractors      `json:"extractors"`
	AllowedEventTypes []string               `json:"allowed_event_types,omitempty"`
	FilterExpr        string                 `json:"filter,omitempty"`

	compiledFilter        cel.Program
	compiledEventType     cel.Program
	compiledCorrelationID cel.Program
}

type webhookSignatureConfig struct {
	// Algorithm picks the verifier: "hmac-sha256", "hmac-sha1", or "none". Empty
	// is treated as "none".
	Algorithm string `json:"algorithm,omitempty"`
	// Header is the request header carrying the signature (e.g. "Linear-Signature").
	Header string `json:"header,omitempty"`
	// Encoding controls how the computed MAC is rendered: "hex" (default) or "base64".
	Encoding string `json:"encoding,omitempty"`
	// Prefix is a literal string prepended to the encoded MAC before comparison
	// (e.g. "sha256=" for GitHub, "v0=" for Slack).
	Prefix string `json:"prefix,omitempty"`
	// SignTemplate shapes the bytes fed into the MAC. Placeholders: "{body}",
	// "{timestamp}". Defaults to "{body}".
	SignTemplate string `json:"sign_template,omitempty"`
	// TimestampHeader names a request header carrying a unix timestamp to
	// substitute into SignTemplate and to bound replay against now.
	TimestampHeader string `json:"timestamp_header,omitempty"`
	// TimestampSkewSeconds caps how far the timestamp may drift from now.
	// Defaults to 300 when TimestampHeader is set.
	TimestampSkewSeconds int `json:"timestamp_skew_seconds,omitempty"`
	// SecretEnv names the environment-variable that holds the signing secret
	// for this trigger instance. The secret never lives in trigger config.
	SecretEnv string `json:"secret_env,omitempty"`
}

type webhookExtractors struct {
	// EventType is a CEL expression returning a string; produces event.event_type.
	EventType string `json:"event_type,omitempty"`
	// CorrelationID is a CEL expression returning a string; produces event.correlation_id.
	CorrelationID string `json:"correlation_id,omitempty"`
}

// webhookTriggerEvent is the normalized event surfaced to filters and downstream
// consumers. Payload is the JSON body decoded as a generic map, so CEL filters
// can navigate vendor-specific shapes with `event.payload.foo.bar`.
type webhookTriggerEvent struct {
	Payload       map[string]any    `json:"payload" cel:"payload"`
	Headers       map[string]string `json:"headers,omitempty" cel:"headers"`
	EventType     string            `json:"event_type,omitempty" cel:"event_type"`
	CorrelationID string            `json:"correlation_id,omitempty" cel:"correlation_id"`
	ReceivedAt    string            `json:"received_at,omitempty" cel:"received_at"`
}

func (c webhookTriggerConfig) Filter(event any) (bool, error) {
	webhookEvent, ok := event.(webhookTriggerEvent)
	if !ok {
		return false, fmt.Errorf("expected webhookTriggerEvent, got %T", event)
	}

	if len(c.AllowedEventTypes) > 0 {
		if webhookEvent.EventType == "" || !slices.Contains(c.AllowedEventTypes, webhookEvent.EventType) {
			return false, nil
		}
	}

	if c.compiledFilter == nil {
		return true, nil
	}
	out, _, err := c.compiledFilter.Eval(map[string]any{"event": event})
	if err != nil {
		return false, fmt.Errorf("evaluate filter: %w", err)
	}
	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("filter result was %T, want bool", out.Value())
	}
	return val, nil
}

func newWebhookDefinition() Definition {
	schema := buildInputSchema[webhookTriggerConfig]()
	compiled := mustCompileSchema(schema)
	return Definition{
		Slug:                 "webhook",
		Title:                "Webhook",
		Description:          "Generic webhook trigger: configurable signature verification and CEL-based field extraction.",
		Kind:                 KindWebhook,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements:      nil,
		EventType:            reflect.TypeFor[webhookTriggerEvent](),
		DecodeConfig: func(raw map[string]any) (Config, error) {
			cfg, err := decodeConfig[webhookTriggerConfig](raw, compiled)
			if err != nil {
				return nil, err
			}
			if err := validateWebhookSignatureConfig(cfg.Signature); err != nil {
				return nil, err
			}
			extractorEnv, err := newWebhookExtractorEnv()
			if err != nil {
				return nil, err
			}
			if cfg.compiledEventType, err = compileWebhookExtractor(extractorEnv, cfg.Extractors.EventType); err != nil {
				return nil, fmt.Errorf("compile event_type extractor: %w", err)
			}
			if cfg.compiledCorrelationID, err = compileWebhookExtractor(extractorEnv, cfg.Extractors.CorrelationID); err != nil {
				return nil, fmt.Errorf("compile correlation_id extractor: %w", err)
			}
			prog, err := compileCELFilter(reflect.TypeFor[webhookTriggerEvent](), cfg.FilterExpr)
			if err != nil {
				return nil, err
			}
			cfg.compiledFilter = prog
			return cfg, nil
		},
		AuthenticateWebhook: func(body []byte, headers http.Header, env map[string]string, config Config) error {
			cfg, ok := config.(webhookTriggerConfig)
			if !ok {
				return fmt.Errorf("expected webhookTriggerConfig, got %T", config)
			}
			if cfg.Signature.Algorithm == "" || cfg.Signature.Algorithm == "none" {
				return nil
			}
			if cfg.Signature.SecretEnv == "" {
				return fmt.Errorf("signature.secret_env is required when algorithm is set")
			}
			ciEnv := toolconfig.CIEnvFrom(env)
			signingSecret := ciEnv.Get(cfg.Signature.SecretEnv)
			if signingSecret == "" {
				return fmt.Errorf("missing %s", cfg.Signature.SecretEnv)
			}
			return validateWebhookSignature(body, headers, signingSecret, cfg.Signature)
		},
		HandleWebhook: func(body []byte, headers http.Header, config Config) (*WebhookIngressResult, error) {
			cfg, ok := config.(webhookTriggerConfig)
			if !ok {
				return nil, fmt.Errorf("expected webhookTriggerConfig, got %T", config)
			}

			var payload map[string]any
			if len(body) > 0 {
				_ = json.Unmarshal(body, &payload)
			}
			if payload == nil {
				payload = map[string]any{}
			}

			headerMap := flattenHeaders(headers)

			eventType, err := evalWebhookExtractor(cfg.compiledEventType, payload, headerMap)
			if err != nil {
				return nil, fmt.Errorf("evaluate event_type extractor: %w", err)
			}
			correlationID, err := evalWebhookExtractor(cfg.compiledCorrelationID, payload, headerMap)
			if err != nil {
				return nil, fmt.Errorf("evaluate correlation_id extractor: %w", err)
			}

			receivedAt := time.Now().UTC()
			event := webhookTriggerEvent{
				Payload:       payload,
				Headers:       headerMap,
				EventType:     eventType,
				CorrelationID: correlationID,
				ReceivedAt:    receivedAt.Format(time.RFC3339Nano),
			}

			eventID := uuid.NewSHA1(uuid.NameSpaceURL, body).String()
			if correlationID == "" {
				correlationID = eventID
			}

			return &WebhookIngressResult{
				Response: nil,
				Event: &EventEnvelope{
					EventID:           eventID,
					CorrelationID:     correlationID,
					TriggerInstanceID: "",
					DefinitionSlug:    "webhook",
					Event:             event,
					RawPayload:        body,
					ReceivedAt:        receivedAt,
				},
				Task: nil,
			}, nil
		},
		BuildScheduledEvent: nil,
		ExtractSchedule:     nil,
	}
}

func validateWebhookSignatureConfig(cfg webhookSignatureConfig) error {
	switch cfg.Algorithm {
	case "", "none":
		return nil
	case "hmac-sha256", "hmac-sha1":
		if cfg.Header == "" {
			return fmt.Errorf("signature.header is required when algorithm is %q", cfg.Algorithm)
		}
		if cfg.SecretEnv == "" {
			return fmt.Errorf("signature.secret_env is required when algorithm is %q", cfg.Algorithm)
		}
		switch cfg.Encoding {
		case "", "hex", "base64":
		default:
			return fmt.Errorf("unsupported signature.encoding %q", cfg.Encoding)
		}
		return nil
	default:
		return fmt.Errorf("unsupported signature.algorithm %q", cfg.Algorithm)
	}
}

func validateWebhookSignature(body []byte, headers http.Header, signingSecret string, cfg webhookSignatureConfig) error {
	presented := headers.Get(cfg.Header)
	if presented == "" {
		return fmt.Errorf("missing %s header", cfg.Header)
	}

	timestamp := ""
	if cfg.TimestampHeader != "" {
		timestamp = headers.Get(cfg.TimestampHeader)
		if timestamp == "" {
			return fmt.Errorf("missing %s header", cfg.TimestampHeader)
		}
		skew := cfg.TimestampSkewSeconds
		if skew <= 0 {
			skew = 300
		}
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", cfg.TimestampHeader, err)
		}
		now := time.Now().Unix()
		if absInt64(now-ts) > int64(skew) {
			return fmt.Errorf("%s outside skew window", cfg.TimestampHeader)
		}
	}

	var mac hash.Hash
	switch cfg.Algorithm {
	case "hmac-sha256":
		mac = hmac.New(sha256.New, []byte(signingSecret))
	case "hmac-sha1":
		mac = hmac.New(sha1.New, []byte(signingSecret))
	default:
		return fmt.Errorf("unsupported algorithm %q", cfg.Algorithm)
	}

	signTemplate := cfg.SignTemplate
	if signTemplate == "" {
		signTemplate = "{body}"
	}
	signed := strings.ReplaceAll(signTemplate, "{body}", string(body))
	signed = strings.ReplaceAll(signed, "{timestamp}", timestamp)
	mac.Write([]byte(signed))

	sum := mac.Sum(nil)
	var encoded string
	switch cfg.Encoding {
	case "", "hex":
		encoded = hex.EncodeToString(sum)
	case "base64":
		encoded = base64.StdEncoding.EncodeToString(sum)
	default:
		return fmt.Errorf("unsupported encoding %q", cfg.Encoding)
	}
	expected := cfg.Prefix + encoded

	if !hmac.Equal([]byte(expected), []byte(presented)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func newWebhookExtractorEnv() (*cel.Env, error) {
	env, err := cel.NewEnv(
		ext.Strings(),
		cel.Variable("body", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("headers", cel.MapType(cel.StringType, cel.StringType)),
	)
	if err != nil {
		return nil, fmt.Errorf("create webhook extractor env: %w", err)
	}
	return env, nil
}

func compileWebhookExtractor(env *cel.Env, expression string) (cel.Program, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, nil
	}
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile extractor: %w", issues.Err())
	}
	// Output type may be dyn when navigating into the decoded JSON map; coerce
	// to string at eval time. Reject types that we know can't become strings
	// (bool, list, map) so misconfiguration surfaces early.
	switch ast.OutputType() {
	case cel.StringType, cel.DynType, cel.IntType, cel.UintType, cel.DoubleType:
	default:
		return nil, fmt.Errorf("extractor must evaluate to string, got %s", ast.OutputType())
	}
	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("build extractor program: %w", err)
	}
	return prog, nil
}

func evalWebhookExtractor(prog cel.Program, body map[string]any, headers map[string]string) (string, error) {
	if prog == nil {
		return "", nil
	}
	out, _, err := prog.Eval(map[string]any{
		"body":    body,
		"headers": headers,
	})
	if err != nil {
		return "", fmt.Errorf("eval: %w", err)
	}
	switch v := out.Value().(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case int64, uint64, float64, bool:
		return fmt.Sprintf("%v", v), nil
	default:
		return "", fmt.Errorf("extractor returned %T, want string-convertible value", v)
	}
}

// flattenHeaders surfaces single-value headers as a string map for CEL access.
// Multi-value headers collapse to their first value, matching the common
// vendor-webhook shape where headers are scalars.
func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) == 0 {
			continue
		}
		out[k] = v[0]
	}
	return out
}

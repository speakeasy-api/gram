package triggers

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/uuid"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// HMACScheme describes a vendor's request-signature scheme. It covers the
// HMAC-based schemes used by Slack, Linear, GitHub, Stripe, and most other
// webhook senders. NewHash is the constructor (e.g. hmac.New(sha256.New, key));
// when nil, authentication is disabled (no signature is required).
type HMACScheme struct {
	// NewHash returns a fresh HMAC writer keyed by the signing secret. Nil
	// disables signature verification.
	NewHash func(key []byte) hash.Hash
	// Header names the request header carrying the signature.
	Header string
	// Encoding renders the computed MAC: "hex" (default) or "base64".
	Encoding string
	// Prefix is prepended to the encoded MAC before constant-time compare
	// (e.g. "sha256=" for GitHub, "v0=" for Slack).
	Prefix string
	// Template shapes the bytes fed to the MAC. Placeholders: {body}, {timestamp}.
	// Defaults to "{body}".
	Template string
	// TimestampHeader names a request header carrying a unix timestamp
	// substituted into Template and bounded for replay protection. Empty
	// disables timestamp checking.
	TimestampHeader string
	// TimestampSkew caps how far the timestamp may drift from now. Defaults
	// to 300s when TimestampHeader is set.
	TimestampSkew time.Duration
}

// Verify computes the expected signature for body under secret and compares
// it against the value in s.Header using a constant-time comparison. When a
// TimestampHeader is configured the timestamp is parsed and bounded by
// TimestampSkew to reject replays.
func (s HMACScheme) Verify(body []byte, headers http.Header, secret string) error {
	presented := headers.Get(s.Header)
	if presented == "" {
		return fmt.Errorf("missing signature header")
	}

	timestamp := ""
	if s.TimestampHeader != "" {
		timestamp = headers.Get(s.TimestampHeader)
		if timestamp == "" {
			return fmt.Errorf("missing timestamp header")
		}
		skew := s.TimestampSkew
		if skew <= 0 {
			skew = 300 * time.Second
		}
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return fmt.Errorf("parse timestamp: %w", err)
		}
		now := time.Now().Unix()
		if absInt64(now-ts) > int64(skew.Seconds()) {
			return fmt.Errorf("timestamp outside skew window")
		}
	}

	template := s.Template
	if template == "" {
		template = "{body}"
	}
	signed := renderSignTemplate(template, body, timestamp)

	mac := s.NewHash([]byte(secret))
	if _, err := mac.Write(signed); err != nil {
		return fmt.Errorf("hash signature: %w", err)
	}

	var encoded string
	switch s.Encoding {
	case "", "hex":
		encoded = hex.EncodeToString(mac.Sum(nil))
	case "base64":
		encoded = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	default:
		return fmt.Errorf("unsupported encoding %q", s.Encoding)
	}
	expected := s.Prefix + encoded

	if !hmac.Equal([]byte(expected), []byte(presented)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// renderSignTemplate substitutes {body} and {timestamp} into template in a
// single pass, so a body that happens to contain the literal "{timestamp}"
// cannot collide with the timestamp substitution (a two-pass ReplaceAll would
// corrupt the MAC input when the body carried that token).
func renderSignTemplate(template string, body []byte, timestamp string) []byte {
	var b strings.Builder
	b.Grow(len(template) + len(body) + len(timestamp))
	i := 0
	for i < len(template) {
		switch {
		case strings.HasPrefix(template[i:], "{body}"):
			b.Write(body)
			i += len("{body}")
		case strings.HasPrefix(template[i:], "{timestamp}"):
			b.WriteString(timestamp)
			i += len("{timestamp}")
		default:
			b.WriteByte(template[i])
			i++
		}
	}
	return []byte(b.String())
}

// WebhookVendor is the per-vendor description a webhook trigger implementation
// supplies. NewWebhookDefinition assembles the shared Definition wiring
// (authenticate via HMACScheme, ingest into a normalized EventEnvelope,
// default-deny event-type filtering) around it, so adding a new webhook source
// is mostly a matter of describing its signature scheme, env var, event types,
// and an Ingest function that decodes the body into a typed event plus
// correlation id.
type WebhookVendor struct {
	Slug                string
	Title               string
	Description         string
	EventType           reflect.Type
	EnvRequirements     []EnvRequirement
	SecretEnv           string
	Signature           HMACScheme
	SupportedEventTypes []string
	// PreVerify inspects the request before signature verification. Return
	// skipSignature=true to authorize the request without checking the HMAC
	// (e.g. Slack's url_verification handshake, which must echo a challenge
	// before any signing secret is necessarily configured). Return an error
	// to reject. May be nil, in which case signature verification always runs.
	PreVerify func(body []byte, headers http.Header) (skipSignature bool, err error)
	// Ingest decodes body + headers into a normalized event for downstream
	// consumers, the vendor's delivery id (used for dedup; falls back to a
	// content hash when empty), and the correlation id that routes the event
	// to an assistant conversation. A nil Response with a nil Event acks
	// without dispatching (e.g. an unsupported Slack interaction envelope).
	Ingest func(body []byte, headers http.Header) (*WebhookIngest, error)
}

// WebhookIngest is the result of a vendor's Ingest step: an optional inline
// response (e.g. Slack's url_verification challenge echo), the normalized
// event, and the routing metadata that NewWebhookDefinition folds into the
// EventEnvelope.
type WebhookIngest struct {
	Response      *WebhookResponse
	Event         any
	EventID       string
	CorrelationID string
}

// NewWebhookDefinition builds a Definition from a WebhookVendor, the
// JSON-schema-described config (built by the vendor via buildInputSchema),
// and a DecodeConfig closure that decodes raw config, validates event types,
// and compiles the CEL filter. The shared wiring — HMAC authentication with
// optional pre-verify bypass, envelope assembly with vendor-supplied event id
// + correlation id, content-hash event-id fallback — lives here so vendors
// don't reimplement it.
func NewWebhookDefinition(
	vendor WebhookVendor,
	schema []byte,
	compiled *jsonschema.Schema,
	decodeConfigFn func(raw map[string]any) (Config, error),
) Definition {
	return Definition{
		Slug:                 vendor.Slug,
		Title:                vendor.Title,
		Description:          vendor.Description,
		Kind:                 KindWebhook,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements:      vendor.EnvRequirements,
		EventType:            vendor.EventType,
		DecodeConfig:         decodeConfigFn,
		AuthenticateWebhook: func(body []byte, headers http.Header, env map[string]string, _ Config) error {
			if vendor.PreVerify != nil {
				skip, err := vendor.PreVerify(body, headers)
				if err != nil {
					return err
				}
				if skip {
					return nil
				}
			}
			if vendor.Signature.NewHash == nil {
				return nil
			}
			ciEnv := toolconfig.CIEnvFrom(env)
			secret := ciEnv.Get(vendor.SecretEnv)
			if secret == "" {
				return fmt.Errorf("missing signing secret")
			}
			return vendor.Signature.Verify(body, headers, secret)
		},
		HandleWebhook: func(body []byte, headers http.Header, _ Config) (*WebhookIngressResult, error) {
			ingest, err := vendor.Ingest(body, headers)
			if err != nil {
				return nil, err
			}
			if ingest == nil || (ingest.Response == nil && ingest.Event == nil) {
				return &WebhookIngressResult{Response: nil, Event: nil, Task: nil}, nil
			}
			if ingest.Response != nil && ingest.Event == nil {
				return &WebhookIngressResult{Response: ingest.Response, Event: nil, Task: nil}, nil
			}
			// EventID and CorrelationID fallbacks are applied by the dispatcher
			// (App.ProcessWebhook) once the trigger instance id is known. The
			// content-hash fallback must be scoped per instance — a bare body
			// hash is identical across instances that receive the same payload,
			// which would collide on the downstream dedup keys.
			return &WebhookIngressResult{
				Response: ingest.Response,
				Event: &EventEnvelope{
					EventID:           ingest.EventID,
					CorrelationID:     ingest.CorrelationID,
					TriggerInstanceID: "",
					DefinitionSlug:    vendor.Slug,
					Event:             ingest.Event,
					RawPayload:        body,
					ReceivedAt:        time.Now().UTC(),
				},
				Task: nil,
			}, nil
		},
		BuildScheduledEvent: nil,
		BuildDirectEvent:    nil,
		ExtractSchedule:     nil,
	}
}

// scopeWebhookEventID produces the per-instance dedup event id for a webhook
// delivery. A delivery targets exactly one trigger instance, so deduping across
// instances is never correct; prefixing the instance id keeps redelivery dedup
// within an instance (same instance + same vendor id or body collapses) while
// keeping instances independent. Both the Temporal dispatch workflow id and the
// assistant enqueue key (project_id, assistant_id, event_id) dedupe on this id,
// so scoping it here covers both layers — and covers vendor-supplied ids too,
// not just the content-hash fallback. vendorID is the vendor's delivery/event
// id, or empty to fall back to a content hash of the body.
func scopeWebhookEventID(triggerInstanceID, vendorID string, body []byte) string {
	id := vendorID
	if id == "" {
		id = uuid.NewSHA1(uuid.NameSpaceURL, body).String()
	}
	return triggerInstanceID + ":" + id
}

// evalWebhookFilter applies the default-deny event-type allowlist (defaulting
// to the vendor's SupportedEventTypes when the instance config leaves
// event_types empty) and then the compiled CEL filter. Each vendor's config
// Filter method is a thin typed wrapper that extracts its event's event_type
// field and delegates here, so the allowlist + CEL semantics stay uniform
// across every webhook source.
func evalWebhookFilter(compiled cel.Program, configuredEventTypes []string, event any, eventType string, supported []string) (bool, error) {
	allowed := configuredEventTypes
	if len(allowed) == 0 {
		allowed = supported
	}
	if eventType == "" || !slices.Contains(allowed, eventType) {
		return false, nil
	}
	if compiled == nil {
		return true, nil
	}
	out, _, err := compiled.Eval(map[string]any{"event": event})
	if err != nil {
		return false, fmt.Errorf("evaluate filter: %w", err)
	}
	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("filter result was %T, want bool", out.Value())
	}
	return val, nil
}

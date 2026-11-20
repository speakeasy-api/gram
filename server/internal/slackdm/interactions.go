package slackdm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// InteractionsPath is the Slack app's interactivity request URL.
	InteractionsPath = "/slack/interactions"

	// MaxInteractionBytes bounds a click payload. Slack's block_actions
	// payloads carry the message or view they came from and stay well under
	// this.
	MaxInteractionBytes = 1 << 20 // 1 MiB

	// slackResponseHost is the only host a response_url may point at.
	slackResponseHost = "hooks.slack.com"

	// responseTimeout bounds the reply to a click.
	responseTimeout = 10 * time.Second
)

// VerifySignature checks Slack's request signature: v0=HMAC-SHA256 over
// "v0:{timestamp}:{body}" with the app's signing secret, with timestamps more
// than 5 minutes off rejected.
func VerifySignature(body []byte, header http.Header, signingSecret string) error {
	if err := triggers.SlackSignature.Verify(body, header, signingSecret); err != nil {
		return fmt.Errorf("verify Slack signature: %w", err)
	}
	return nil
}

// Responder replies to a click through its response_url.
type Responder interface {
	Respond(ctx context.Context, responseURL string, message ResponseMessage) error
}

// ResponseMessage is a reply to a click. It either replaces the message the
// click came from or adds an ephemeral note only the clicker sees.
type ResponseMessage struct {
	// ResponseType is ephemeral for a note only the clicker sees.
	ResponseType string `json:"response_type,omitempty"`

	// ReplaceOriginal replaces the message the click came from.
	ReplaceOriginal bool `json:"replace_original"`

	// Text is the reply's text, and the fallback when it has blocks.
	Text string `json:"text"`

	// Blocks replace the original message's blocks.
	Blocks []Block `json:"blocks,omitempty"`
}

// Ephemeral is a reply only the clicker sees, leaving the message as it is.
func Ephemeral(text string) ResponseMessage {
	return ResponseMessage{ResponseType: "ephemeral", ReplaceOriginal: false, Text: text, Blocks: nil}
}

// Replace is a reply that replaces the message the click came from.
func Replace(message Message) ResponseMessage {
	return ResponseMessage{ResponseType: "", ReplaceOriginal: true, Text: message.Text, Blocks: message.Blocks}
}

// HTTPResponder posts replies to Slack's response_url.
type HTTPResponder struct {
	client *guardian.HTTPClient
}

// NewHTTPResponder builds a Responder that posts with client.
func NewHTTPResponder(client *guardian.HTTPClient) *HTTPResponder {
	return &HTTPResponder{client: client}
}

// Respond posts message to responseURL, which must be a Slack response URL:
// the payload is signed, but the URL is still never trusted to point
// anywhere else.
func (r *HTTPResponder) Respond(ctx context.Context, responseURL string, message ResponseMessage) error {
	target, err := url.Parse(responseURL)
	if err != nil || target.Scheme != "https" || target.Host != slackResponseHost {
		return errors.New("response_url is not a Slack response URL")
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Slack reply: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, responseTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Slack reply: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Slack reply: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("send Slack reply: status %d", resp.StatusCode)
	}
	return nil
}

// route sends clicks whose action ID starts with prefix to handler.
type route struct {
	prefix  string
	handler http.Handler
}

// Router serves the interactivity endpoint. It sends a click on one element
// to the handler registered for the start of its action ID, with the body
// intact, so each handler verifies the signature and reads the payload
// itself. Anything no handler claims is checked and acknowledged by the
// router, since Slack shows any status but 200 as a failure.
type Router struct {
	logger        *slog.Logger
	signingSecret string
	routes        []route
}

// NewRouter builds a Router that checks unclaimed interactions against the
// Slack app's signing secret.
func NewRouter(logger *slog.Logger, signingSecret string) *Router {
	return &Router{logger: logger.With(attr.SlogComponent("slack_interactions")), signingSecret: signingSecret, routes: nil}
}

// Handle sends clicks whose action ID starts with prefix to handler. The
// first matching prefix wins, in the order they were added.
func (r *Router) Handle(prefix string, handler http.Handler) *Router {
	r.routes = append(r.routes, route{prefix: prefix, handler: handler})
	return r
}

// Unclaimed is the handler for interactions no route claims. A handler that
// passes on an interaction it routed to itself hands it here.
func (r *Router) Unclaimed() http.Handler {
	return http.HandlerFunc(r.serveUnclaimed)
}

// actionPayload is the part of an interaction payload routing reads.
type actionPayload struct {
	Actions []struct {
		ActionID string `json:"action_id"`
	} `json:"actions"`
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, MaxInteractionBytes+1))
	if err != nil || len(body) > MaxInteractionBytes {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	if handler := r.match(body); handler != nil {
		handler.ServeHTTP(w, req)
		return
	}
	r.serveUnclaimed(w, req)
}

// match finds the handler for a click on one element. Anything else,
// including a payload it cannot read, is unclaimed.
func (r *Router) match(body []byte) http.Handler {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil
	}
	var payload actionPayload
	if err := json.Unmarshal([]byte(form.Get("payload")), &payload); err != nil || len(payload.Actions) != 1 {
		return nil
	}
	for _, candidate := range r.routes {
		if strings.HasPrefix(payload.Actions[0].ActionID, candidate.prefix) {
			return candidate.handler
		}
	}
	return nil
}

// serveUnclaimed verifies the signature and acknowledges a well-formed
// interaction without acting on it.
func (r *Router) serveUnclaimed(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, MaxInteractionBytes+1))
	if err != nil || len(body) > MaxInteractionBytes {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	if r.signingSecret == "" {
		http.Error(w, "Slack interactions are not configured", http.StatusNotFound)
		return
	}
	if err := VerifySignature(body, req.Header, r.signingSecret); err != nil {
		r.logger.WarnContext(req.Context(), "reject unsigned Slack interaction", attr.SlogError(err))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	var payload actionPayload
	if err := json.Unmarshal([]byte(form.Get("payload")), &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

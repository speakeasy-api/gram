package mcpevents

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// callbackTimeout bounds verification and each delivery attempt.
	callbackTimeout = 10 * time.Second
	// maxCallbackBytes bounds memory used by an untrusted receiver response.
	maxCallbackBytes = 4 << 10 // 4 KiB
	// minSecretBytes and maxSecretBytes are the draft's signing-key limits.
	minSecretBytes = 24
	// maxSecretBytes caps a valid Standard Webhooks signing key.
	maxSecretBytes = 64
	// maxURLBytes bounds persisted callback endpoints.
	maxURLBytes = 4096
	// challengeBytes provides 256 bits of unpredictable verification entropy.
	challengeBytes = 32
)

// CallbackError contains only a bounded category, never a callback URL or secret.
type CallbackError struct {
	// Reason categorizes the refused callback.
	Reason string
}

func (e *CallbackError) Error() string { return "event callback: " + e.Reason }

// Webhook verifies and sends signed callbacks through a DNS-rebinding-safe policy.
type Webhook struct {
	client *guardian.HTTPClient
}

// NewWebhook refuses redirects on both verification and application deliveries.
// The caller must supply the default public-network guardian policy in production.
func NewWebhook(policy *guardian.Policy) *Webhook {
	return &Webhook{client: policy.PooledClient(guardian.WithCheckRedirect(func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}))}
}

// ValidateDestination checks syntax before performing any network request.
func ValidateDestination(destination Destination, requireSecret bool) error {
	u, err := url.Parse(destination.URL)
	if err != nil || len(destination.URL) > maxURLBytes || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || destination.Mode != "webhook" {
		return errors.New("delivery must contain an absolute HTTPS webhook URL without credentials or fragment")
	}
	if requireSecret {
		_, err = decodeSecret(destination.Secret)
	}
	return err
}

func decodeSecret(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, "whsec_")
	if !ok || len(encoded) > base64.StdEncoding.EncodedLen(maxSecretBytes) {
		return nil, errors.New("delivery.secret must be a Standard Webhooks key")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(key) < minSecretBytes || len(key) > maxSecretBytes {
		return nil, errors.New("delivery.secret must encode 24 to 64 bytes")
	}
	return key, nil
}

// Verify requires a signed, fresh challenge to be echoed by the receiver.
func (w *Webhook) Verify(ctx context.Context, id string, destination Destination, now time.Time) error {
	if err := ValidateDestination(destination, true); err != nil {
		return err
	}
	challenge := base64.RawURLEncoding.EncodeToString(randomChallenge())
	payload := struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}{Type: "verification", Challenge: challenge}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode verification: %w", err)
	}
	status, response, err := w.send(ctx, id, "msg_verification_"+challenge, destination.URL, []string{destination.Secret}, now, body)
	if err != nil {
		return err
	}
	var echoed struct {
		Challenge string `json:"challenge"`
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices || json.Unmarshal(response, &echoed) != nil || subtle.ConstantTimeCompare([]byte(echoed.Challenge), []byte(challenge)) != 1 {
		return &CallbackError{Reason: "challenge_failed"}
	}
	return nil
}

func randomChallenge() []byte {
	value := make([]byte, challengeBytes)
	_, _ = rand.Read(value)
	return value
}

func (w *Webhook) send(ctx context.Context, subscriptionID, eventID, callbackURL string, secrets []string, now time.Time, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callbackTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, &CallbackError{Reason: "invalid_url"}
	}
	signatures := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		signer, err := standardwebhooks.NewWebhook(secret)
		if err != nil {
			return 0, nil, fmt.Errorf("sign event callback: %w", err)
		}
		signed, err := signer.Sign(eventID, now, body)
		if err != nil {
			return 0, nil, fmt.Errorf("sign event callback: %w", err)
		}
		signatures = append(signatures, signed)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(standardwebhooks.HeaderWebhookID, eventID)
	request.Header.Set(standardwebhooks.HeaderWebhookTimestamp, strconv.FormatInt(now.Unix(), 10))
	request.Header.Set(standardwebhooks.HeaderWebhookSignature, strings.Join(signatures, " "))
	request.Header.Set("X-MCP-Subscription-Id", subscriptionID)
	response, err := w.client.Do(request)
	if err != nil {
		reason := "unreachable"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		return 0, nil, &CallbackError{Reason: reason}
	}
	defer o11y.NoLogDefer(func() error { return response.Body.Close() })
	data, err := io.ReadAll(io.LimitReader(response.Body, maxCallbackBytes+1))
	if err != nil || len(data) > maxCallbackBytes {
		return response.StatusCode, nil, &CallbackError{Reason: "invalid_response"}
	}
	return response.StatusCode, data, nil
}

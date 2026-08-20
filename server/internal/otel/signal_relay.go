package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	forwardingrepo "github.com/speakeasy-api/gram/server/internal/otelforwarding/repo"
)

const (
	relayDestinationCacheTTL = 60 * time.Second
	maxRelayErrorBodyBytes   = 4 * 1024
)

type relayReason string

const (
	relayReasonInvalid            relayReason = "invalid"
	relayReasonNoDestination      relayReason = "no-destination"
	relayReasonConfigError        relayReason = "config-error"
	relayReasonNetworkError       relayReason = "network-error"
	relayReasonHTTP4xx            relayReason = "http-4xx"
	relayReasonHTTP5xx            relayReason = "http-5xx"
	relayReasonPermanentHTTPError relayReason = "permanent-http-error"
)

// signalRelay resolves and caches customer-defined OTLP destinations, then
// delivers one signal's protobuf export requests to its configured endpoint.
type signalRelay struct {
	readReplica      *pgxpool.Pool
	encryptionClient *encryption.Client
	policy           *guardian.Policy
	endpointPath     string
	signalName       string
	now              func() time.Time

	cacheMu          sync.RWMutex
	destinationCache map[string]cachedRelayDestination
	destinationLoads singleflight.Group
}

type cachedRelayDestination struct {
	destination *relayDestination
	expiresAt   time.Time
}

// relayDestination sends protobuf exports to one organization's configured
// OTLP endpoint using its forwarding headers and HTTP policy.
type relayDestination struct {
	organizationID string
	endpoint       string
	headers        http.Header
	httpClient     *guardian.HTTPClient
	signalName     string
}

type relayExportError struct {
	reason    relayReason
	retryable bool
	err       error
}

func (e *relayExportError) Error() string {
	return e.err.Error()
}

func (e *relayExportError) Unwrap() error {
	return e.err
}

func newSignalRelay(
	readReplica *pgxpool.Pool,
	encryptionClient *encryption.Client,
	policy *guardian.Policy,
	endpointPath string,
	signalName string,
) *signalRelay {
	return &signalRelay{
		readReplica:      readReplica,
		encryptionClient: encryptionClient,
		policy:           policy,
		endpointPath:     endpointPath,
		signalName:       signalName,
		now:              time.Now,
		cacheMu:          sync.RWMutex{},
		destinationCache: make(map[string]cachedRelayDestination),
		destinationLoads: singleflight.Group{},
	}
}

func (r *signalRelay) destinationForOrganization(ctx context.Context, organizationID string) (*relayDestination, error) {
	now := r.now()
	if cached, ok := r.cachedDestination(organizationID, now); ok {
		return cached, nil
	}

	value, err, _ := r.destinationLoads.Do(organizationID, func() (any, error) {
		now := r.now()
		if cached, ok := r.cachedDestination(organizationID, now); ok {
			return cachedRelayDestination{destination: cached, expiresAt: now.Add(relayDestinationCacheTTL)}, nil
		}

		destination, err := r.loadDestination(ctx, organizationID)
		if err != nil {
			return nil, err
		}

		cached := cachedRelayDestination{
			destination: destination,
			expiresAt:   now.Add(relayDestinationCacheTTL),
		}
		r.cacheMu.Lock()
		r.destinationCache[organizationID] = cached
		r.cacheMu.Unlock()
		return cached, nil
	})
	if err != nil {
		return nil, err
	}

	cached, ok := value.(cachedRelayDestination)
	if !ok {
		return nil, fmt.Errorf("unexpected cached relay destination type %T", value)
	}
	return cached.destination, nil
}

func (r *signalRelay) loadDestination(ctx context.Context, organizationID string) (*relayDestination, error) {
	config, err := forwardingrepo.New(r.readReplica).GetOrgOTELForwardingConfig(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get forwarding settings for organization: %w", err)
	}
	if config.EndpointUrl == "" || !config.Enabled {
		return nil, nil
	}

	headers := make(map[string]string)
	if config.HeadersEncrypted.Valid && config.HeadersEncrypted.String != "" {
		plaintext, err := r.encryptionClient.Decrypt(config.HeadersEncrypted.String)
		if err != nil {
			return nil, fmt.Errorf("decrypt forwarding headers: %w", err)
		}
		if err := json.Unmarshal([]byte(plaintext), &headers); err != nil {
			return nil, fmt.Errorf("decode forwarding headers: %w", err)
		}
	}

	return r.newDestination(organizationID, config.EndpointUrl, headers)
}

func (r *signalRelay) newDestination(
	organizationID string,
	baseURL string,
	headerValues map[string]string,
) (*relayDestination, error) {
	endpoint, err := url.JoinPath(baseURL, r.endpointPath)
	if err != nil {
		return nil, fmt.Errorf("build forwarding endpoint: %w", err)
	}

	headers := make(http.Header, len(headerValues))
	for key, headerValue := range headerValues {
		headers.Set(key, headerValue)
	}
	retryConfig := guardian.DefaultRetryConfig()
	defaultCheckRetry := retryConfig.CheckRetry
	retryConfig.WaitMin = 100 * time.Millisecond
	retryConfig.WaitMax = time.Second
	retryConfig.MaxAttempts = 1
	retryConfig.CheckRetry = func(ctx context.Context, response *http.Response, err error) (bool, error) {
		retry, retryErr := defaultCheckRetry(ctx, response, err)
		if retry || retryErr != nil {
			return retry, retryErr
		}
		return response != nil && response.StatusCode == http.StatusRequestTimeout, nil
	}
	retryConfig.ErrorHandler = func(response *http.Response, err error, _ int) (*http.Response, error) {
		return response, err
	}
	httpClient := r.policy.PooledClient(guardian.WithRetryConfig(retryConfig))
	httpClient.Timeout = 10 * time.Second

	return &relayDestination{
		organizationID: organizationID,
		endpoint:       endpoint,
		headers:        headers,
		httpClient:     httpClient,
		signalName:     r.signalName,
	}, nil
}

func (r *signalRelay) cachedDestination(organizationID string, now time.Time) (*relayDestination, bool) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	cached, ok := r.destinationCache[organizationID]
	if !ok || !now.Before(cached.expiresAt) {
		return nil, false
	}
	return cached.destination, true
}

func (d *relayDestination) export(ctx context.Context, message proto.Message) error {
	body, err := proto.Marshal(message)
	if err != nil {
		return &relayExportError{
			reason:    relayReasonInvalid,
			retryable: false,
			err:       fmt.Errorf("marshal OTLP %s export: %w", d.signalName, err),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return &relayExportError{
			reason:    relayReasonPermanentHTTPError,
			retryable: false,
			err:       fmt.Errorf("create OTLP %s export request: %w", d.signalName, err),
		}
	}
	for key, values := range d.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("Content-Type", "application/x-protobuf")

	response, err := d.httpClient.Do(req)
	if err != nil {
		return &relayExportError{
			reason:    relayReasonNetworkError,
			retryable: true,
			err:       fmt.Errorf("send OTLP %s export: %w", d.signalName, err),
		}
	}
	defer o11y.NoLogDefer(func() error { return response.Body.Close() })

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}

	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxRelayErrorBodyBytes))
	_, _ = io.Copy(io.Discard, response.Body)
	responseSnippet := guardian.PrintableBodySnippet(responseBody)
	reason, retryable := classifyRelayStatus(response.StatusCode)
	responseErr := fmt.Errorf("OTLP %s export returned %s", d.signalName, response.Status)
	if !retryable && responseSnippet != "" {
		responseErr = fmt.Errorf("OTLP %s export returned %s: %s", d.signalName, response.Status, responseSnippet)
	}
	return &relayExportError{
		reason:    reason,
		retryable: retryable,
		err:       responseErr,
	}
}

func classifyRelayStatus(statusCode int) (relayReason, bool) {
	switch {
	case statusCode >= http.StatusInternalServerError:
		return relayReasonHTTP5xx, true
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests:
		return relayReasonHTTP4xx, true
	default:
		return relayReasonPermanentHTTPError, false
	}
}

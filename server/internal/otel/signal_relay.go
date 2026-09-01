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
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/proto"

	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	relayDestinationCacheTTL        = 60 * time.Second
	relayDestinationCacheMaxEntries = 1024
	maxRelayErrorBodyBytes          = 4 * 1024
	relayDataSourceProductTelemetry = "product_telemetry"
)

var sensitiveDataAttributeKeys = map[string]struct{}{
	"gen_ai.input.messages":  {},
	"gen_ai.output.messages": {},
	"user_prompt":            {},
	"prompt":                 {},
}

func redactSensitiveAttributes(attributes []*commonv1.KeyValue) []*commonv1.KeyValue {
	return slices.DeleteFunc(attributes, func(attribute *commonv1.KeyValue) bool {
		if attribute == nil {
			return false
		}
		_, sensitive := sensitiveDataAttributeKeys[attribute.GetKey()]
		return sensitive
	})
}

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
	destinationCache map[relayRouteKey]cachedRelayDestination
	destinationLoads singleflight.Group
}

type relayRouteKey struct {
	organizationID string
	projectID      uuid.UUID
}

type cachedRelayDestination struct {
	destination *relayDestination
	expiresAt   time.Time
}

// relayDestination sends protobuf exports to one project's configured OTLP
// endpoint using its destination headers and HTTP policy.
type relayDestination struct {
	organizationID       string
	projectID            uuid.UUID
	endpoint             string
	headers              http.Header
	httpClient           *guardian.HTTPClient
	signalName           string
	includeSensitiveData bool
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
		destinationCache: make(map[relayRouteKey]cachedRelayDestination),
		destinationLoads: singleflight.Group{},
	}
}

func (r *signalRelay) destinationForRoute(ctx context.Context, key relayRouteKey) (*relayDestination, error) {
	now := r.now()
	if cached, ok := r.cachedDestination(key, now); ok {
		return cached, nil
	}

	value, err, _ := r.destinationLoads.Do(relayRouteLoadKey(key), func() (any, error) {
		now := r.now()
		if cached, ok := r.cachedDestination(key, now); ok {
			return cachedRelayDestination{destination: cached, expiresAt: now.Add(relayDestinationCacheTTL)}, nil
		}

		destination, err := r.loadDestination(ctx, key)
		if err != nil {
			return nil, err
		}

		cached := cachedRelayDestination{
			destination: destination,
			expiresAt:   now.Add(relayDestinationCacheTTL),
		}
		r.cacheDestination(key, cached, now)
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

func relayRouteLoadKey(key relayRouteKey) string {
	return key.organizationID + "\x00" + key.projectID.String()
}

func (r *signalRelay) loadDestination(ctx context.Context, key relayRouteKey) (*relayDestination, error) {
	config, err := dataexportsrepo.New(r.readReplica).GetActiveOtelRouteDestination(ctx, dataexportsrepo.GetActiveOtelRouteDestinationParams{
		OrganizationID: key.organizationID,
		ProjectID:      key.projectID,
		DataSource:     relayDataSourceProductTelemetry,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active OTEL route destination: %w", err)
	}

	headers := make(map[string]string)
	if config.HeadersEncrypted.Valid && config.HeadersEncrypted.String != "" {
		plaintext, err := r.encryptionClient.Decrypt(config.HeadersEncrypted.String)
		if err != nil {
			return nil, fmt.Errorf("decrypt destination headers: %w", err)
		}
		if err := json.Unmarshal([]byte(plaintext), &headers); err != nil {
			return nil, fmt.Errorf("decode destination headers: %w", err)
		}
	}

	return r.newDestination(key, config.EndpointUrl, headers, config.IncludeSensitiveData)
}

func (r *signalRelay) newDestination(
	key relayRouteKey,
	baseURL string,
	headerValues map[string]string,
	includeSensitiveData bool,
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
		organizationID:       key.organizationID,
		projectID:            key.projectID,
		endpoint:             endpoint,
		headers:              headers,
		httpClient:           httpClient,
		signalName:           r.signalName,
		includeSensitiveData: includeSensitiveData,
	}, nil
}

func (r *signalRelay) cacheDestination(key relayRouteKey, cached cachedRelayDestination, now time.Time) {
	var evicted []*relayDestination

	r.cacheMu.Lock()
	if replaced, ok := r.destinationCache[key]; ok {
		delete(r.destinationCache, key)
		if replaced.destination != nil && replaced.destination != cached.destination {
			evicted = append(evicted, replaced.destination)
		}
	}
	for cachedKey, candidate := range r.destinationCache {
		if now.Before(candidate.expiresAt) {
			continue
		}
		delete(r.destinationCache, cachedKey)
		if candidate.destination != nil {
			evicted = append(evicted, candidate.destination)
		}
	}
	for len(r.destinationCache) >= relayDestinationCacheMaxEntries {
		var evictKey relayRouteKey
		var evictCandidate cachedRelayDestination
		evictSet := false
		for cachedKey, candidate := range r.destinationCache {
			if !evictSet ||
				candidate.expiresAt.Before(evictCandidate.expiresAt) ||
				(candidate.expiresAt.Equal(evictCandidate.expiresAt) && relayRouteKeyLess(cachedKey, evictKey)) {
				evictKey = cachedKey
				evictCandidate = candidate
				evictSet = true
			}
		}
		delete(r.destinationCache, evictKey)
		if evictCandidate.destination != nil {
			evicted = append(evicted, evictCandidate.destination)
		}
	}
	r.destinationCache[key] = cached
	r.cacheMu.Unlock()

	for _, destination := range evicted {
		closeIdleRelayDestination(destination)
	}
}

func relayRouteKeyLess(left, right relayRouteKey) bool {
	if left.organizationID != right.organizationID {
		return left.organizationID < right.organizationID
	}
	return bytes.Compare(left.projectID[:], right.projectID[:]) < 0
}

func closeIdleRelayDestination(destination *relayDestination) {
	if destination == nil || destination.httpClient == nil {
		return
	}
	destination.httpClient.CloseIdleConnections()
}

func (r *signalRelay) cachedDestination(key relayRouteKey, now time.Time) (*relayDestination, bool) {
	r.cacheMu.RLock()
	cached, ok := r.destinationCache[key]
	if !ok {
		r.cacheMu.RUnlock()
		return nil, false
	}
	if now.Before(cached.expiresAt) {
		r.cacheMu.RUnlock()
		return cached.destination, true
	}
	r.cacheMu.RUnlock()

	// Recheck under the write lock: another goroutine may have refreshed this
	// project route after the expired read above.
	r.cacheMu.Lock()
	cached, ok = r.destinationCache[key]
	if !ok {
		r.cacheMu.Unlock()
		return nil, false
	}
	if now.Before(cached.expiresAt) {
		r.cacheMu.Unlock()
		return cached.destination, true
	}
	delete(r.destinationCache, key)
	r.cacheMu.Unlock()

	closeIdleRelayDestination(cached.destination)
	return nil, false
}

func (d *relayDestination) export(ctx context.Context, message proto.Message) error {
	return d.exportWithLimit(ctx, message, 0)
}

func (d *relayDestination) exportWithLimit(ctx context.Context, message proto.Message, maxBytes int) error {
	body, err := proto.Marshal(message)
	if err != nil {
		return &relayExportError{
			reason:    relayReasonInvalid,
			retryable: false,
			err:       fmt.Errorf("marshal OTLP %s export: %w", d.signalName, err),
		}
	}
	if maxBytes > 0 && len(body) > maxBytes {
		return &relayExportError{
			reason:    relayReasonInvalid,
			retryable: false,
			err:       fmt.Errorf("OTLP %s export is %d bytes, limit is %d", d.signalName, len(body), maxBytes),
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

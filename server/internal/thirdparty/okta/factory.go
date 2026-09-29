package okta

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// ClientFactory hands out one memoized Client per remote session client,
// rebuilt whenever its Config changes. Callers must not cache instances
// themselves; call Client on every use so Forget and Config changes take effect.
type ClientFactory interface {
	Client(cfg Config) (Client, error)
	Forget(id uuid.UUID)
}

type clientFactory struct {
	logger     *slog.Logger
	httpClient *guardian.HTTPClient
	signer     remotesessions.TokenEndpointAssertionSigner
	decrypter  SecretDecrypter

	mu      sync.Mutex
	clients map[uuid.UUID]memoizedClient
}

type memoizedClient struct {
	cfg    Config
	client Client
}

var _ ClientFactory = (*clientFactory)(nil)

// NewClientFactory returns a factory backed by the real Okta Management API.
// The pooled client is built without retries so the per-client no-redirect
// policy NewClient installs is honored by the exchange itself.
func NewClientFactory(logger *slog.Logger, guardianPolicy *guardian.Policy, signer remotesessions.TokenEndpointAssertionSigner, decrypter SecretDecrypter) ClientFactory {
	httpClient := guardianPolicy.PooledClient(
		guardian.WithResilience("okta", guardian.ResilienceConfig{
			Partition:       partitionByHashedHost(),
			Limit:           guardian.PerMinute(defaultRequestsPerMinute),
			WaitForCapacity: true,
			Breaker: guardian.BreakerPolicy{
				FailureRateThreshold: 0.5,
				MinThroughput:        10,
				Window:               time.Minute,
				Delay:                30 * time.Second,
				SuccessThreshold:     2,
				IncludeSubset:        false,
			},
		}),
	)
	return &clientFactory{
		logger:     logger,
		httpClient: httpClient,
		signer:     signer,
		decrypter:  decrypter,
		mu:         sync.Mutex{},
		clients:    map[uuid.UUID]memoizedClient{},
	}
}

// partitionByHashedHost fingerprints the customer hostname because guardian
// exports partition segments as OTel attributes.
func partitionByHashedHost() guardian.PartitionStrategy {
	byHost := guardian.PartitionByHost()
	return func(req *http.Request) []string {
		segments := byHost(req)
		sum := sha256.Sum256([]byte(segments[0]))
		segments[0] = fmt.Sprintf("host-sha256-%x", sum)
		return segments
	}
}

func (f *clientFactory) Client(cfg Config) (Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.clients[cfg.RemoteSessionClientID]; ok && m.cfg == cfg {
		return m.client, nil
	}
	c, err := NewClient(f.logger, f.httpClient, f.signer, f.decrypter, cfg)
	if err != nil {
		delete(f.clients, cfg.RemoteSessionClientID)
		return nil, err
	}
	f.clients[cfg.RemoteSessionClientID] = memoizedClient{cfg: cfg, client: c}
	return c, nil
}

func (f *clientFactory) Forget(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.clients, id)
}

// FakeFactory hands out one Fake per Okta org URL.
type FakeFactory struct {
	mu    sync.Mutex
	fakes map[string]*Fake
}

var _ ClientFactory = (*FakeFactory)(nil)

// NewFakeFactory builds a Fake per OrgURL from fixtures keyed by OrgURL.
func NewFakeFactory(fixtures map[string]Fixtures) *FakeFactory {
	fakes := make(map[string]*Fake, len(fixtures))
	for orgURL, fx := range fixtures {
		fakes[orgURL] = NewFake(fx)
	}
	return &FakeFactory{mu: sync.Mutex{}, fakes: fakes}
}

func (f *FakeFactory) Client(cfg Config) (Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fake, ok := f.fakes[cfg.OrgURL]
	if !ok {
		return nil, fmt.Errorf("okta: no fake fixtures for org url %q", cfg.OrgURL)
	}
	fake.useConfig(cfg)
	return fake, nil
}

func (f *FakeFactory) Forget(uuid.UUID) {}

// Fake returns the Fake for orgURL, or nil when none was seeded.
func (f *FakeFactory) Fake(orgURL string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fakes[orgURL]
}

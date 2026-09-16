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

// ClientFactory hands out one memoized Client per remote session client.
// Callers must not cache instances themselves; call Client on every use so
// Forget takes effect after a credential change.
type ClientFactory interface {
	Client(cfg Config) (Client, error)
	Forget(id uuid.UUID)
}

type clientFactory struct {
	logger     *slog.Logger
	httpClient *guardian.HTTPClient
	signer     remotesessions.TokenEndpointAssertionSigner

	mu      sync.Mutex
	clients map[uuid.UUID]Client
}

var _ ClientFactory = (*clientFactory)(nil)

// NewClientFactory returns a factory backed by the real Okta Management API.
func NewClientFactory(logger *slog.Logger, guardianPolicy *guardian.Policy, signer remotesessions.TokenEndpointAssertionSigner) ClientFactory {
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
		mu:         sync.Mutex{},
		clients:    map[uuid.UUID]Client{},
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
	if c, ok := f.clients[cfg.RemoteSessionClientID]; ok {
		return c, nil
	}
	c, err := NewClient(f.logger, f.httpClient, f.signer, cfg)
	if err != nil {
		return nil, err
	}
	f.clients[cfg.RemoteSessionClientID] = c
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
	return fake, nil
}

func (f *FakeFactory) Forget(uuid.UUID) {}

// Fake returns the Fake for orgURL, or nil when none was seeded.
func (f *FakeFactory) Fake(orgURL string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fakes[orgURL]
}

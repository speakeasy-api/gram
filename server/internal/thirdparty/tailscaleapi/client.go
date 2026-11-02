// Package tailscaleapi wraps the Tailscale public API calls the private
// network reconciler needs: minting one-shot auth keys, defining Tailscale
// Services, and deleting devices. Every call authenticates with the customer's
// OAuth client, which never leaves the control plane.
package tailscaleapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tailscale "tailscale.com/client/tailscale/v2"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	// DefaultBaseURL is the public Tailscale API.
	DefaultBaseURL = "https://api.tailscale.com"

	// userAgent identifies Speakeasy in Tailscale's request logs.
	userAgent = "speakeasy-tailnet-gateway"

	// requestTimeout bounds one API call including the OAuth token exchange.
	requestTimeout = 30 * time.Second
)

var (
	// ErrUnauthorized means the OAuth client was rejected or lacks a scope.
	ErrUnauthorized = errors.New("tailscale api: credentials rejected")

	// ErrNotFound means the device or service does not exist.
	ErrNotFound = errors.New("tailscale api: not found")
)

// Credentials is a Tailscale OAuth client.
type Credentials struct {
	// ClientID identifies the OAuth client.
	ClientID string

	// ClientSecret authenticates the OAuth client.
	ClientSecret string
}

// AuthKeyRequest describes the one-shot node registration key to mint.
type AuthKeyRequest struct {
	// Tag is the ACL tag the registered node adopts.
	Tag string

	// Description is shown in the admin console's key list.
	Description string

	// TTL bounds how long the unused key stays valid.
	TTL time.Duration
}

// AuthKey is a minted registration key.
type AuthKey struct {
	// ID is the key's identifier in the admin console.
	ID string

	// Key is the secret handed to the node; it is never logged.
	Key string

	// Expires is when an unused key stops working.
	Expires time.Time
}

// Service is a Tailscale Service definition.
type Service struct {
	// Name is the service name including the svc: prefix.
	Name string

	// Ports lists advertised endpoints such as tcp:443.
	Ports []string

	// Tags are the ACL tags assigned to the service.
	Tags []string

	// Comment is shown in the admin console.
	Comment string
}

// Client is the subset of the Tailscale API used by the reconciler.
type Client interface {
	CreateAuthKey(ctx context.Context, creds Credentials, req AuthKeyRequest) (AuthKey, error)
	DeleteDevice(ctx context.Context, creds Credentials, nodeID string) error
	UpsertService(ctx context.Context, creds Credentials, svc Service) error
	DeleteService(ctx context.Context, creds Credentials, name string) error
}

// HTTPClient talks to the Tailscale API over HTTPS. One configured API client
// is kept per OAuth client so its access token is reused until it expires
// instead of being exchanged on every call.
type HTTPClient struct {
	baseURL *url.URL
	http    *guardian.HTTPClient

	mu      sync.Mutex
	clients map[credentialKey]*tailscale.Client
}

// credentialKey identifies an OAuth client without holding its secret in a
// map key; the secret is folded in as a digest.
type credentialKey struct {
	clientID string
	secret   [32]byte
}

// maxCachedClients bounds cached API clients. Each private network customer
// has one OAuth client, so this only matters if the reconciler churns through
// far more credentials than that.
const maxCachedClients = 1024

var _ Client = (*HTTPClient)(nil)

// New builds a client against baseURL (DefaultBaseURL when empty) using the
// guardian policy's outbound HTTP client.
func New(policy *guardian.Policy, baseURL string) (*HTTPClient, error) {
	if policy == nil {
		return nil, errors.New("tailscale api client requires a guardian policy")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid tailscale api base url %q", baseURL)
	}
	if err := RequireTLS(parsed); err != nil {
		return nil, err
	}
	httpClient := policy.PooledClient()
	httpClient.Timeout = requestTimeout
	return &HTTPClient{baseURL: parsed, http: httpClient, mu: sync.Mutex{}, clients: make(map[credentialKey]*tailscale.Client)}, nil
}

// RequireTLS rejects a base URL that would carry the customer's OAuth client
// in the clear. Plain HTTP is accepted only for loopback hosts, which is what
// in-process test servers use.
func RequireTLS(u *url.URL) error {
	if u == nil {
		return errors.New("tailscale api base url is required")
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	host := u.Hostname()
	if strings.EqualFold(u.Scheme, "http") && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return nil
	}
	return fmt.Errorf("tailscale api base url %q must use https", u.String())
}

func (c *HTTPClient) api(creds Credentials) (*tailscale.Client, error) {
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("%w: oauth client is incomplete", ErrUnauthorized)
	}
	key := credentialKey{clientID: creds.ClientID, secret: sha256.Sum256([]byte(creds.ClientSecret))}
	c.mu.Lock()
	defer c.mu.Unlock()
	if client, ok := c.clients[key]; ok {
		return client, nil
	}
	if len(c.clients) >= maxCachedClients {
		for stale := range c.clients {
			delete(c.clients, stale)
			break
		}
	}
	//nolint:exhaustruct // APIKey and Tailnet take their documented defaults.
	client := &tailscale.Client{
		BaseURL:   c.baseURL,
		UserAgent: userAgent,
		Auth:      &tailscale.OAuth{ClientID: creds.ClientID, ClientSecret: creds.ClientSecret, Scopes: nil},
		HTTP:      c.http,
	}
	c.clients[key] = client
	return client, nil
}

// CreateAuthKey mints a single-use, preauthorized, non-ephemeral key scoped to
// req.Tag, so the node it registers persists across restarts.
func (c *HTTPClient) CreateAuthKey(ctx context.Context, creds Credentials, req AuthKeyRequest) (AuthKey, error) {
	api, err := c.api(creds)
	if err != nil {
		return AuthKey{}, err
	}
	if req.Tag == "" || req.TTL < time.Second {
		return AuthKey{}, errors.New("auth key request requires a tag and a ttl of at least one second")
	}

	var capabilities tailscale.KeyCapabilities
	capabilities.Devices.Create.Reusable = false
	capabilities.Devices.Create.Ephemeral = false
	capabilities.Devices.Create.Preauthorized = true
	capabilities.Devices.Create.Tags = []string{req.Tag}
	key, err := api.Keys().CreateAuthKey(ctx, tailscale.CreateKeyRequest{
		Capabilities:  capabilities,
		ExpirySeconds: int64(req.TTL / time.Second),
		Description:   req.Description,
	})
	if err != nil {
		return AuthKey{}, mapError("create auth key", err)
	}
	if key == nil || key.Key == "" {
		return AuthKey{}, errors.New("tailscale api returned an empty auth key")
	}
	return AuthKey{ID: key.ID, Key: key.Key, Expires: key.Expires}, nil
}

// DeleteDevice removes a node from the tailnet. A missing device is success.
func (c *HTTPClient) DeleteDevice(ctx context.Context, creds Credentials, nodeID string) error {
	api, err := c.api(creds)
	if err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("device deletion requires a node id")
	}
	if err := api.Devices().Delete(ctx, nodeID); err != nil {
		err = mapError("delete device", err)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// UpsertService defines or updates a Tailscale Service.
func (c *HTTPClient) UpsertService(ctx context.Context, creds Credentials, svc Service) error {
	api, err := c.api(creds)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(svc.Name, "svc:") || len(svc.Ports) == 0 {
		return errors.New("service definition requires a svc: name and at least one port")
	}
	//nolint:exhaustruct // DisplayName, Addrs and Annotations are managed by Tailscale.
	err = api.Services().CreateOrUpdate(ctx, tailscale.Service{
		Name:    svc.Name,
		Ports:   svc.Ports,
		Tags:    svc.Tags,
		Comment: svc.Comment,
	})
	if err != nil {
		return mapError("upsert service", err)
	}
	return nil
}

// DeleteService removes a Tailscale Service definition. A missing service is
// success.
func (c *HTTPClient) DeleteService(ctx context.Context, creds Credentials, name string) error {
	api, err := c.api(creds)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(name, "svc:") {
		return errors.New("service deletion requires a svc: name")
	}
	if err := api.Services().Delete(ctx, name); err != nil {
		err = mapError("delete service", err)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func mapError(operation string, err error) error {
	if apiErr, ok := errors.AsType[tailscale.APIError](err); ok {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%s: %w", operation, ErrUnauthorized)
		case http.StatusNotFound:
			return fmt.Errorf("%s: %w", operation, ErrNotFound)
		}
	}
	// The OAuth token exchange surfaces client rejection as an oauth2 error
	// rather than an APIError.
	if strings.Contains(err.Error(), "oauth2") && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "invalid_client")) {
		return fmt.Errorf("%s: %w", operation, ErrUnauthorized)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

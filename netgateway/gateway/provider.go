// Package gateway implements the provider-neutral core of network-gateway:
// ingress supervision, Redis-leased ownership, durable node state, and the
// reverse proxy that carries requests from an org's overlay node into
// gram-server. Overlay-technology specifics live behind the Provider and Node
// interfaces; the tailscale implementation is the only one today and the only
// package allowed to import tailscale.com.
package gateway

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/google/uuid"
)

// ErrStateNotFound is returned by StateStore.Get when no value exists for a
// key. Providers translate it into their own not-found sentinel.
var ErrStateNotFound = errors.New("netingress: state not found")

// StateStore is the durable KV store holding one node's identity material
// (for tailscale: machine key, node key, profile). It is what lets a replica
// resume as the same overlay device after failover with no local disk.
type StateStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
}

// CredentialKind values mirror network_ingresses.credential_kind.
const (
	CredentialKindAuthKey     = "auth_key"
	CredentialKindOAuthClient = "oauth_client" //nolint:gosec // discriminator value, not a secret
)

// Credential is the decrypted join material for a customer network. Exactly
// one mode is populated, per the management API's validation.
type Credential struct {
	// Kind is auth_key or oauth_client.
	Kind string

	// AuthKey is a one-shot join key. The supervisor nulls the stored copy
	// after the first successful join; resume never needs it again.
	AuthKey string

	// OAuthClientID identifies an OAuth client able to mint fresh join keys.
	OAuthClientID string

	// OAuthClientSecret is the matching client secret.
	OAuthClientSecret string
}

// IngressConfig is one enabled network_ingresses row with its credential
// decrypted — everything a provider needs to run the org's node.
type IngressConfig struct {
	ID             uuid.UUID
	OrganizationID string

	// Provider selects the overlay technology, e.g. "tailscale".
	Provider string

	// Hostname is the device name the node advertises on the customer network.
	Hostname string

	// Tags are the ACL tags the node advertises (tailscale: tag:...).
	Tags []string

	// IdentityRequired rejects requests the node cannot attribute to a
	// network identity before they reach gram-server.
	IdentityRequired bool

	// UpdatedAt is the row's last mutation time; the supervisor restarts a
	// running node when it observes a newer value.
	UpdatedAt time.Time

	Credential Credential
}

// PeerIdentity is the provider-neutral identity of a caller on the customer
// network, carried to gram-server as X-Gram-Netingress-User-* headers.
type PeerIdentity struct {
	// Login is the network-attested user login, e.g. an email.
	Login string

	// DisplayName is the human name attached to the login, if any.
	DisplayName string

	// Device is the caller's device name on the network.
	Device string

	// Tags are ACL tags carried by the calling device, if any.
	Tags []string

	// Caps are provider capability grants attached to the caller. Advisory.
	Caps []string
}

// NodeStatus is a point-in-time view of a running node, persisted to the
// ingress row's health columns.
type NodeStatus struct {
	Online bool

	// NetworkName is the customer network joined, e.g. a tailnet name.
	NetworkName string

	// DNSName is where the node is reachable on the customer network.
	DNSName string

	// NodeID is the provider's stable identifier for the device.
	NodeID string

	// Err is the most recent provider error, empty when healthy.
	Err string
}

// Provider constructs nodes for one overlay technology.
type Provider interface {
	Name() string
	NewNode(ctx context.Context, cfg IngressConfig, state StateStore) (Node, error)
}

// Node is one running overlay endpoint serving an org's MCP surface.
type Node interface {
	// Start joins the customer network, resuming prior device identity from
	// the state store when present.
	Start(ctx context.Context) error

	// Listener accepts connections arriving over the customer network.
	Listener(ctx context.Context) (net.Listener, error)

	// Identity attributes a request's remote address to a network identity.
	// Returns (nil, nil) when the provider cannot attribute the caller.
	Identity(ctx context.Context, remoteAddr string) (*PeerIdentity, error)

	// Status reports current node health for persistence.
	Status(ctx context.Context) NodeStatus

	// Close leaves the network and releases local resources. Durable device
	// identity stays in the state store.
	Close(ctx context.Context) error
}

package tailnetgateway

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
)

// ErrNodeNeedsLogin means control refused the node's persisted identity and
// it is waiting for an interactive login it will never get.
var ErrNodeNeedsLogin = errors.New("tailnet node needs login")

// NodeSpec describes one tailnet node to start.
type NodeSpec struct {
	// IngressID is the ingress the node serves.
	IngressID uuid.UUID

	// OrganizationID owns the ingress.
	OrganizationID string

	// Slot is the node identity index; it equals the pod ordinal.
	Slot int32

	// Hostname is the node's tailnet hostname.
	Hostname string

	// ServiceName is the Tailscale Service to advertise, including svc:.
	ServiceName string

	// Tag is the ACL tag to advertise.
	Tag string

	// AuthKey registers the node on first start; it is empty once state exists.
	AuthKey string

	// Store persists the node's keys and profile.
	Store ipn.StateStore

	// StateDir is a scratch directory tsnet needs even with a custom Store.
	StateDir string

	// ControlURL overrides the coordination server; empty means Tailscale.
	ControlURL string

	// StartTimeout bounds how long the node may take to come online.
	StartTimeout time.Duration
}

// NodeState is a point-in-time view of a running node.
type NodeState struct {
	// Online is true once the node is connected, control has approved it to
	// host its service, and it holds the service's TLS certificate.
	Online bool

	// BackendState is tsnet's state machine name, for diagnostics.
	BackendState string

	// NeedsLogin is true when control no longer accepts the node's identity.
	NeedsLogin bool

	// AwaitingApproval is true when the node is connected but control has not
	// approved it to host its service, so the service has no addresses here.
	AwaitingApproval bool

	// AwaitingCertificate is true when the node is approved but has no TLS
	// certificate for its service yet, so every client handshake fails.
	AwaitingCertificate bool

	// CertificateStatus says why the certificate is missing and when the
	// node retries; empty otherwise.
	CertificateStatus string

	// NodeID is the stable Tailscale node id, used to delete the device.
	NodeID string

	// FQDN is the advertised service name, such as speakeasy.example.ts.net.
	FQDN string
}

// Node is a running tailnet node hosting one ingress.
type Node interface {
	// Serve starts handling service traffic with handler. It returns once the
	// listener is accepting; the handler runs until Close.
	Serve(handler http.Handler) error

	// Status reports the current node state.
	Status(ctx context.Context) (NodeState, error)

	// WhoIs resolves a tailnet peer address to its node and user.
	WhoIs(ctx context.Context, peer netip.Addr) (*apitype.WhoIsResponse, error)

	// Close disconnects from the tailnet and stops serving.
	Close(ctx context.Context) error
}

// NodeRuntime starts nodes. The production runtime embeds tsnet; tests
// substitute an in-memory implementation.
type NodeRuntime interface {
	Start(ctx context.Context, spec NodeSpec) (Node, error)
}

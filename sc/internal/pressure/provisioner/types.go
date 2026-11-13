// Package provisioner ports the tailnet gateway's provisioner (PR #8377,
// server/internal/tailnetgateway/provisioner.go and composite.go) onto the
// sc statechart package.
//
// The provisioner is not one long-lived instance. The reconciler calls
// Apply, Observe and Delete for an ingress whenever it reconciles it, and
// each call reads the slot rows the gateway pods heartbeat into, decides,
// acts, and returns an observation. The rows stay the source of truth: the
// gateway selects what to host by joining on them, and the reconciler reads
// their heartbeat columns. So the charts here run as one sc.Instance per
// pass, to completion inside the call, and persist nothing of their own:
//
//   - slotchart.go decides, for one slot row in one Apply, whether to purge,
//     reset, mint a key, or leave it alone, and carries that out.
//   - teardown.go is DeleteWithCredentials: list, retire, wait for the pods,
//     delete devices and the service, purge.
//
// Apply's own sequence (validate, ensure the organization key, define the
// service, ensure and retire slots, delete renamed services) and Observe's
// aggregation stay plain Go; see the report for why.
package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
)

// Ingress statuses and error codes, as k8s.NetworkIngress* defines them.
const (
	StatusPending  = "pending"
	StatusOnline   = "online"
	StatusDegraded = "degraded"
	StatusError    = "error"

	ProviderTailscale = "tailscale"

	ErrorInvalidDesiredState            = "invalid_desired_state"
	ErrorUnsupportedProvider            = "unsupported_provider"
	ErrorInvalidCredentials             = "invalid_credentials" // #nosec G101 -- bounded status code, not credential material.
	ErrorProviderCredentialsRejected    = "provider_credentials_rejected"
	ErrorProviderForbidden              = "provider_forbidden"
	ErrorProviderRateLimited            = "provider_rate_limited"
	ErrorProviderAPIUnavailable         = "provider_api_unavailable"
	ErrorProviderServiceNotApproved     = "provider_service_not_approved"
	ErrorProviderCertificateUnavailable = "provider_certificate_unavailable"
	ErrorEncryptionKeyUnavailable       = "encryption_key_unavailable"

	// RuntimeGateway is network_ingresses.runtime for gateway ingresses.
	RuntimeGateway = "gateway"
)

// Provider-neutral sentinels the reconciler classifies by.
var (
	ErrInvalidDesiredState         = errors.New("invalid network ingress desired state")
	ErrUnsupportedProvider         = errors.New("unsupported network ingress provider")
	ErrDeletionPending             = errors.New("network ingress deletion pending")
	ErrProviderUnavailable         = errors.New("network ingress provider unavailable")
	ErrProviderCredentialsRejected = errors.New("network ingress provider rejected credentials")
	// ErrProviderForbidden blocks teardown like rejected credentials.
	ErrProviderForbidden      = fmt.Errorf("%w: permission denied", ErrProviderCredentialsRejected)
	ErrProviderRateLimited    = errors.New("network ingress provider rate limited")
	ErrProviderAPIUnavailable = errors.New("network ingress provider api unavailable")
)

// Desired is the desired state of one ingress (k8s.NetworkIngressDesired).
type Desired struct {
	ID             uuid.UUID
	OrganizationID string
	Provider       string
	Hostname       string
	Credentials    []byte
}

// ResourceNames identifies an ingress to Observe and Delete.
type ResourceNames struct {
	OwnerID uuid.UUID
}

// Observation is what the reconciler records for an ingress.
type Observation struct {
	Status      string
	DNSName     string
	ErrorCode   string
	ConnectedAt *time.Time
}

// Tailscale API (thirdparty/tailscaleapi).

var (
	ErrTailscaleUnauthorized = errors.New("tailscale api: credentials rejected")
	ErrTailscaleForbidden    = fmt.Errorf("%w: permission denied", ErrTailscaleUnauthorized)
	ErrTailscaleRateLimited  = errors.New("tailscale api: rate limited")
	ErrTailscaleUnavailable  = errors.New("tailscale api: unavailable")
)

// Credentials is a customer's Tailscale OAuth client.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // #nosec G117 -- the field holds the secret by design.
}

// AuthKeyRequest asks for a one-shot, preauthorized, tagged key.
type AuthKeyRequest struct {
	Tag         string
	Description string
	TTL         time.Duration
}

// AuthKey is a minted key.
type AuthKey struct {
	ID      string
	Key     string
	Expires time.Time
}

// Service is a Tailscale Service definition.
type Service struct {
	Name    string
	Ports   []string
	Tags    []string
	Comment string
}

// TailscaleAPI is the Tailscale API client.
type TailscaleAPI interface {
	CreateAuthKey(ctx context.Context, creds Credentials, req AuthKeyRequest) (AuthKey, error)
	DeleteDevice(ctx context.Context, creds Credentials, nodeID string) error
	UpsertService(ctx context.Context, creds Credentials, svc Service) error
	DeleteService(ctx context.Context, creds Credentials, name string) error
}

// ParseCredentials reads the stored OAuth client (k8s.ParseTailscaleCredentials).
func ParseCredentials(encoded []byte) (Credentials, error) {
	var creds Credentials
	if err := json.Unmarshal(encoded, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parse Tailscale credentials: %w", err)
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return Credentials{}, errors.New("parse Tailscale credentials: client id and secret are required")
	}
	return creds, nil
}

// Store.

// ErrIngressNotFound is returned by [Store.Owner] for an unknown ingress.
var ErrIngressNotFound = errors.New("network ingress not found")

// Owner is GetNetworkIngressGatewayOwner: tombstones included.
type Owner struct {
	OrganizationID string
	Hostname       string
	Runtime        string
}

// SlotRow is one network_ingress_nodes row as the reconciler reads it.
type SlotRow struct {
	Slot             int32
	Status           string
	NodeID           string
	DNSName          string
	HasAuthKey       bool
	AuthKeyExpiresAt time.Time
	// LastSeenAt is zero when the slot was never heartbeat.
	LastSeenAt     time.Time
	ConnectedSince time.Time
	Retired        bool
}

// Store is the provisioner's view of Postgres.
type Store interface {
	Owner(ctx context.Context, id uuid.UUID) (Owner, error)
	EnsureSlot(ctx context.Context, id uuid.UUID, org string, slot int32) error
	// RetireSlots marks every slot at or above minSlot retired.
	RetireSlots(ctx context.Context, id uuid.UUID, org string, minSlot int32) error
	ListSlots(ctx context.Context, id uuid.UUID, org string) ([]SlotRow, error)
	// SetAuthKey installs a key for a slot that is not retired and has not
	// registered, and returns it to pending.
	SetAuthKey(ctx context.Context, id uuid.UUID, org string, slot int32, ciphertext string, expires time.Time) (int64, error)
	// DeleteSlot hard-deletes one retired slot.
	DeleteSlot(ctx context.Context, id uuid.UUID, org string, slot int32) error
	DeleteSlotStates(ctx context.Context, id uuid.UUID, org string, slot int32) error
	DeleteStates(ctx context.Context, id uuid.UUID, org string) error
	DeleteCertificateStates(ctx context.Context, id uuid.UUID, org string) error
	DeleteSlots(ctx context.Context, id uuid.UUID, org string) error
	NodeStateStore(id uuid.UUID, org string, slot int32) (host.NodeStateStore, error)
}

// Keys is organization encryption: the data key is minted on first use and
// auth keys are sealed under it.
type Keys interface {
	EnsureUseCase(ctx context.Context, organizationID string) error
	EncryptAuthKey(ctx context.Context, organizationID, authKey string) (string, error)
}

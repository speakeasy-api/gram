package tailnetgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

const (
	// RuntimeGateway marks an ingress hosted by the tailnet gateway.
	RuntimeGateway = "gateway"

	// DefaultSlots is how many node identities host each ingress. It matches
	// the gateway StatefulSet's maximum replica count.
	DefaultSlots = 3

	// DefaultAuthKeyTTL bounds how long an unused one-shot key stays valid.
	// A week covers a gateway outage without leaving live keys around.
	DefaultAuthKeyTTL = 7 * 24 * time.Hour

	// DefaultStaleAfter is how old a heartbeat may be before its host counts
	// as offline. Pods heartbeat every poll interval (15s); eight missed
	// beats means the pod is gone, not slow.
	DefaultStaleAfter = 2 * time.Minute

	// authKeyRenewMargin re-mints a key that would expire before the next
	// hourly sweep could notice.
	authKeyRenewMargin = 2 * time.Hour

	// servicePortSpec is the only endpoint every ingress service advertises.
	servicePortSpec = "tcp:443"

	// serviceComment is shown next to the service in the admin console.
	serviceComment = "Speakeasy private MCP ingress"
)

// ProvisionerConfig tunes the gateway provider.
type ProvisionerConfig struct {
	// Tag is the ACL tag minted keys carry and the service is tagged with.
	Tag string

	// Slots is how many node identities host each ingress.
	Slots int32

	// AuthKeyTTL bounds unused one-shot keys.
	AuthKeyTTL time.Duration

	// StaleAfter is the heartbeat age after which a host counts as offline.
	StaleAfter time.Duration

	// Now supplies the clock; nil means time.Now.
	Now func() time.Time
}

// Provisioner implements the network ingress provider for gateway-hosted
// ingresses. Apply defines the customer's Tailscale Service and mints keys
// for empty slots; Observe reads gateway heartbeats; teardown deletes the
// devices and service through the Tailscale API.
type Provisioner struct {
	db  *pgxpool.Pool
	enc *encryption.Client
	api tailscaleapi.Client
	cfg ProvisionerConfig
}

var (
	_ k8s.NetworkIngressProvisioner         = (*Provisioner)(nil)
	_ k8s.NetworkIngressCredentialedDeleter = (*Provisioner)(nil)
)

// NewProvisioner validates the configuration and returns the provider.
func NewProvisioner(db *pgxpool.Pool, enc *encryption.Client, api tailscaleapi.Client, cfg ProvisionerConfig) (*Provisioner, error) {
	if db == nil || enc == nil || api == nil {
		return nil, errors.New("tailnet gateway provisioner requires a database, encryption client and Tailscale API client")
	}
	if err := ValidateTag(cfg.Tag); err != nil {
		return nil, err
	}
	if cfg.Slots <= 0 {
		return nil, errors.New("tailnet gateway provisioner requires at least one slot")
	}
	if cfg.AuthKeyTTL <= 0 {
		cfg.AuthKeyTTL = DefaultAuthKeyTTL
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = DefaultStaleAfter
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Provisioner{db: db, enc: enc, api: api, cfg: cfg}, nil
}

type owner struct {
	organizationID string
	hostname       string
	runtime        string
}

func (p *Provisioner) owner(ctx context.Context, id uuid.UUID) (owner, error) {
	row, err := repo.New(p.db).GetNetworkIngressGatewayOwner(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner{}, fmt.Errorf("%w: ingress does not exist", k8s.ErrNetworkIngressInvalidDesiredState)
	}
	if err != nil {
		return owner{}, fmt.Errorf("load ingress owner: %w", err)
	}
	return owner{organizationID: row.OrganizationID, hostname: row.Hostname, runtime: row.Runtime.String}, nil
}

func credentialsFrom(encoded []byte) (tailscaleapi.Credentials, error) {
	parsed, err := k8s.ParseTailscaleCredentials(encoded)
	if err != nil {
		return tailscaleapi.Credentials{}, fmt.Errorf("parse Tailscale credentials: %w", err)
	}
	return tailscaleapi.Credentials{ClientID: parsed.ClientID, ClientSecret: parsed.ClientSecret}, nil
}

//nolint:exhaustruct // observations populate only the state they know.
func errorObservation(code string) k8s.NetworkIngressObservation {
	return k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusError, ErrorCode: code}
}

// Apply defines the service, ensures slot rows and mints keys for slots that
// have neither persisted state nor an unexpired key, then observes.
func (p *Provisioner) Apply(ctx context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	if desired.ID == uuid.Nil || desired.OrganizationID == "" || desired.Provider != k8s.NetworkIngressProviderTailscale || len(desired.Credentials) == 0 {
		return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), fmt.Errorf("%w: desired state is incomplete", k8s.ErrNetworkIngressInvalidDesiredState)
	}
	if err := ValidateLabel(desired.Hostname); err != nil {
		return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), fmt.Errorf("%w: %w", k8s.ErrNetworkIngressInvalidDesiredState, err)
	}
	creds, err := credentialsFrom(desired.Credentials)
	if err != nil {
		return errorObservation(k8s.NetworkIngressErrorInvalidCredentials), err
	}

	err = p.api.UpsertService(ctx, creds, tailscaleapi.Service{
		Name: ServiceName(desired.Hostname), Ports: []string{servicePortSpec}, Tags: []string{p.cfg.Tag}, Comment: serviceComment,
	})
	if errors.Is(err, tailscaleapi.ErrUnauthorized) {
		return errorObservation(k8s.NetworkIngressErrorInvalidCredentials), err
	}
	if err != nil {
		return errorObservation(""), err
	}

	queries := repo.New(p.db)
	for slot := range p.cfg.Slots {
		if err := queries.EnsureNetworkIngressNodeSlot(ctx, repo.EnsureNetworkIngressNodeSlotParams{
			NetworkIngressID: desired.ID, OrganizationID: desired.OrganizationID, Slot: slot,
		}); err != nil {
			return errorObservation(""), fmt.Errorf("ensure gateway slot: %w", err)
		}
	}
	if err := queries.RetireNetworkIngressNodes(ctx, repo.RetireNetworkIngressNodesParams{
		NetworkIngressID: desired.ID, OrganizationID: desired.OrganizationID, MinSlot: p.cfg.Slots,
	}); err != nil {
		return errorObservation(""), fmt.Errorf("retire surplus gateway slots: %w", err)
	}

	nodes, err := queries.ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: desired.ID, OrganizationID: desired.OrganizationID})
	if err != nil {
		return errorObservation(""), fmt.Errorf("list gateway slots: %w", err)
	}
	now := p.cfg.Now()
	for _, node := range nodes {
		if node.RetiredAt.Valid || node.Slot >= p.cfg.Slots {
			continue
		}
		hasState, err := queries.HasNetworkIngressNodeState(ctx, repo.HasNetworkIngressNodeStateParams{
			NetworkIngressID: desired.ID, OrganizationID: desired.OrganizationID, Slot: node.Slot,
		})
		if err != nil {
			return errorObservation(""), fmt.Errorf("check gateway slot state: %w", err)
		}
		if hasState {
			continue
		}
		if node.AuthKeyEncrypted.Valid && node.AuthKeyExpiresAt.Valid && node.AuthKeyExpiresAt.Time.After(now.Add(authKeyRenewMargin)) {
			continue
		}
		key, err := p.api.CreateAuthKey(ctx, creds, tailscaleapi.AuthKeyRequest{
			Tag: p.cfg.Tag, Description: fmt.Sprintf("Speakeasy gateway slot %d (%s)", node.Slot, desired.ID), TTL: p.cfg.AuthKeyTTL,
		})
		if errors.Is(err, tailscaleapi.ErrUnauthorized) {
			return errorObservation(k8s.NetworkIngressErrorInvalidCredentials), err
		}
		if err != nil {
			return errorObservation(""), err
		}
		ciphertext, err := p.enc.Encrypt([]byte(key.Key))
		if err != nil {
			return errorObservation(""), fmt.Errorf("encrypt gateway auth key: %w", err)
		}
		expires := key.Expires
		if expires.IsZero() {
			expires = now.Add(p.cfg.AuthKeyTTL)
		}
		if _, err := queries.SetNetworkIngressNodeAuthKey(ctx, repo.SetNetworkIngressNodeAuthKeyParams{
			AuthKeyEncrypted: conv.ToPGText(ciphertext), AuthKeyExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true, InfinityModifier: pgtype.Finite},
			NetworkIngressID: desired.ID, OrganizationID: desired.OrganizationID, Slot: node.Slot,
		}); err != nil {
			return errorObservation(""), fmt.Errorf("store gateway auth key: %w", err)
		}
	}

	if _, err := queries.MarkNetworkIngressRuntime(ctx, repo.MarkNetworkIngressRuntimeParams{
		Runtime: conv.ToPGText(RuntimeGateway), ID: desired.ID, OrganizationID: desired.OrganizationID,
	}); err != nil {
		return errorObservation(""), fmt.Errorf("mark ingress runtime: %w", err)
	}
	return p.observe(ctx, desired.ID, desired.OrganizationID)
}

// Observe reports the ingress online when any slot has a fresh online
// heartbeat, degraded when every heartbeat is stale, and pending otherwise.
func (p *Provisioner) Observe(ctx context.Context, resources k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), err
	}
	return p.observe(ctx, resources.OwnerID, own.organizationID)
}

//nolint:exhaustruct // observations populate only the state they know.
func (p *Provisioner) observe(ctx context.Context, id uuid.UUID, organizationID string) (k8s.NetworkIngressObservation, error) {
	nodes, err := repo.New(p.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: id, OrganizationID: organizationID})
	if err != nil {
		return errorObservation(""), fmt.Errorf("list gateway slots: %w", err)
	}
	now := p.cfg.Now()
	observation := k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusPending}
	seen, failed := false, false
	for _, node := range nodes {
		if node.RetiredAt.Valid || !node.LastSeenAt.Valid {
			continue
		}
		fresh := node.LastSeenAt.Time.After(now.Add(-p.cfg.StaleAfter))
		seen = seen || fresh
		switch {
		case fresh && node.Status == string(NodeStatusOnline) && node.DnsName.Valid:
			observation.Status = k8s.NetworkIngressStatusOnline
			observation.DNSName = node.DnsName.String
			if node.ConnectedSince.Valid && (observation.ConnectedAt == nil || node.ConnectedSince.Time.Before(*observation.ConnectedAt)) {
				connected := node.ConnectedSince.Time
				observation.ConnectedAt = &connected
			}
		case fresh && node.Status == string(NodeStatusError):
			failed = true
		}
	}
	if observation.Status == k8s.NetworkIngressStatusOnline {
		return observation, nil
	}
	if failed {
		return errorObservation(""), nil
	}
	if !seen && len(nodes) > 0 && anyHeartbeat(nodes) {
		observation.Status = k8s.NetworkIngressStatusDegraded
	}
	return observation, nil
}

func anyHeartbeat(nodes []repo.NetworkIngressNode) bool {
	for _, node := range nodes {
		if node.LastSeenAt.Valid && !node.RetiredAt.Valid {
			return true
		}
	}
	return false
}

// Delete without credentials can only succeed when no node identities exist;
// otherwise devices would be left on the customer's tailnet.
func (p *Provisioner) Delete(ctx context.Context, resources k8s.NetworkIngressResourceNames) error {
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		return err
	}
	nodes, err := repo.New(p.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID})
	if err != nil {
		return fmt.Errorf("list gateway slots: %w", err)
	}
	if len(nodes) == 0 {
		return nil
	}
	return fmt.Errorf("%w: credentials are required to delete tailnet devices", k8s.ErrNetworkIngressProviderCredentialsRejected)
}

// DeleteWithCredentials retires every slot, waits for gateway pods to stop
// hosting, then deletes the devices and the service through the API and
// purges the persisted node identities.
func (p *Provisioner) DeleteWithCredentials(ctx context.Context, resources k8s.NetworkIngressResourceNames, credentials []byte) error {
	creds, err := credentialsFrom(credentials)
	if err != nil {
		return fmt.Errorf("%w: %w", k8s.ErrNetworkIngressProviderCredentialsRejected, err)
	}
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		return err
	}
	queries := repo.New(p.db)
	if err := queries.RetireNetworkIngressNodes(ctx, repo.RetireNetworkIngressNodesParams{
		NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID, MinSlot: 0,
	}); err != nil {
		return fmt.Errorf("retire gateway slots: %w", err)
	}
	nodes, err := queries.ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID})
	if err != nil {
		return fmt.Errorf("list gateway slots: %w", err)
	}
	now := p.cfg.Now()
	for _, node := range nodes {
		if !node.LastSeenAt.Valid || !node.LastSeenAt.Time.After(now.Add(-p.cfg.StaleAfter)) {
			continue
		}
		switch node.Status {
		case string(NodeStatusStopped), string(NodeStatusPending), string(NodeStatusError):
			continue
		default:
			return fmt.Errorf("%w: slot %d is still hosted", k8s.ErrNetworkIngressDeletionPending, node.Slot)
		}
	}
	for _, node := range nodes {
		if !node.NodeID.Valid || node.NodeID.String == "" {
			continue
		}
		if err := p.api.DeleteDevice(ctx, creds, node.NodeID.String); err != nil {
			return mapAPIError(err)
		}
	}
	if own.hostname != "" {
		if err := p.api.DeleteService(ctx, creds, ServiceName(own.hostname)); err != nil {
			return mapAPIError(err)
		}
	}
	if err := queries.DeleteNetworkIngressNodeStates(ctx, repo.DeleteNetworkIngressNodeStatesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID}); err != nil {
		return fmt.Errorf("delete gateway node state: %w", err)
	}
	if err := queries.DeleteNetworkIngressNodes(ctx, repo.DeleteNetworkIngressNodesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID}); err != nil {
		return fmt.Errorf("delete gateway slots: %w", err)
	}
	return nil
}

func mapAPIError(err error) error {
	if errors.Is(err, tailscaleapi.ErrUnauthorized) {
		return fmt.Errorf("%w: %w", k8s.ErrNetworkIngressProviderCredentialsRejected, err)
	}
	return err
}

// FindOrphans is a no-op: the gateway owns no Kubernetes resources.
func (p *Provisioner) FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
	return nil, nil
}

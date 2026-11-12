package tailnetgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

const (
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
)

// ProvisionerConfig tunes the gateway provider.
type ProvisionerConfig struct {
	// Tag is the ACL tag minted keys carry and the service is tagged with.
	Tag string

	// Slots is how many node identities host each ingress.
	Slots int32

	// Namer decides which services this stack may define and delete, so a
	// local development stack never touches production's on a shared tailnet.
	Namer networkingress.Namer

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
	logger *slog.Logger
	db     *pgxpool.Pool
	enc    *orgencryption.Service
	keys   KeyProvisioner
	api    tailscaleapi.Client
	cfg    ProvisionerConfig
}

var (
	_ k8s.NetworkIngressProvisioner         = (*Provisioner)(nil)
	_ k8s.NetworkIngressCredentialedDeleter = (*Provisioner)(nil)
)

// NewProvisioner validates the configuration and returns the provider. Auth
// keys and node state are sealed under each organization's key: keys mints
// the organization's data key on first use and enc encrypts with it. Either
// may be nil when organization encryption is not configured, in which case
// Apply is unavailable while observation and teardown still work.
func NewProvisioner(logger *slog.Logger, db *pgxpool.Pool, enc *orgencryption.Service, keys KeyProvisioner, api tailscaleapi.Client, cfg ProvisionerConfig) (*Provisioner, error) {
	if logger == nil || db == nil || api == nil {
		return nil, errors.New("tailnet gateway provisioner requires a logger, a database and a Tailscale API client")
	}
	if err := ValidateTag(cfg.Tag); err != nil {
		return nil, err
	}
	if cfg.Slots <= 0 {
		return nil, errors.New("tailnet gateway provisioner requires at least one slot")
	}
	if cfg.Namer == nil {
		return nil, errors.New("tailnet gateway provisioner requires a namer")
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
	return &Provisioner{logger: logger.With(attr.SlogComponent("tailnet_gateway_provisioner")), db: db, enc: enc, keys: keys, api: api, cfg: cfg}, nil
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

// validateDesired checks everything Apply needs before any side effect. The
// returned error observation carries the code Apply reports.
func (p *Provisioner) validateDesired(ctx context.Context, desired k8s.NetworkIngressDesired) (tailscaleapi.Credentials, k8s.NetworkIngressObservation, error) {
	if desired.ID == uuid.Nil || desired.OrganizationID == "" || desired.Provider != k8s.NetworkIngressProviderTailscale || len(desired.Credentials) == 0 {
		return tailscaleapi.Credentials{}, errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), fmt.Errorf("%w: desired state is incomplete", k8s.ErrNetworkIngressInvalidDesiredState)
	}
	if err := ValidateLabel(desired.Hostname); err != nil {
		return tailscaleapi.Credentials{}, errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), fmt.Errorf("%w: %w", k8s.ErrNetworkIngressInvalidDesiredState, err)
	}
	owned, err := p.cfg.Namer.OwnsHostname(ctx, desired.Hostname)
	if err != nil {
		return tailscaleapi.Credentials{}, errorObservation(""), fmt.Errorf("check hostname ownership: %w", err)
	}
	if !owned {
		return tailscaleapi.Credentials{}, errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), fmt.Errorf("%w: this stack does not own hostname %q", k8s.ErrNetworkIngressInvalidDesiredState, desired.Hostname)
	}
	if p.enc == nil || p.keys == nil {
		return tailscaleapi.Credentials{}, errorObservation(""), errors.New("tailnet gateway provisioner needs organization encryption to store auth keys")
	}
	creds, err := credentialsFrom(desired.Credentials)
	if err != nil {
		return tailscaleapi.Credentials{}, errorObservation(k8s.NetworkIngressErrorInvalidCredentials), err
	}
	return creds, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, nil
}

// Apply defines the service, ensures slot rows and mints keys for slots that
// have not registered and hold no unexpired key, then observes.
//
// A failed Apply says nothing about hosts that are already serving, and the
// private listener routes by the recorded DNS name. So when Apply fails while
// a host is online, the error observation keeps that host's DNS name, and a
// Tailscale outage or rate limit does not cut live traffic.
func (p *Provisioner) Apply(ctx context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	observation, err := p.apply(ctx, desired)
	if err != nil {
		p.logFailure(ctx, "apply", desired.ID, desired.OrganizationID, observation.ErrorCode, err)
	}
	if err == nil || observation.DNSName != "" || desired.ID == uuid.Nil || desired.OrganizationID == "" {
		return observation, err
	}
	live, observeErr := p.observe(ctx, desired.ID, desired.OrganizationID)
	if observeErr == nil && live.Status == k8s.NetworkIngressStatusOnline {
		observation.DNSName = live.DNSName
		observation.ConnectedAt = live.ConnectedAt
	}
	return observation, err
}

func (p *Provisioner) apply(ctx context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	creds, observation, err := p.validateDesired(ctx, desired)
	if err != nil {
		return observation, err
	}
	// Auth keys and node state are sealed under the organization's key, so
	// make sure it has one before minting anything.
	if err := p.keys.EnsureUseCase(ctx, desired.OrganizationID, EncryptionUseCase, orgencryption.SystemActor()); err != nil {
		return encryptionFailure(fmt.Errorf("ensure gateway encryption key: %w", err))
	}
	// A key that exists can still be suspended or unreachable. Check it can
	// seal before minting: an auth key Tailscale issued that cannot be stored
	// is wasted, and every later pass would mint more.
	if _, err := p.encryptAuthKey(ctx, desired.OrganizationID, ""); err != nil {
		return encryptionFailure(err)
	}

	serviceName, err := p.cfg.Namer.ServiceName(ctx, desired.Hostname)
	if err != nil {
		return errorObservation(""), fmt.Errorf("name gateway service: %w", err)
	}
	serviceComment, err := p.cfg.Namer.ServiceComment(ctx)
	if err != nil {
		return errorObservation(""), fmt.Errorf("describe gateway service: %w", err)
	}
	err = p.api.UpsertService(ctx, creds, tailscaleapi.Service{
		Name: serviceName, Ports: []string{servicePortSpec}, Tags: []string{p.cfg.Tag}, Comment: serviceComment,
	})
	if err != nil {
		return apiFailure(err)
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
	// A label change leaves the previous service behind; the hosts' reported
	// FQDNs remember which one it was.
	if err := p.deleteRenamedServices(ctx, creds, desired.Hostname, nodes); err != nil {
		return apiFailure(err)
	}
	now := p.cfg.Now()
	for _, node := range nodes {
		if node.RetiredAt.Valid {
			// A retired slot whose host has stopped is purged: its device leaves
			// the tailnet and a later slot count increase starts a fresh identity.
			if err := p.purgeRetiredSlot(ctx, creds, desired.ID, desired.OrganizationID, node, now); err != nil {
				return apiFailure(err)
			}
			continue
		}
		if node.Slot >= p.cfg.Slots {
			continue
		}
		store, err := NewPostgresStateStore(p.db, p.enc, desired.ID, desired.OrganizationID, node.Slot)
		if err != nil {
			return errorObservation(""), fmt.Errorf("build gateway slot state store: %w", err)
		}
		registered, err := store.Registered(ctx)
		if err != nil {
			return encryptionFailure(fmt.Errorf("check gateway slot registration: %w", err))
		}
		if registered && node.Status == string(NodeStatusNeedsLogin) {
			if err := p.resetRefusedSlot(ctx, creds, store, node); err != nil {
				return apiFailure(err)
			}
			registered = false
		}
		if registered {
			continue
		}
		if node.AuthKeyEncrypted.Valid && node.AuthKeyExpiresAt.Valid && node.AuthKeyExpiresAt.Time.After(now.Add(authKeyRenewMargin)) {
			continue
		}
		description, err := p.cfg.Namer.AuthKeyDescription(ctx, desired.ID, node.Slot)
		if err != nil {
			return errorObservation(""), fmt.Errorf("describe gateway auth key: %w", err)
		}
		key, err := p.api.CreateAuthKey(ctx, creds, tailscaleapi.AuthKeyRequest{
			Tag: p.cfg.Tag, Description: description, TTL: p.cfg.AuthKeyTTL,
		})
		if err != nil {
			return apiFailure(err)
		}
		ciphertext, err := p.encryptAuthKey(ctx, desired.OrganizationID, key.Key)
		if err != nil {
			return encryptionFailure(err)
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

	return p.observe(ctx, desired.ID, desired.OrganizationID)
}

// resetRefusedSlot replaces an identity control no longer accepts, typically
// because the device was removed from the tailnet. The pod has already
// stopped hosting it. The old device is deleted if it still exists and the
// persisted state is cleared, so the caller mints a key for a fresh login.
func (p *Provisioner) resetRefusedSlot(ctx context.Context, creds tailscaleapi.Credentials, store *PostgresStateStore, node repo.NetworkIngressNode) error {
	if node.NodeID.Valid && node.NodeID.String != "" {
		if err := p.api.DeleteDevice(ctx, creds, node.NodeID.String); err != nil {
			return fmt.Errorf("delete refused slot device: %w", err)
		}
	}
	if err := store.Reset(ctx); err != nil {
		return fmt.Errorf("reset refused slot state: %w", err)
	}
	return nil
}

// purgeRetiredSlot deletes a retired slot's device and rows once no host is
// still reporting it live.
func (p *Provisioner) purgeRetiredSlot(ctx context.Context, creds tailscaleapi.Credentials, ingressID uuid.UUID, organizationID string, node repo.NetworkIngressNode, now time.Time) error {
	if node.LastSeenAt.Valid && node.LastSeenAt.Time.After(now.Add(-p.cfg.StaleAfter)) {
		switch node.Status {
		case string(NodeStatusStopped), string(NodeStatusPending), string(NodeStatusError), string(NodeStatusNeedsLogin):
		default:
			return nil
		}
	}
	if node.NodeID.Valid && node.NodeID.String != "" {
		if err := p.api.DeleteDevice(ctx, creds, node.NodeID.String); err != nil {
			return fmt.Errorf("delete retired slot device: %w", err)
		}
	}
	queries := repo.New(p.db)
	if err := queries.DeleteNetworkIngressNodeSlotStates(ctx, repo.DeleteNetworkIngressNodeSlotStatesParams{NetworkIngressID: ingressID, OrganizationID: organizationID, Slot: node.Slot}); err != nil {
		return fmt.Errorf("purge retired slot state: %w", err)
	}
	if err := queries.DeleteNetworkIngressNodeSlot(ctx, repo.DeleteNetworkIngressNodeSlotParams{NetworkIngressID: ingressID, OrganizationID: organizationID, Slot: node.Slot}); err != nil {
		return fmt.Errorf("purge retired slot: %w", err)
	}
	return nil
}

// deleteRenamedServices removes services advertised under a previous label.
func (p *Provisioner) deleteRenamedServices(ctx context.Context, creds tailscaleapi.Credentials, label string, nodes []repo.NetworkIngressNode) error {
	seen := map[string]struct{}{}
	for _, node := range nodes {
		if !node.DnsName.Valid {
			continue
		}
		previous, _, _ := strings.Cut(node.DnsName.String, ".")
		if previous == "" || previous == label {
			continue
		}
		owned, err := p.cfg.Namer.OwnsHostname(ctx, previous)
		if err != nil {
			return fmt.Errorf("check renamed service ownership: %w", err)
		}
		if !owned {
			continue
		}
		serviceName, err := p.cfg.Namer.ServiceName(ctx, previous)
		if err != nil {
			return fmt.Errorf("name renamed service: %w", err)
		}
		if _, done := seen[previous]; done {
			continue
		}
		seen[previous] = struct{}{}
		if err := p.api.DeleteService(ctx, creds, serviceName); err != nil {
			return fmt.Errorf("delete renamed service: %w", err)
		}
	}
	return nil
}

// Observe reports the ingress online when any slot has a fresh online
// heartbeat, degraded when every heartbeat is stale, and pending otherwise.
func (p *Provisioner) Observe(ctx context.Context, resources k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		p.logFailure(ctx, "observe", resources.OwnerID, "", operationErrorCode(err), err)
		return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), err
	}
	observation, err := p.observe(ctx, resources.OwnerID, own.organizationID)
	if err != nil {
		p.logFailure(ctx, "observe", resources.OwnerID, own.organizationID, observation.ErrorCode, err)
	}
	return observation, err
}

//nolint:exhaustruct // observations populate only the state they know.
func (p *Provisioner) observe(ctx context.Context, id uuid.UUID, organizationID string) (k8s.NetworkIngressObservation, error) {
	nodes, err := repo.New(p.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: id, OrganizationID: organizationID})
	if err != nil {
		return errorObservation(""), fmt.Errorf("list gateway slots: %w", err)
	}
	now := p.cfg.Now()
	observation := k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusPending}
	seen, failed, unapproved, uncertified := false, false, false, false
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
		case fresh && (node.Status == string(NodeStatusError) || node.Status == string(NodeStatusNeedsLogin)):
			failed = true
		case fresh && node.Status == string(NodeStatusAwaitingApproval):
			unapproved = true
		case fresh && node.Status == string(NodeStatusAwaitingCertificate):
			uncertified = true
		}
	}
	if observation.Status == k8s.NetworkIngressStatusOnline {
		return observation, nil
	}
	// Connected but unapproved hosts are something the customer can fix in
	// their tailnet, so that takes precedence over a generic failure.
	if unapproved {
		return errorObservation(k8s.NetworkIngressErrorProviderServiceNotApproved), nil
	}
	// Approved hosts without a TLS certificate fail every client handshake.
	// The gateway retries on its own; the code names the cause.
	if uncertified {
		return errorObservation(k8s.NetworkIngressErrorProviderCertificateUnavailable), nil
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

// Delete without credentials is DeleteWithCredentials with none: it only
// succeeds when the gateway never touched the tailnet for this ingress.
func (p *Provisioner) Delete(ctx context.Context, resources k8s.NetworkIngressResourceNames) error {
	return p.DeleteWithCredentials(ctx, resources, nil)
}

// DeleteWithCredentials retires every slot, waits for gateway pods to stop
// hosting, then deletes the devices and the service through the API and
// purges the persisted node identities. Once Apply has run, the service and
// any devices exist on the customer's tailnet, so missing or unparseable
// credentials block cleanup rather than silently leaving them behind.
func (p *Provisioner) DeleteWithCredentials(ctx context.Context, resources k8s.NetworkIngressResourceNames, credentials []byte) error {
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		p.logFailure(ctx, "delete", resources.OwnerID, "", operationErrorCode(err), err)
		return err
	}
	err = p.deleteWithCredentials(ctx, own, resources, credentials)
	// Waiting for hosts to stop is the expected first answer to a delete.
	if err != nil && !errors.Is(err, k8s.ErrNetworkIngressDeletionPending) {
		p.logFailure(ctx, "delete", resources.OwnerID, own.organizationID, operationErrorCode(err), err)
	}
	return err
}

func (p *Provisioner) deleteWithCredentials(ctx context.Context, own owner, resources k8s.NetworkIngressResourceNames, credentials []byte) error {
	existing, err := repo.New(p.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID})
	if err != nil {
		return fmt.Errorf("list gateway slots: %w", err)
	}
	// Apply creates the slot rows right after defining the service, so an
	// ingress without them never got a device onto the tailnet. Calling
	// Tailscale anyway would let a wrong secret block deletion for good, and
	// could delete a svc: the gateway never created. The rare cost is a
	// service left behind when the slot insert failed after the upsert.
	if len(existing) == 0 {
		return nil
	}
	if len(credentials) == 0 {
		return fmt.Errorf("%w: credentials are required to delete the tailnet service and devices", k8s.ErrNetworkIngressProviderCredentialsRejected)
	}
	creds, err := credentialsFrom(credentials)
	if err != nil {
		return fmt.Errorf("%w: %w", k8s.ErrNetworkIngressProviderCredentialsRejected, err)
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
		case string(NodeStatusStopped), string(NodeStatusPending), string(NodeStatusError), string(NodeStatusNeedsLogin):
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
		owned, err := p.cfg.Namer.OwnsHostname(ctx, own.hostname)
		if err != nil {
			return fmt.Errorf("check service ownership: %w", err)
		}
		if owned {
			serviceName, err := p.cfg.Namer.ServiceName(ctx, own.hostname)
			if err != nil {
				return fmt.Errorf("name gateway service: %w", err)
			}
			if err := p.api.DeleteService(ctx, creds, serviceName); err != nil {
				return mapAPIError(err)
			}
		}
	}
	if err := queries.DeleteNetworkIngressNodeStates(ctx, repo.DeleteNetworkIngressNodeStatesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID}); err != nil {
		return fmt.Errorf("delete gateway node state: %w", err)
	}
	if err := queries.DeleteNetworkIngressCertificateStates(ctx, repo.DeleteNetworkIngressCertificateStatesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID}); err != nil {
		return fmt.Errorf("delete gateway certificate state: %w", err)
	}
	if err := queries.DeleteNetworkIngressNodes(ctx, repo.DeleteNetworkIngressNodesParams{NetworkIngressID: resources.OwnerID, OrganizationID: own.organizationID}); err != nil {
		return fmt.Errorf("delete gateway slots: %w", err)
	}
	return nil
}

// operationErrorCode is the bounded code for an error from an operation that
// reports no observation, such as teardown or the ingress owner lookup, or
// "" when the error is unclassified. A missing ingress is
// invalid_desired_state; a database error is not.
func operationErrorCode(err error) string {
	switch {
	case errors.Is(err, k8s.ErrNetworkIngressInvalidDesiredState):
		return k8s.NetworkIngressErrorInvalidDesiredState
	case errors.Is(err, k8s.ErrNetworkIngressProviderCredentialsRejected):
		return k8s.NetworkIngressErrorProviderCredentialsRejected
	case errors.Is(err, k8s.ErrNetworkIngressProviderForbidden):
		return k8s.NetworkIngressErrorProviderForbidden
	case errors.Is(err, k8s.ErrNetworkIngressProviderRateLimited):
		return k8s.NetworkIngressErrorProviderRateLimited
	case errors.Is(err, k8s.ErrNetworkIngressProviderAPIUnavailable):
		return k8s.NetworkIngressErrorProviderAPIUnavailable
	case encryptionKeyUnavailable(err):
		return k8s.NetworkIngressErrorEncryptionKeyUnavailable
	default:
		return ""
	}
}

// logFailure records why a provisioner operation failed. The reconciler's
// shared provider telemetry only reports a bounded error code, because the
// Kubernetes operator's errors can carry Kubernetes request data. This
// provider's errors come from the Tailscale API client and Postgres and never
// carry the customer's OAuth client secret or auth keys (a rejected token
// exchange reports Tailscale's response, such as invalid_client), so the full
// error is logged here: it is the only place the cause of a failed reconcile
// is visible. Failures the customer or Tailscale causes, which carry a
// specific error code, are warnings; anything unclassified is an error.
func (p *Provisioner) logFailure(ctx context.Context, operation string, ingressID uuid.UUID, organizationID, errorCode string, err error) {
	attrs := []any{
		attr.SlogNetworkIngressOperation(operation),
		attr.SlogNetworkIngressID(ingressID.String()),
		attr.SlogError(err),
	}
	if organizationID != "" {
		attrs = append(attrs, attr.SlogOrganizationID(organizationID))
	}
	if errorCode != "" {
		attrs = append(attrs, attr.SlogNetworkIngressErrorCode(errorCode))
		p.logger.WarnContext(ctx, "tailnet gateway provisioner operation failed", attrs...)
		return
	}
	p.logger.ErrorContext(ctx, "tailnet gateway provisioner operation failed", attrs...)
}

// mapAPIError tags a Tailscale API failure with the provider-neutral sentinel
// the reconciler and metrics classify by.
func mapAPIError(err error) error {
	var sentinel error
	switch {
	case errors.Is(err, tailscaleapi.ErrForbidden):
		sentinel = k8s.ErrNetworkIngressProviderForbidden
	case errors.Is(err, tailscaleapi.ErrUnauthorized):
		sentinel = k8s.ErrNetworkIngressProviderCredentialsRejected
	case errors.Is(err, tailscaleapi.ErrRateLimited):
		sentinel = k8s.ErrNetworkIngressProviderRateLimited
	case errors.Is(err, tailscaleapi.ErrUnavailable):
		sentinel = k8s.ErrNetworkIngressProviderAPIUnavailable
	default:
		return err
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}

// apiFailure is the observation and error Apply reports for a failed
// Tailscale API call. A rejected OAuth client keeps the invalid_credentials
// code that connection setup reports; the teardown blocker sentinel is only
// for Delete. Every other failure gets its own bounded code.
func apiFailure(err error) (k8s.NetworkIngressObservation, error) {
	switch {
	case errors.Is(err, tailscaleapi.ErrForbidden):
		return errorObservation(k8s.NetworkIngressErrorProviderForbidden), mapAPIError(err)
	case errors.Is(err, tailscaleapi.ErrUnauthorized):
		return errorObservation(k8s.NetworkIngressErrorInvalidCredentials), err
	case errors.Is(err, tailscaleapi.ErrRateLimited):
		return errorObservation(k8s.NetworkIngressErrorProviderRateLimited), mapAPIError(err)
	case errors.Is(err, tailscaleapi.ErrUnavailable):
		return errorObservation(k8s.NetworkIngressErrorProviderAPIUnavailable), mapAPIError(err)
	default:
		return errorObservation(""), err
	}
}

// FindOrphans is a no-op: the gateway owns no Kubernetes resources.
func (p *Provisioner) FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
	return nil, nil
}

// encryptAuthKey seals a freshly minted auth key under the organization's
// key, bound to the column it is stored in.
func (p *Provisioner) encryptAuthKey(ctx context.Context, organizationID, authKey string) (string, error) {
	keys, err := p.enc.For(organizationID, EncryptionUseCase)
	if err != nil {
		return "", fmt.Errorf("gateway auth key encryption: %w", err)
	}
	ciphertext, err := keys.Encrypt(ctx, authKeyColumn, []byte(authKey))
	if err != nil {
		return "", fmt.Errorf("encrypt gateway auth key: %w", err)
	}
	return ciphertext, nil
}

// encryptionFailure is the observation for an encryption error. An
// organization key that is suspended or unreachable gets its own code,
// because for a customer-managed key only the customer can fix it.
func encryptionFailure(err error) (k8s.NetworkIngressObservation, error) {
	if encryptionKeyUnavailable(err) {
		return errorObservation(k8s.NetworkIngressErrorEncryptionKeyUnavailable), err
	}
	return errorObservation(""), err
}

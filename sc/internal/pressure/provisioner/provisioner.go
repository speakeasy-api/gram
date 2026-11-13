package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
)

const (
	// DefaultSlots is how many node identities host each ingress.
	DefaultSlots = 3

	// DefaultAuthKeyTTL bounds how long an unused one-shot key stays valid.
	DefaultAuthKeyTTL = 7 * 24 * time.Hour

	// DefaultStaleAfter is how old a heartbeat may be before its host counts
	// as offline.
	DefaultStaleAfter = 2 * time.Minute

	// authKeyRenewMargin re-mints a key that would expire before the next
	// hourly sweep could notice.
	authKeyRenewMargin = 2 * time.Hour

	servicePortSpec = "tcp:443"
	serviceComment  = "Speakeasy private MCP ingress"
)

// Config tunes the gateway provider.
type Config struct {
	Tag        string
	Slots      int32
	AuthKeyTTL time.Duration
	StaleAfter time.Duration
	// Now supplies the clock; nil means time.Now.
	Now func() time.Time
}

// Provisioner implements the network ingress provider for gateway-hosted
// ingresses.
//
// Its two charts are declared once, in New, with their services named but
// not implemented. Each Apply and Delete binds them to the call's ingress
// and credentials, which so stay out of the chart context that drivers
// persist and effects carry, and runs one instance per pass to completion.
type Provisioner struct {
	logger   *slog.Logger
	store    Store
	keys     Keys
	api      TailscaleAPI
	cfg      Config
	slots    *sc.Machine[SlotState, slotPass]
	teardown *sc.Machine[TeardownState, teardownPass]
}

func serviceName(label string) string { return host.ServiceName(label) }

// New validates the configuration and returns the provider. keys may be nil
// when organization encryption is not configured, in which case Apply is
// unavailable while observation and teardown still work.
func New(logger *slog.Logger, store Store, keys Keys, api TailscaleAPI, cfg Config) (*Provisioner, error) {
	if logger == nil || store == nil || api == nil {
		return nil, errors.New("tailnet gateway provisioner requires a logger, a store and a Tailscale API client")
	}
	if !strings.HasPrefix(cfg.Tag, "tag:") || len(cfg.Tag) == len("tag:") {
		return nil, fmt.Errorf("invalid Tailscale tag %q", cfg.Tag)
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
	slots, err := newSlotChart()
	if err != nil {
		return nil, err
	}
	teardown, err := newTeardownChart()
	if err != nil {
		return nil, err
	}
	return &Provisioner{
		logger: logger.With(slog.String("component", "tailnet_gateway_provisioner")),
		store:  store, keys: keys, api: api, cfg: cfg, slots: slots, teardown: teardown,
	}, nil
}

// runPass runs one instance of m with context c until it reaches a final
// state, on the calling goroutine apart from its services, and returns where
// it ended.
func runPass[S comparable, C any](ctx context.Context, m *sc.Machine[S, C], c C) (sc.Snapshot[S, C], error) {
	inst, err := m.Instance(ctx, c)
	if err != nil {
		// Only an action can fail here, and neither chart has one.
		return sc.Snapshot[S, C]{}, fmt.Errorf("start %s: %w", m.ID(), err)
	}
	defer inst.Close()
	snap, err := inst.Await(ctx, sc.Snapshot[S, C].Done)
	if err != nil {
		return snap, fmt.Errorf("run %s: %w", m.ID(), err)
	}
	return snap, nil
}

func (p *Provisioner) owner(ctx context.Context, id uuid.UUID) (Owner, error) {
	own, err := p.store.Owner(ctx, id)
	if errors.Is(err, ErrIngressNotFound) {
		return Owner{}, fmt.Errorf("%w: ingress does not exist", ErrInvalidDesiredState)
	}
	if err != nil {
		return Owner{}, fmt.Errorf("load ingress owner: %w", err)
	}
	return own, nil
}

func errorObservation(code string) Observation {
	return Observation{Status: StatusError, DNSName: "", ErrorCode: code, ConnectedAt: nil}
}

// validateDesired checks everything Apply needs before any side effect.
func (p *Provisioner) validateDesired(desired Desired) (Credentials, Observation, error) {
	if desired.ID == uuid.Nil || desired.OrganizationID == "" || desired.Provider != ProviderTailscale || len(desired.Credentials) == 0 {
		return Credentials{}, errorObservation(ErrorInvalidDesiredState), fmt.Errorf("%w: desired state is incomplete", ErrInvalidDesiredState)
	}
	if err := validateLabel(desired.Hostname); err != nil {
		return Credentials{}, errorObservation(ErrorInvalidDesiredState), fmt.Errorf("%w: %w", ErrInvalidDesiredState, err)
	}
	if p.keys == nil {
		return Credentials{}, errorObservation(""), errors.New("tailnet gateway provisioner needs organization encryption to store auth keys")
	}
	creds, err := ParseCredentials(desired.Credentials)
	if err != nil {
		return Credentials{}, errorObservation(ErrorInvalidCredentials), err
	}
	return creds, Observation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, nil
}

// validateLabel accepts a lowercase DNS label.
func validateLabel(value string) error {
	if value == "" || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return fmt.Errorf("invalid service label %q", value)
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return fmt.Errorf("invalid service label %q", value)
		}
	}
	return nil
}

// Apply defines the service, ensures slot rows and reconciles each slot,
// then observes. When Apply fails while a host is online, the error
// observation keeps that host's DNS name, so a Tailscale outage does not cut
// live traffic.
func (p *Provisioner) Apply(ctx context.Context, desired Desired) (Observation, error) {
	observation, err := p.apply(ctx, desired)
	if err != nil {
		p.logFailure(ctx, "apply", desired.ID, desired.OrganizationID, observation.ErrorCode, err)
	}
	if err == nil || observation.DNSName != "" || desired.ID == uuid.Nil || desired.OrganizationID == "" {
		return observation, err
	}
	live, observeErr := p.observe(ctx, desired.ID, desired.OrganizationID)
	if observeErr == nil && live.Status == StatusOnline {
		observation.DNSName = live.DNSName
		observation.ConnectedAt = live.ConnectedAt
	}
	return observation, err
}

func (p *Provisioner) apply(ctx context.Context, desired Desired) (Observation, error) {
	creds, observation, err := p.validateDesired(desired)
	if err != nil {
		return observation, err
	}
	if err := p.keys.EnsureUseCase(ctx, desired.OrganizationID); err != nil {
		return encryptionFailure(fmt.Errorf("ensure gateway encryption key: %w", err))
	}
	// A key that exists can still be suspended or unreachable. Check it can
	// seal before minting: an auth key that cannot be stored is wasted.
	if _, err := p.keys.EncryptAuthKey(ctx, desired.OrganizationID, ""); err != nil {
		return encryptionFailure(fmt.Errorf("encrypt gateway auth key: %w", err))
	}

	if err := p.api.UpsertService(ctx, creds, Service{
		Name: serviceName(desired.Hostname), Ports: []string{servicePortSpec}, Tags: []string{p.cfg.Tag}, Comment: serviceComment,
	}); err != nil {
		return apiFailure(err)
	}

	for slot := range p.cfg.Slots {
		if err := p.store.EnsureSlot(ctx, desired.ID, desired.OrganizationID, slot); err != nil {
			return errorObservation(""), fmt.Errorf("ensure gateway slot: %w", err)
		}
	}
	if err := p.store.RetireSlots(ctx, desired.ID, desired.OrganizationID, p.cfg.Slots); err != nil {
		return errorObservation(""), fmt.Errorf("retire surplus gateway slots: %w", err)
	}

	nodes, err := p.store.ListSlots(ctx, desired.ID, desired.OrganizationID)
	if err != nil {
		return errorObservation(""), fmt.Errorf("list gateway slots: %w", err)
	}
	if err := p.deleteRenamedServices(ctx, creds, desired.Hostname, nodes); err != nil {
		return apiFailure(err)
	}

	slots, err := slotPassEnv{p: p, desired: desired, creds: creds}.bind(p.slots)
	if err != nil {
		return errorObservation(""), err
	}
	now := p.cfg.Now()
	for _, node := range nodes {
		snap, err := runPass(ctx, slots, slotPass{Facts: p.factsOf(node, now), Registered: false, Reset: false, Failure: nil})
		if err != nil {
			return errorObservation(""), err
		}
		if snap.In(SlotFailed) {
			return failureObservation(snap.Context.Failure)
		}
	}

	return p.observe(ctx, desired.ID, desired.OrganizationID)
}

// factsOf is what the original's per-slot loop read from the row.
func (p *Provisioner) factsOf(node SlotRow, now time.Time) slotFacts {
	return slotFacts{
		Slot:     node.Slot,
		NodeID:   node.NodeID,
		Retired:  node.Retired,
		Hosted:   p.fresh(node, now) && hostingStatus(node.Status),
		Refused:  node.Status == string(host.NodeStatusNeedsLogin),
		KeyValid: node.HasAuthKey && !node.AuthKeyExpiresAt.IsZero() && node.AuthKeyExpiresAt.After(now.Add(authKeyRenewMargin)),
		Surplus:  !node.Retired && node.Slot >= p.cfg.Slots,
	}
}

// fresh reports whether the slot was heartbeat within StaleAfter.
func (p *Provisioner) fresh(node SlotRow, now time.Time) bool {
	return !node.LastSeenAt.IsZero() && node.LastSeenAt.After(now.Add(-p.cfg.StaleAfter))
}

// authKeyDescription describes the auth key minted for one slot. Tailscale
// rejects punctuation and caps descriptions at 50 characters.
func authKeyDescription(ingressID uuid.UUID, slot int32) string {
	return fmt.Sprintf("Speakeasy gateway %s slot %d", ingressID.String()[:8], slot)
}

// deleteRenamedServices removes services advertised under a previous label.
func (p *Provisioner) deleteRenamedServices(ctx context.Context, creds Credentials, label string, nodes []SlotRow) error {
	seen := map[string]struct{}{}
	for _, node := range nodes {
		previous, _, _ := strings.Cut(node.DNSName, ".")
		if previous == "" || previous == label {
			continue
		}
		if _, done := seen[previous]; done {
			continue
		}
		seen[previous] = struct{}{}
		if err := p.api.DeleteService(ctx, creds, serviceName(previous)); err != nil {
			return fmt.Errorf("delete renamed service: %w", err)
		}
	}
	return nil
}

// Observe reports the ingress online when any slot has a fresh online
// heartbeat, degraded when every heartbeat is stale, and pending otherwise.
func (p *Provisioner) Observe(ctx context.Context, resources ResourceNames) (Observation, error) {
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		p.logFailure(ctx, "observe", resources.OwnerID, "", operationErrorCode(err), err)
		return errorObservation(ErrorInvalidDesiredState), err
	}
	observation, err := p.observe(ctx, resources.OwnerID, own.OrganizationID)
	if err != nil {
		p.logFailure(ctx, "observe", resources.OwnerID, own.OrganizationID, observation.ErrorCode, err)
	}
	return observation, err
}

// observe folds the slot rows into one observation. It is not a chart: it
// has no state of its own between calls, only a precedence among the
// statuses the pods wrote (online, then awaiting approval, then awaiting a
// certificate, then failed, then degraded).
func (p *Provisioner) observe(ctx context.Context, id uuid.UUID, organizationID string) (Observation, error) {
	nodes, err := p.store.ListSlots(ctx, id, organizationID)
	if err != nil {
		return errorObservation(""), fmt.Errorf("list gateway slots: %w", err)
	}
	now := p.cfg.Now()
	observation := Observation{Status: StatusPending, DNSName: "", ErrorCode: "", ConnectedAt: nil}
	seen, failed, unapproved, uncertified := false, false, false, false
	for _, node := range nodes {
		if node.Retired || node.LastSeenAt.IsZero() {
			continue
		}
		fresh := p.fresh(node, now)
		seen = seen || fresh
		switch {
		case fresh && node.Status == string(host.NodeStatusOnline) && node.DNSName != "":
			observation.Status = StatusOnline
			observation.DNSName = node.DNSName
			if !node.ConnectedSince.IsZero() && (observation.ConnectedAt == nil || node.ConnectedSince.Before(*observation.ConnectedAt)) {
				connected := node.ConnectedSince
				observation.ConnectedAt = &connected
			}
		case fresh && (node.Status == string(host.NodeStatusError) || node.Status == string(host.NodeStatusNeedsLogin)):
			failed = true
		case fresh && node.Status == string(host.NodeStatusAwaitingApproval):
			unapproved = true
		case fresh && node.Status == string(host.NodeStatusAwaitingCertificate):
			uncertified = true
		}
	}
	switch {
	case observation.Status == StatusOnline:
		return observation, nil
	case unapproved:
		return errorObservation(ErrorProviderServiceNotApproved), nil
	case uncertified:
		return errorObservation(ErrorProviderCertificateUnavailable), nil
	case failed:
		return errorObservation(""), nil
	}
	if !seen && anyHeartbeat(nodes) {
		observation.Status = StatusDegraded
	}
	return observation, nil
}

func anyHeartbeat(nodes []SlotRow) bool {
	for _, node := range nodes {
		if !node.LastSeenAt.IsZero() && !node.Retired {
			return true
		}
	}
	return false
}

// Delete without credentials only succeeds when the gateway never touched
// the tailnet for this ingress.
func (p *Provisioner) Delete(ctx context.Context, resources ResourceNames) error {
	return p.DeleteWithCredentials(ctx, resources, nil)
}

// DeleteWithCredentials retires every slot, waits for gateway pods to stop
// hosting, then deletes the devices and the service and purges the
// persisted node identities.
func (p *Provisioner) DeleteWithCredentials(ctx context.Context, resources ResourceNames, credentials []byte) error {
	own, err := p.owner(ctx, resources.OwnerID)
	if err != nil {
		p.logFailure(ctx, "delete", resources.OwnerID, "", operationErrorCode(err), err)
		return err
	}
	err = p.deleteWithCredentials(ctx, own, resources, credentials)
	if err != nil && !errors.Is(err, ErrDeletionPending) {
		p.logFailure(ctx, "delete", resources.OwnerID, own.OrganizationID, operationErrorCode(err), err)
	}
	return err
}

func (p *Provisioner) deleteWithCredentials(ctx context.Context, own Owner, resources ResourceNames, credentials []byte) error {
	pass := teardownPass{Owner: own, HasCredentials: len(credentials) > 0, CredentialsOK: false, NodeIDs: nil, HostedSlot: 0, Failure: nil}
	var creds Credentials
	var credsErr error
	if pass.HasCredentials {
		creds, credsErr = ParseCredentials(credentials)
		pass.CredentialsOK = credsErr == nil
	}
	chart, err := teardownPassEnv{p: p, id: resources, creds: creds}.bind(p.teardown)
	if err != nil {
		return err
	}
	snap, err := runPass(ctx, chart, pass)
	if err != nil {
		return err
	}
	switch {
	case snap.In(TeardownUntouched), snap.In(TeardownDeleted):
		return nil
	case snap.In(TeardownRejected) && !pass.HasCredentials:
		return fmt.Errorf("%w: credentials are required to delete the tailnet service and devices", ErrProviderCredentialsRejected)
	case snap.In(TeardownRejected):
		return fmt.Errorf("%w: %w", ErrProviderCredentialsRejected, credsErr)
	case snap.In(TeardownWaiting):
		return fmt.Errorf("%w: slot %d is still hosted", ErrDeletionPending, snap.Context.HostedSlot)
	default:
		return snap.Context.Failure
	}
}

// operationErrorCode is the bounded code for an error from an operation that
// reports no observation.
func operationErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidDesiredState):
		return ErrorInvalidDesiredState
	case errors.Is(err, ErrProviderCredentialsRejected):
		// Forbidden wraps rejected credentials, so it is reported as such,
		// as the original's order did.
		return ErrorProviderCredentialsRejected
	case errors.Is(err, ErrProviderForbidden):
		return ErrorProviderForbidden
	case errors.Is(err, ErrProviderRateLimited):
		return ErrorProviderRateLimited
	case errors.Is(err, ErrProviderAPIUnavailable):
		return ErrorProviderAPIUnavailable
	case errors.Is(err, host.ErrEncryptionKeyUnavailable):
		return ErrorEncryptionKeyUnavailable
	default:
		return ""
	}
}

// logFailure records why a provisioner operation failed. Failures with a
// specific error code are warnings; anything unclassified is an error.
func (p *Provisioner) logFailure(ctx context.Context, operation string, ingressID uuid.UUID, organizationID, errorCode string, err error) {
	attrs := []any{
		slog.String("network_ingress.operation", operation),
		slog.String("network_ingress.id", ingressID.String()),
		slog.Any("error", err),
	}
	if organizationID != "" {
		attrs = append(attrs, slog.String("organization.id", organizationID))
	}
	if errorCode != "" {
		attrs = append(attrs, slog.String("network_ingress.error_code", errorCode))
		p.logger.WarnContext(ctx, "tailnet gateway provisioner operation failed", attrs...)
		return
	}
	p.logger.ErrorContext(ctx, "tailnet gateway provisioner operation failed", attrs...)
}

// mapAPIError tags a Tailscale API failure with the provider-neutral sentinel.
func mapAPIError(err error) error {
	var sentinel error
	switch {
	case errors.Is(err, ErrTailscaleForbidden):
		sentinel = ErrProviderForbidden
	case errors.Is(err, ErrTailscaleUnauthorized):
		sentinel = ErrProviderCredentialsRejected
	case errors.Is(err, ErrTailscaleRateLimited):
		sentinel = ErrProviderRateLimited
	case errors.Is(err, ErrTailscaleUnavailable):
		sentinel = ErrProviderAPIUnavailable
	default:
		return err
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}

// apiFailure is the observation and error Apply reports for a failed
// Tailscale API call. A rejected OAuth client keeps invalid_credentials;
// the teardown blocker sentinel is only for Delete.
func apiFailure(err error) (Observation, error) {
	switch {
	case errors.Is(err, ErrTailscaleForbidden):
		return errorObservation(ErrorProviderForbidden), mapAPIError(err)
	case errors.Is(err, ErrTailscaleUnauthorized):
		return errorObservation(ErrorInvalidCredentials), err
	case errors.Is(err, ErrTailscaleRateLimited):
		return errorObservation(ErrorProviderRateLimited), mapAPIError(err)
	case errors.Is(err, ErrTailscaleUnavailable):
		return errorObservation(ErrorProviderAPIUnavailable), mapAPIError(err)
	default:
		return errorObservation(""), err
	}
}

// encryptionFailure is the observation for an encryption error.
func encryptionFailure(err error) (Observation, error) {
	if errors.Is(err, host.ErrEncryptionKeyUnavailable) {
		return errorObservation(ErrorEncryptionKeyUnavailable), err
	}
	return errorObservation(""), err
}

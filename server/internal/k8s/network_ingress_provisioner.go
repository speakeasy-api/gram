package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	NetworkIngressProviderTailscale = "tailscale"

	NetworkIngressStatusPending  = "pending"
	NetworkIngressStatusOnline   = "online"
	NetworkIngressStatusDegraded = "degraded"
	NetworkIngressStatusError    = "error"

	NetworkIngressErrorInvalidDesiredState = "invalid_desired_state"
	NetworkIngressErrorUnsupportedProvider = "unsupported_provider"
	NetworkIngressErrorInvalidCredentials  = "invalid_credentials" // #nosec G101 -- bounded status code, not credential material.
	NetworkIngressErrorKubernetes          = "kubernetes_api"
)

var (
	ErrNetworkIngressUnsupportedProvider = errors.New("unsupported network ingress provider")
	ErrNetworkIngressInvalidDesiredState = errors.New("invalid network ingress desired state")
	ErrNetworkIngressReplacementPending  = errors.New("network ingress immutable replacement pending")
)

// NetworkIngressResourceNames is the provider-neutral, persisted identity of
// every Kubernetes resource owned by one ingress. Reconciliation and deletion
// consume this value verbatim; they never derive names from mutable customer
// input or from a tombstone.
type NetworkIngressResourceNames struct {
	OwnerID                uuid.UUID `json:"owner_id"`
	Namespace              string    `json:"namespace"`
	CredentialsSecret      string    `json:"credentials_secret"`
	Tailnet                string    `json:"tailnet"`
	ProxyGroup             string    `json:"proxy_group"`
	ProxyGroupPolicy       string    `json:"proxy_group_policy"`
	AttestorServiceAccount string    `json:"attestor_service_account"`
	AttestorCASecret       string    `json:"attestor_ca_secret"`
	AttestorDeployment     string    `json:"attestor_deployment"`
	AttestorService        string    `json:"attestor_service"`
	AttestorNetworkPolicy  string    `json:"attestor_network_policy"`
	ProxyNetworkPolicy     string    `json:"proxy_network_policy"`
	Ingress                string    `json:"ingress"`
}

func NewNetworkIngressResourceNames(id uuid.UUID) (NetworkIngressResourceNames, error) {
	if id == uuid.Nil {
		return NetworkIngressResourceNames{}, fmt.Errorf("%w: ingress id is required", ErrNetworkIngressInvalidDesiredState)
	}
	suffix := strings.ToLower(strings.ReplaceAll(id.String(), "-", ""))
	prefix := "gram-netingress-" + suffix[:20]
	return NetworkIngressResourceNames{
		OwnerID:                id,
		Namespace:              prefix,
		CredentialsSecret:      prefix + "-oauth",
		Tailnet:                prefix,
		ProxyGroup:             prefix,
		ProxyGroupPolicy:       prefix,
		AttestorServiceAccount: prefix + "-attestor",
		AttestorCASecret:       prefix + "-ca",
		AttestorDeployment:     prefix + "-attestor",
		AttestorService:        prefix + "-attestor",
		AttestorNetworkPolicy:  prefix + "-attestor",
		ProxyNetworkPolicy:     prefix + "-proxy",
		Ingress:                prefix,
	}, nil
}

func (n NetworkIngressResourceNames) Validate() error {
	if n.OwnerID == uuid.Nil {
		return fmt.Errorf("%w: resource owner is required", ErrNetworkIngressInvalidDesiredState)
	}
	values := []string{
		n.Namespace,
		n.CredentialsSecret,
		n.Tailnet,
		n.ProxyGroup,
		n.ProxyGroupPolicy,
		n.AttestorServiceAccount,
		n.AttestorCASecret,
		n.AttestorDeployment,
		n.AttestorService,
		n.AttestorNetworkPolicy,
		n.ProxyNetworkPolicy,
		n.Ingress,
	}
	for _, value := range values {
		if len(k8svalidation.IsDNS1123Label(value)) > 0 {
			return fmt.Errorf("%w: resource identity is invalid", ErrNetworkIngressInvalidDesiredState)
		}
	}
	return nil
}

func (n NetworkIngressResourceNames) Marshal() ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(n)
	if err != nil {
		return nil, fmt.Errorf("marshal network ingress resource names: %w", err)
	}
	return encoded, nil
}

func ParseNetworkIngressResourceNames(encoded []byte) (NetworkIngressResourceNames, error) {
	var names NetworkIngressResourceNames
	if len(encoded) == 0 {
		return names, fmt.Errorf("%w: resource identities are required", ErrNetworkIngressInvalidDesiredState)
	}
	if err := json.Unmarshal(encoded, &names); err != nil {
		return names, fmt.Errorf("parse network ingress resource names: %w", err)
	}
	if err := names.Validate(); err != nil {
		return NetworkIngressResourceNames{}, err
	}
	return names, nil
}

// NetworkIngressDesired carries provider-neutral desired state. Credentials are
// decrypted provider-opaque bytes and must remain in-memory only. Only the
// selected provider adapter may decode their contents.
type NetworkIngressDesired struct {
	ID             uuid.UUID
	Provider       string
	Hostname       string
	Credentials    []byte
	Resources      NetworkIngressResourceNames
	AttestorImage  string
	BackendService string
	BackendPort    int32
}

func (d NetworkIngressDesired) Validate() error {
	if d.ID == uuid.Nil || d.Provider == "" || d.Hostname == "" || len(d.Credentials) == 0 || d.AttestorImage == "" || d.BackendService == "" || d.BackendPort <= 0 {
		return fmt.Errorf("%w: desired state is incomplete", ErrNetworkIngressInvalidDesiredState)
	}
	if err := d.Resources.Validate(); err != nil {
		return err
	}
	if d.Resources.OwnerID != d.ID {
		return fmt.Errorf("%w: resource owner does not match ingress", ErrNetworkIngressInvalidDesiredState)
	}
	return nil
}

type NetworkIngressObservation struct {
	Status      string
	DNSName     string
	ErrorCode   string
	ConnectedAt *time.Time
}

type NetworkIngressProvisioner interface {
	Apply(context.Context, NetworkIngressDesired) (NetworkIngressObservation, error)
	Observe(context.Context, NetworkIngressResourceNames) (NetworkIngressObservation, error)
	Delete(context.Context, NetworkIngressResourceNames) error
}

type NetworkIngressProvisionerRegistry struct {
	providers map[string]NetworkIngressProvisioner
}

func NewNetworkIngressProvisionerRegistry(providers map[string]NetworkIngressProvisioner) (*NetworkIngressProvisionerRegistry, error) {
	registry := &NetworkIngressProvisionerRegistry{providers: make(map[string]NetworkIngressProvisioner, len(providers))}
	for provider, provisioner := range providers {
		provider = strings.TrimSpace(provider)
		if provider == "" || provisioner == nil {
			return nil, fmt.Errorf("%w: provider registry entry is invalid", ErrNetworkIngressInvalidDesiredState)
		}
		registry.providers[provider] = provisioner
	}
	return registry, nil
}

func (r *NetworkIngressProvisionerRegistry) Provisioner(provider string) (NetworkIngressProvisioner, error) {
	if r == nil {
		return nil, ErrNetworkIngressUnsupportedProvider
	}
	provisioner, ok := r.providers[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNetworkIngressUnsupportedProvider, provider)
	}
	return provisioner, nil
}

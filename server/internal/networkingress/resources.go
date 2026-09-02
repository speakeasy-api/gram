package networkingress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// ResourceNames is the provider-neutral, persisted identity of every
// Kubernetes resource AIS-605 creates for one ingress. Keep this JSON contract
// in lock-step with k8s.NetworkIngressResourceNames; the parallel PRs cannot
// import each other until they share a merged base.
type ResourceNames struct {
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

func NewResourceNames(id uuid.UUID) (ResourceNames, error) {
	if id == uuid.Nil {
		return ResourceNames{}, fmt.Errorf("network ingress id is required")
	}
	digest := sha256.Sum256(id[:])
	prefix := "gram-netingress-" + hex.EncodeToString(digest[:10])
	return ResourceNames{
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

func (n ResourceNames) Marshal() ([]byte, error) {
	if n.OwnerID == uuid.Nil {
		return nil, fmt.Errorf("network ingress resource owner is required")
	}
	expected, err := NewResourceNames(n.OwnerID)
	if err != nil {
		return nil, err
	}
	if n != expected {
		return nil, fmt.Errorf("network ingress resource identities do not match owner")
	}
	encoded, err := json.Marshal(n)
	if err != nil {
		return nil, fmt.Errorf("marshal network ingress resource names: %w", err)
	}
	return encoded, nil
}

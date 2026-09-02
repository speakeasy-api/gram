package networkingress

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/k8s"
)

// ResourceNames aliases the canonical provider-neutral resource identity
// contract owned by the Kubernetes provisioner layer.
type ResourceNames = k8s.NetworkIngressResourceNames

// NewResourceNames is retained for compatibility with management callers while
// delegating construction and validation to the canonical provisioner contract.
func NewResourceNames(id uuid.UUID) (ResourceNames, error) {
	names, err := k8s.NewNetworkIngressResourceNames(id)
	if err != nil {
		return ResourceNames{}, fmt.Errorf("create network ingress resource names: %w", err)
	}
	return names, nil
}

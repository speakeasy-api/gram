package tailnetgateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/k8s"
)

// OperatorProvisioner is the Tailscale Kubernetes operator provider as seen
// by the cutover: it can report whether it still owns resources and inventory
// orphans in addition to the lifecycle methods.
type OperatorProvisioner interface {
	k8s.NetworkIngressProvisioner
	HasResources(context.Context, k8s.NetworkIngressResourceNames) (bool, error)
	FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error)
}

// CompositeProvisioner routes each ingress to the gateway while the operator
// provider, when present, tears down resources it still owns. New ingresses
// go straight to the gateway; an operator-managed ingress is moved once its
// Kubernetes resources are gone.
type CompositeProvisioner struct {
	operator OperatorProvisioner
	gateway  *Provisioner
}

var (
	_ k8s.NetworkIngressProvisioner         = (*CompositeProvisioner)(nil)
	_ k8s.NetworkIngressCredentialedDeleter = (*CompositeProvisioner)(nil)
)

// NewCompositeProvisioner builds the cutover provider. operator may be nil
// once the operator is decommissioned.
func NewCompositeProvisioner(operator OperatorProvisioner, gateway *Provisioner) (*CompositeProvisioner, error) {
	if gateway == nil {
		return nil, errors.New("composite network ingress provisioner requires the gateway provider")
	}
	return &CompositeProvisioner{operator: operator, gateway: gateway}, nil
}

//nolint:exhaustruct // observations populate only the state they know.
func (c *CompositeProvisioner) Apply(ctx context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	// Never dismantle the operator backend for a desired state the gateway
	// would reject anyway.
	if _, observation, err := c.gateway.ValidateDesired(desired); err != nil {
		return observation, err
	}
	if c.operator != nil {
		present, err := c.operator.HasResources(ctx, desired.Resources)
		if err != nil {
			return errorObservation(k8s.NetworkIngressErrorKubernetes), fmt.Errorf("check operator resources: %w", err)
		}
		if present {
			if err := c.operator.Delete(ctx, desired.Resources); err != nil {
				return k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusPending}, fmt.Errorf("tear down operator resources: %w", err)
			}
		}
	}
	return c.gateway.Apply(ctx, desired)
}

func (c *CompositeProvisioner) Observe(ctx context.Context, resources k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	if c.operator != nil {
		own, err := c.gateway.owner(ctx, resources.OwnerID)
		if err != nil {
			return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), err
		}
		if own.runtime != RuntimeGateway {
			observation, err := c.operator.Observe(ctx, resources)
			if err != nil {
				return observation, fmt.Errorf("observe operator resources: %w", err)
			}
			return observation, nil
		}
	}
	return c.gateway.Observe(ctx, resources)
}

func (c *CompositeProvisioner) Delete(ctx context.Context, resources k8s.NetworkIngressResourceNames) error {
	if err := c.deleteOperatorResources(ctx, resources); err != nil {
		return err
	}
	return c.gateway.Delete(ctx, resources)
}

func (c *CompositeProvisioner) DeleteWithCredentials(ctx context.Context, resources k8s.NetworkIngressResourceNames, credentials []byte) error {
	if err := c.deleteOperatorResources(ctx, resources); err != nil {
		return err
	}
	return c.gateway.DeleteWithCredentials(ctx, resources, credentials)
}

func (c *CompositeProvisioner) deleteOperatorResources(ctx context.Context, resources k8s.NetworkIngressResourceNames) error {
	if c.operator == nil {
		return nil
	}
	present, err := c.operator.HasResources(ctx, resources)
	if err != nil {
		return fmt.Errorf("check operator resources: %w", err)
	}
	if !present {
		return nil
	}
	if err := c.operator.Delete(ctx, resources); err != nil {
		return fmt.Errorf("tear down operator resources: %w", err)
	}
	return nil
}

// FindOrphans delegates to the operator; the gateway owns no Kubernetes
// resources.
func (c *CompositeProvisioner) FindOrphans(ctx context.Context, known []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
	if c.operator == nil {
		return nil, nil
	}
	orphans, err := c.operator.FindOrphans(ctx, known)
	if err != nil {
		return nil, fmt.Errorf("inventory operator resources: %w", err)
	}
	return orphans, nil
}

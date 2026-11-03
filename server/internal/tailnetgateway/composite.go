package tailnetgateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
)

// OperatorProvisioner is the Tailscale Kubernetes operator provider as seen
// by the cutover: it can report whether it still owns resources and inventory
// orphans in addition to the lifecycle methods.
type OperatorProvisioner interface {
	k8s.NetworkIngressProvisioner
	HasResources(context.Context, k8s.NetworkIngressResourceNames) (bool, error)
	FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error)
}

// CompositeProvisioner routes each ingress by its runtime column. An ingress
// whose runtime is "gateway" is hosted by the tailnet gateway, and any
// Kubernetes resources the operator still owns for it are torn down first.
// Every other ingress stays on the operator: moving one is an explicit,
// per-organization decision recorded in the runtime column, never a side
// effect of enabling the gateway.
type CompositeProvisioner struct {
	operator OperatorProvisioner
	gateway  *Provisioner

	// gatewayDeployed reports whether gateway pods run in this environment.
	// Without them, gateway ingresses wait as provider-unavailable rather
	// than being picked up by the operator.
	gatewayDeployed bool
}

var (
	_ k8s.NetworkIngressProvisioner         = (*CompositeProvisioner)(nil)
	_ k8s.NetworkIngressCredentialedDeleter = (*CompositeProvisioner)(nil)
)

// NewCompositeProvisioner builds the cutover provider. operator may be nil
// once the operator is decommissioned. gatewayDeployed is false until the
// gateway pods run in this environment.
func NewCompositeProvisioner(operator OperatorProvisioner, gateway *Provisioner, gatewayDeployed bool) (*CompositeProvisioner, error) {
	if gateway == nil {
		return nil, errors.New("composite network ingress provisioner requires the gateway provider")
	}
	return &CompositeProvisioner{operator: operator, gateway: gateway, gatewayDeployed: gatewayDeployed}, nil
}

//nolint:exhaustruct // observations populate only the state they know.
func (c *CompositeProvisioner) Apply(ctx context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	onGateway, err := c.onGateway(ctx, desired.ID)
	if err != nil {
		return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), err
	}
	if !onGateway {
		if c.operator == nil {
			return errorObservation(k8s.NetworkIngressErrorUnsupportedProvider), fmt.Errorf("%w: the operator provider is disabled and this ingress has not been moved to the tailnet gateway", k8s.ErrNetworkIngressUnsupportedProvider)
		}
		observation, err := c.operator.Apply(ctx, desired)
		if err != nil {
			return observation, fmt.Errorf("apply operator resources: %w", err)
		}
		return observation, nil
	}

	if !c.gatewayDeployed {
		return k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusPending}, fmt.Errorf("%w: the tailnet gateway is not deployed in this environment", k8s.ErrNetworkIngressProviderUnavailable)
	}

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
	onGateway, err := c.onGateway(ctx, resources.OwnerID)
	if err != nil {
		return errorObservation(k8s.NetworkIngressErrorInvalidDesiredState), err
	}
	if onGateway || c.operator == nil {
		return c.gateway.Observe(ctx, resources)
	}
	observation, err := c.operator.Observe(ctx, resources)
	if err != nil {
		return observation, fmt.Errorf("observe operator resources: %w", err)
	}
	return observation, nil
}

// onGateway reports whether the ingress has been moved to the tailnet gateway.
func (c *CompositeProvisioner) onGateway(ctx context.Context, id uuid.UUID) (bool, error) {
	own, err := c.gateway.owner(ctx, id)
	if err != nil {
		return false, err
	}
	return own.runtime == networkingress.RuntimeGateway, nil
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

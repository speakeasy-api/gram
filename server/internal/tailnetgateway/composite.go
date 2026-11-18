package tailnetgateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
)

// OperatorProvisioner is the Tailscale Kubernetes operator provider: the
// lifecycle methods plus the orphan inventory the reconciler sweeps.
type OperatorProvisioner interface {
	k8s.NetworkIngressProvisioner
	FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error)
}

// CompositeProvisioner routes each ingress by its runtime column. An ingress
// whose runtime is "gateway" is hosted only by the tailnet gateway; every
// other ingress is handled only by the operator, exactly as before the
// gateway existed. Nothing here moves an ingress between the two.
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

// NewCompositeProvisioner builds the routing provider. operator may be nil
// once the operator is decommissioned. gatewayDeployed is false until the
// gateway pods run in this environment.
func NewCompositeProvisioner(operator OperatorProvisioner, gateway *Provisioner, gatewayDeployed bool) (*CompositeProvisioner, error) {
	if gateway == nil {
		return nil, errors.New("composite network ingress provisioner requires the gateway provider")
	}
	return &CompositeProvisioner{operator: operator, gateway: gateway, gatewayDeployed: gatewayDeployed}, nil
}

//nolint:exhaustruct_v5 // observations populate only the state they know.
func (c *CompositeProvisioner) Apply(ctx context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	if !onGateway(desired.Runtime) {
		if c.operator == nil {
			return errorObservation(k8s.NetworkIngressErrorUnsupportedProvider), errOperatorDisabled
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
	return c.gateway.Apply(ctx, desired)
}

func (c *CompositeProvisioner) Observe(ctx context.Context, resources k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	if onGateway(resources.Runtime) || c.operator == nil {
		return c.gateway.Observe(ctx, resources)
	}
	observation, err := c.operator.Observe(ctx, resources)
	if err != nil {
		return observation, fmt.Errorf("observe operator resources: %w", err)
	}
	return observation, nil
}

// onGateway reports whether the ingress is hosted by the tailnet gateway. The
// executor passes the runtime from the row it already holds: reading it here
// would take a second pooled connection while the executor holds one for its
// reconcile lock, and with as many reconciles in flight as the pool has
// connections every one of them would wait for a connection none releases.
func onGateway(runtime string) bool {
	return runtime == networkingress.RuntimeGateway
}

func (c *CompositeProvisioner) Delete(ctx context.Context, resources k8s.NetworkIngressResourceNames) error {
	return c.DeleteWithCredentials(ctx, resources, nil)
}

// DeleteWithCredentials tears down whichever provider hosts the ingress. The
// operator's teardown needs no credentials; the gateway's does.
func (c *CompositeProvisioner) DeleteWithCredentials(ctx context.Context, resources k8s.NetworkIngressResourceNames, credentials []byte) error {
	if onGateway(resources.Runtime) {
		return c.gateway.DeleteWithCredentials(ctx, resources, credentials)
	}
	if c.operator == nil {
		return errOperatorDisabled
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

var errOperatorDisabled = fmt.Errorf("%w: the operator provider is disabled and this ingress is not hosted by the tailnet gateway", k8s.ErrNetworkIngressUnsupportedProvider)

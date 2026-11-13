package provisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Orphan is an operator resource no ingress owns.
type Orphan struct {
	Names ResourceNames
}

// OperatorProvisioner is the Tailscale Kubernetes operator provider.
type OperatorProvisioner interface {
	Apply(ctx context.Context, desired Desired) (Observation, error)
	Observe(ctx context.Context, resources ResourceNames) (Observation, error)
	Delete(ctx context.Context, resources ResourceNames) error
	FindOrphans(ctx context.Context, known []ResourceNames) ([]Orphan, error)
}

// Composite routes each ingress by its runtime column. It is routing, not a
// lifecycle, and is ported as plain Go.
type Composite struct {
	operator        OperatorProvisioner
	gateway         *Provisioner
	gatewayDeployed bool
}

// NewComposite builds the routing provider. operator may be nil once the
// operator is decommissioned.
func NewComposite(operator OperatorProvisioner, gateway *Provisioner, gatewayDeployed bool) (*Composite, error) {
	if gateway == nil {
		return nil, errors.New("composite network ingress provisioner requires the gateway provider")
	}
	return &Composite{operator: operator, gateway: gateway, gatewayDeployed: gatewayDeployed}, nil
}

var errOperatorDisabled = fmt.Errorf("%w: the operator provider is disabled and this ingress is not hosted by the tailnet gateway", ErrUnsupportedProvider)

func (c *Composite) onGateway(ctx context.Context, id uuid.UUID) (bool, error) {
	own, err := c.gateway.owner(ctx, id)
	if err != nil {
		return false, err
	}
	return own.Runtime == RuntimeGateway, nil
}

// Apply routes to whichever provider hosts the ingress.
func (c *Composite) Apply(ctx context.Context, desired Desired) (Observation, error) {
	onGateway, err := c.onGateway(ctx, desired.ID)
	if err != nil {
		return errorObservation(ErrorInvalidDesiredState), err
	}
	if !onGateway {
		if c.operator == nil {
			return errorObservation(ErrorUnsupportedProvider), errOperatorDisabled
		}
		observation, err := c.operator.Apply(ctx, desired)
		if err != nil {
			return observation, fmt.Errorf("apply operator resources: %w", err)
		}
		return observation, nil
	}
	if !c.gatewayDeployed {
		return Observation{Status: StatusPending, DNSName: "", ErrorCode: "", ConnectedAt: nil},
			fmt.Errorf("%w: the tailnet gateway is not deployed in this environment", ErrProviderUnavailable)
	}
	return c.gateway.Apply(ctx, desired)
}

// Observe routes to whichever provider hosts the ingress.
func (c *Composite) Observe(ctx context.Context, resources ResourceNames) (Observation, error) {
	onGateway, err := c.onGateway(ctx, resources.OwnerID)
	if err != nil {
		return errorObservation(ErrorInvalidDesiredState), err
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

// Delete tears down without credentials.
func (c *Composite) Delete(ctx context.Context, resources ResourceNames) error {
	return c.DeleteWithCredentials(ctx, resources, nil)
}

// DeleteWithCredentials tears down whichever provider hosts the ingress.
func (c *Composite) DeleteWithCredentials(ctx context.Context, resources ResourceNames, credentials []byte) error {
	onGateway, err := c.onGateway(ctx, resources.OwnerID)
	if err != nil {
		return err
	}
	if onGateway {
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
func (c *Composite) FindOrphans(ctx context.Context, known []ResourceNames) ([]Orphan, error) {
	if c.operator == nil {
		return nil, nil
	}
	orphans, err := c.operator.FindOrphans(ctx, known)
	if err != nil {
		return nil, fmt.Errorf("inventory operator resources: %w", err)
	}
	return orphans, nil
}

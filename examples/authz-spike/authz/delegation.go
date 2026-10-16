package authz

import (
	"context"
	"fmt"
)

// DelegationKind distinguishes direct user execution from delegated agent use.
type DelegationKind string

const (
	// DirectExecution evaluates a user's policy without a represented principal.
	DirectExecution DelegationKind = "direct"

	// DelegatedExecution requires both agent and current delegator authority.
	DelegatedExecution DelegationKind = "delegated"
)

// Delegation is trusted execution context, not lifecycle ownership.
type Delegation struct {
	// Kind selects the supported execution path explicitly.
	Kind DelegationKind

	// Delegator names the represented user for delegated execution only.
	Delegator Principal
}

func validateDelegation(principal Principal, delegation Delegation) error {
	switch delegation.Kind {
	case DirectExecution:
		if principal.Kind != UserPrincipal || delegation.Delegator != (Principal{}) {
			return fmt.Errorf("direct execution requires a user and no delegator")
		}
	case DelegatedExecution:
		if err := validatePrincipal(delegation.Delegator); err != nil {
			return err
		}
		if principal.Kind != AgentPrincipal || delegation.Delegator.Kind != UserPrincipal || principal.OrganizationID != delegation.Delegator.OrganizationID {
			return fmt.Errorf("delegated execution requires an agent and user delegator in the same organization")
		}
	default:
		return fmt.Errorf("unknown delegation kind %q", delegation.Kind)
	}
	return nil
}

// BindDelegatedCredential records a domain-authorized agent/delegator binding.
// Authentication must establish the delegator before issuance; this method neither
// authenticates a user nor authorizes issuing credentials or delegating authority.
func (c *Client) BindDelegatedCredential(ctx context.Context, agent, delegator Principal, credentialID string) error {
	delegation := Delegation{Kind: DelegatedExecution, Delegator: delegator}
	if err := validatePrincipal(agent); err != nil {
		return err
	}
	if err := validateDelegation(agent, delegation); err != nil {
		return err
	}
	if credentialID == "" || credentialID == "*" {
		return fmt.Errorf("credential binding requires a concrete credential ID")
	}
	writer, err := c.credentialWriter()
	if err != nil {
		return err
	}
	return writer.BindCredential(ctx, agent, credentialID, delegation)
}

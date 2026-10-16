package authz

import (
	"context"
	"fmt"
)

// CredentialWriter records domain-authorized identity bindings, not grant policy.
type CredentialWriter interface {
	// BindCredential stores an immutable principal and delegation binding.
	BindCredential(context.Context, Principal, string, Delegation) error
}

func (c *Client) credentialWriter() (CredentialWriter, error) {
	writer, ok := c.backend.(CredentialWriter)
	if !ok {
		return nil, fmt.Errorf("authorization backend does not support credential binding writes")
	}
	return writer, nil
}

// BindCredential records an authenticated credential's principal, not permissions.
// The calling management domain must independently authorize issuance.
func (c *Client) BindCredential(ctx context.Context, principal Principal, credentialID string) error {
	if err := validatePrincipal(principal); err != nil {
		return err
	}
	if principal.Kind != UserPrincipal {
		return fmt.Errorf("agent credentials require an explicit delegator binding")
	}
	if credentialID == "" || credentialID == "*" {
		return fmt.Errorf("credential binding requires a concrete credential ID")
	}
	writer, err := c.credentialWriter()
	if err != nil {
		return err
	}
	return writer.BindCredential(ctx, principal, credentialID, Delegation{Kind: DirectExecution})
}

type credentialBinding struct {
	principal  Principal
	delegation Delegation
}

type credentialKey struct {
	organizationID string
	id             string
}

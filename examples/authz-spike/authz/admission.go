package authz

import "fmt"

// admittedExecution is private, short-lived input to policy resolution. It is
// created and consumed under the same memory lock; it is never put in context.
type admittedExecution struct {
	// principals names the actor followed by any current delegator.
	principals []Principal
}

// admitExecutionLocked checks a trusted middleware assertion against stored
// credential bindings. The caller must hold b.mu for admission and resolution.
// This fixture boundary does not authenticate sessions or prove human membership,
// credential expiry/revocation, agent lifecycle, or actual external invoker identity.
func (b *MemoryBackend) admitExecutionLocked(subject Subject) (admittedExecution, error) {
	if err := validateSubject(subject); err != nil {
		return admittedExecution{}, err
	}
	if subject.Authentication.Kind == CredentialAuthentication {
		credential, exists := b.credentials[credentialKey{organizationID: subject.Principal.OrganizationID, id: subject.Authentication.ID}]
		if !exists || credential.principal != subject.Principal || credential.delegation != subject.Delegation {
			return admittedExecution{}, fmt.Errorf("%w: credential does not match principal and delegation context", ErrDenied)
		}
	}
	principals := []Principal{subject.Principal}
	if subject.Delegation.Kind == DelegatedExecution {
		principals = append(principals, subject.Delegation.Delegator)
	}
	return admittedExecution{principals: principals}, nil
}

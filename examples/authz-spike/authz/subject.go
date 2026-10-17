package authz

import "fmt"

// PrincipalKind identifies whose policy is evaluated, independently of transport.
type PrincipalKind string

const (
	// UserPrincipal identifies a human user's effective policy.
	UserPrincipal PrincipalKind = "user"

	// AgentPrincipal identifies an agent's independently assigned policy.
	AgentPrincipal PrincipalKind = "agent"
)

// Principal is a tenant-bound policy identity, usable without authentication data
// when provisioning grants or recording credential bindings.
type Principal struct {
	// OrganizationID is part of the identity, not an optional request dimension.
	OrganizationID string

	// Kind distinguishes users and agents even when their IDs are equal.
	Kind PrincipalKind

	// ID identifies the principal within its organization and kind.
	ID string
}

// AuthenticationKind explicitly selects the supported authentication path.
type AuthenticationKind string

const (
	// SessionAuthentication represents an authenticated human session.
	SessionAuthentication AuthenticationKind = "session"

	// CredentialAuthentication requires a stored credential-to-principal binding.
	CredentialAuthentication AuthenticationKind = "credential"
)

// Authentication identifies how middleware authenticated a principal.
type Authentication struct {
	// Kind selects the path; an empty or unknown kind is invalid.
	Kind AuthenticationKind

	// ID identifies the authenticated session or credential.
	ID string
}

// Subject combines the actor, authentication, and delegation context. Middleware
// supplies these; structural validity alone does not establish their authenticity.
type Subject struct {
	// Principal identifies whose policy the backend loads.
	Principal Principal

	// Authentication selects session handling or credential binding validation.
	Authentication Authentication

	// Delegation explicitly selects direct execution or a trusted on-behalf-of path.
	Delegation Delegation
}

func validatePrincipal(principal Principal) error {
	if principal.OrganizationID == "" || principal.OrganizationID == "*" || principal.ID == "" || principal.ID == "*" {
		return fmt.Errorf("principal requires a concrete organization and ID")
	}
	switch principal.Kind {
	case UserPrincipal, AgentPrincipal:
		return nil
	default:
		return fmt.Errorf("unknown principal kind %q", principal.Kind)
	}
}

func validateSubject(subject Subject) error {
	if err := validatePrincipal(subject.Principal); err != nil {
		return err
	}
	if err := validateDelegation(subject.Principal, subject.Delegation); err != nil {
		return err
	}
	if subject.Authentication.ID == "" || subject.Authentication.ID == "*" {
		return fmt.Errorf("authentication requires a concrete session or credential ID")
	}
	switch subject.Authentication.Kind {
	case SessionAuthentication:
		if subject.Principal.Kind != UserPrincipal {
			return fmt.Errorf("this spike supports human sessions only for user principals")
		}
	case CredentialAuthentication:
		// Both users and agents may authenticate using bound credentials.
	default:
		return fmt.Errorf("unknown authentication kind %q", subject.Authentication.Kind)
	}
	return nil
}

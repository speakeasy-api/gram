package orgencryption

import (
	"errors"
	"fmt"
)

// Mode says who controls an organization's KEK. Stored in
// organization_encryption_configs.mode.
type Mode string

const (
	// ModeSpeakeasyManaged means Speakeasy minted the KEK in its own key ring.
	ModeSpeakeasyManaged Mode = "speakeasy_managed"

	// ModeCustomerManaged means the KEK lives in the customer's KMS.
	ModeCustomerManaged Mode = "customer_managed"
)

// ConfigState is the lifecycle state of an organization's configuration.
// Stored in organization_encryption_configs.state.
type ConfigState string

const (
	// ConfigStateActive means one KEK wraps every DEK.
	ConfigStateActive ConfigState = "active"

	// ConfigStateSwitching means DEKs are wrapped by both the current and the
	// previous KEK until the switch completes or rolls back.
	ConfigStateSwitching ConfigState = "switching"

	// ConfigStateSuspended means the KEK became unusable; every encrypt and
	// decrypt fails until the health probe or an operator resumes it.
	ConfigStateSuspended ConfigState = "suspended"
)

// DataKeyState is the lifecycle state of a DEK. Stored in
// data_encryption_keys.state.
type DataKeyState string

const (
	// DataKeyStateActive encrypts new values for its use case.
	DataKeyStateActive DataKeyState = "active"

	// DataKeyStateDecryptOnly still decrypts values sealed before a rotation.
	DataKeyStateDecryptOnly DataKeyState = "decrypt_only"

	// DataKeyStateRevoked never decrypts again. No operation revokes a DEK
	// yet; the queries already treat it as unreadable.
	DataKeyStateRevoked DataKeyState = "revoked"
)

const (
	// externalKeyPurposeEncrypt is external_keys.purpose for KEKs.
	externalKeyPurposeEncrypt = "encrypt"

	// externalKeyProviderGcpKms is the only provider KEKs support.
	externalKeyProviderGcpKms = "gcp_kms"
)

var (
	// ErrUnknownUseCase means the use case is not registered.
	ErrUnknownUseCase = errors.New("orgencryption: unknown use case")

	// ErrNotProvisioned means the organization has no configuration or no DEK
	// for the use case yet. Callers on a server or worker path provision with
	// Provisioner.EnsureUseCase.
	ErrNotProvisioned = errors.New("orgencryption: organization encryption is not provisioned for this use case")

	// ErrSuspended means the organization's KEK is unusable, typically because
	// the customer disabled it or removed Speakeasy's access.
	ErrSuspended = errors.New("orgencryption: organization encryption is suspended")

	// ErrKeyUnavailable means a KEK could not unwrap or wrap a DEK. It wraps
	// the provider error, which carries the gRPC status.
	ErrKeyUnavailable = errors.New("orgencryption: key encryption key unavailable")

	// ErrInvalidCiphertext means a value is not an envelope this package
	// produced for the organization, use case and column it was read from.
	ErrInvalidCiphertext = errors.New("orgencryption: ciphertext does not belong to this organization, use case and column")

	// ErrInvalidKey means a key cannot serve as a KEK: wrong purpose or
	// provider, deleted, or missing its credential.
	ErrInvalidKey = errors.New("orgencryption: key cannot be used as a key encryption key")

	// ErrInvalidState means the configuration is not in a state that allows
	// the operation, such as completing a switch that is not in flight.
	ErrInvalidState = errors.New("orgencryption: operation not allowed in the configuration's current state")

	// ErrConcurrentChange means the configuration changed while an operation
	// ran its KMS calls. Retrying is safe.
	ErrConcurrentChange = errors.New("orgencryption: configuration changed concurrently, retry")

	// ErrPlatformCredentialUnusable means the platform credential managed
	// KEKs depend on is missing or misconfigured.
	ErrPlatformCredentialUnusable = errors.New("orgencryption: platform encryption credential unusable")

	// ErrTransitionNotAllowed means a transition does not start from the use
	// case's current transition state, such as rolling back from org_kms.
	ErrTransitionNotAllowed = errors.New("orgencryption: transition not allowed from the current transition state")

	// ErrFinalizeBlocked means a finalize precondition does not hold yet. The
	// wrapped message says which one.
	ErrFinalizeBlocked = errors.New("orgencryption: finalize preconditions not met")

	// ErrFenced means a write was refused because the transition state moved
	// to a newer generation after the writer decided. The writer decides
	// again and seals the same plaintext again; it never repeats a call to a
	// provider.
	ErrFenced = errors.New("orgencryption: transition state changed since the decision, decide again")

	// ErrDecisionExpired means a Decision was held longer than MaxDecisionHold
	// before sealing. The writer decides again and seals the same plaintext.
	ErrDecisionExpired = errors.New("orgencryption: transition decision expired, decide again")

	// ErrOrganizationEncryptionUnavailable means the use case is past
	// platform_key but this process has no organization encryption, so it
	// can neither seal nor open the organization-key copy.
	ErrOrganizationEncryptionUnavailable = errors.New("orgencryption: organization encryption is not configured in this process")

	// ErrUnreadable means a stored value does not decrypt: it is corrupt,
	// sealed for another organization, use case or column, or only its
	// platform-key copy remains after the use case stopped reading it.
	// Retrying cannot help. Every other error from Cipher.Open, such as
	// ErrKeyUnavailable or ErrSuspended, may clear on its own.
	ErrUnreadable = errors.New("orgencryption: stored value does not decrypt")

	// ErrMissing means the row holds no copy of the value.
	ErrMissing = errors.New("orgencryption: stored value is not set")
)

// keyProblems are the errors that mean an organization key could not be used:
// an unreachable, revoked, suspended, disabled or misconfigured key, a process
// without organization encryption serving an enrolled organization, or
// transition state that kept moving under a write.
var keyProblems = []error{
	ErrKeyUnavailable,
	ErrSuspended,
	ErrNotProvisioned,
	ErrInvalidKey,
	ErrInvalidState,
	ErrPlatformCredentialUnusable,
	ErrOrganizationEncryptionUnavailable,
	ErrConcurrentChange,
	ErrFenced,
	ErrDecisionExpired,
}

// IsUnreadable reports whether err from opening a value means the stored
// value itself is missing or does not decrypt, so retrying cannot help and
// the value must be replaced. Every other error from opening one, including
// key problems and database failures, may clear on its own: callers treat it
// as retryable and never as a reason to replace the value or sign in again.
func IsUnreadable(err error) bool {
	return errors.Is(err, ErrUnreadable) || errors.Is(err, ErrMissing)
}

// IsKeyProblem reports whether err from opening, sealing or storing a value,
// or from a usability check, is a problem with the organization key rather
// than with the value or the database. Callers use it to say so in their
// messages; deciding whether an error is retryable is IsUnreadable's job.
func IsKeyProblem(err error) bool {
	for _, problem := range keyProblems {
		if errors.Is(err, problem) {
			return true
		}
	}

	return false
}

// requireUsableState fails closed unless a configuration in state may encrypt
// and decrypt: active and switching may, suspended may not, and any other
// value is refused rather than trusted.
func requireUsableState(state string) error {
	switch ConfigState(state) {
	case ConfigStateActive, ConfigStateSwitching:
		return nil
	case ConfigStateSuspended:
		return ErrSuspended
	default:
		return fmt.Errorf("%w: unknown configuration state %q", ErrInvalidState, state)
	}
}

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
)

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

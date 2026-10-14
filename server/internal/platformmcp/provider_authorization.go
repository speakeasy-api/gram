package platformmcp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ProviderAuthorizationIdentityChaining is the absence recorded when a usable
// chained credential, not an interactive session, authorizes the caller.
const ProviderAuthorizationIdentityChaining = "identity_chaining"

const (
	providerAuthorizationFingerprintDomain = "platform-mcp-provider-authorization-v1"
	assistantReadinessFingerprintDomain    = "platform-mcp-assistant-readiness-v1"
)

// ProviderAuthorizationClientCredential is the Absence of an identity whose
// authorization is the credential a self remote session client holds for
// itself. No subject grant exists, so the client and its issuer identify it,
// and a replaced credential is the same authorization.
const ProviderAuthorizationClientCredential = "client_credential"

// ProviderAuthorizationAbsence is the Absence for a resolved authorization:
// none for a subject's grant, ProviderAuthorizationClientCredential for a self
// client's own credential.
func ProviderAuthorizationAbsence(authorization remotesessions.ResolvedAuthorization) string {
	if authorization.CredentialOwner == remotesessions.CredentialOwnerSelf {
		return ProviderAuthorizationClientCredential
	}
	return ""
}

// ClientCredentialReadiness classifies a failure to obtain a self client's
// own credential. Nothing the member authorizes repairs it, so it never asks
// the member to authorize: an outage is unreachable, anything else needs an
// administrator. ok is false for any other error.
func ClientCredentialReadiness(err error) (state ReadinessState, evidence string, ok bool) {
	switch {
	case errors.Is(err, remotesessions.ErrClientCredentialUnavailable):
		return ReadinessUnreachable, "upstream_client_credential_unavailable", true
	case errors.Is(err, remotesessions.ErrClientCredentialMisconfigured):
		return ReadinessNeedsConfiguration, "upstream_client_credential_misconfigured", true
	default:
		return "", "", false
	}
}

// ClientCredentialProbeReadiness keeps a probe the upstream refused from
// asking the member to sign in when the refused credential is a self
// client's own: only an administrator repairs it. Any other result passes
// through.
func ClientCredentialProbeReadiness(authorization remotesessions.ResolvedAuthorization, state ReadinessState, evidence string) (ReadinessState, string) {
	if authorization.CredentialOwner == remotesessions.CredentialOwnerSelf && state == ReadinessUnauthorized {
		return ReadinessNeedsConfiguration, "upstream_client_credential_rejected"
	}
	return state, evidence
}

// ProviderAuthorizationIdentity contains the durable, non-secret identity of
// the shared provider authorization used for one Platform MCP registration.
type ProviderAuthorizationIdentity struct {
	OrganizationID         string
	Subject                urn.SessionSubject
	RegistrationID         uuid.UUID
	RemoteSessionID        uuid.UUID
	RemoteSessionUpdatedAt time.Time
	RemoteSessionClientID  uuid.UUID
	RemoteSessionIssuerID  uuid.UUID
	Absence                string
}

// assistantReadinessFingerprint scopes a provider authorization fingerprint to
// the connectionless assistant actor that observed it. The legacy readiness
// unique index treats all NULL connections as identical, so this additional
// opaque key prevents cross-user collisions until that index can be retired.
func assistantReadinessFingerprint(providerFingerprint, userID string, surface ActingSurface) string {
	payload := assistantReadinessFingerprintDomain + "\x00" + providerFingerprint + "\x00" + userID + "\x00" + string(surface)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

// ProviderAuthorizationFingerprint returns an opaque value suitable for
// readiness persistence. It intentionally excludes access and refresh tokens.
func ProviderAuthorizationFingerprint(identity ProviderAuthorizationIdentity) (string, error) {
	if identity.OrganizationID == "" || identity.Subject.IsZero() || identity.RegistrationID == uuid.Nil {
		return "", ErrReadinessInvalid
	}

	payload := providerAuthorizationFingerprintDomain + "\x00" +
		identity.OrganizationID + "\x00" +
		identity.Subject.String() + "\x00" +
		identity.RegistrationID.String() + "\x00"
	switch identity.Absence {
	case "":
		if identity.RemoteSessionID == uuid.Nil || identity.RemoteSessionUpdatedAt.IsZero() || identity.RemoteSessionClientID == uuid.Nil || identity.RemoteSessionIssuerID == uuid.Nil {
			return "", ErrReadinessInvalid
		}
		payload += "active_session\x00" +
			identity.RemoteSessionID.String() + "\x00" +
			identity.RemoteSessionUpdatedAt.UTC().Format(time.RFC3339Nano) + "\x00" +
			identity.RemoteSessionClientID.String() + "\x00" +
			identity.RemoteSessionIssuerID.String()
	case ProviderAuthorizationClientCredential:
		if identity.RemoteSessionID != uuid.Nil || !identity.RemoteSessionUpdatedAt.IsZero() || identity.RemoteSessionClientID == uuid.Nil || identity.RemoteSessionIssuerID == uuid.Nil {
			return "", ErrReadinessInvalid
		}
		payload += ProviderAuthorizationClientCredential + "\x00" +
			identity.RemoteSessionClientID.String() + "\x00" +
			identity.RemoteSessionIssuerID.String()
	case "no_client", "no_session", "anonymous", ProviderAuthorizationIdentityChaining:
		if identity.RemoteSessionID != uuid.Nil || !identity.RemoteSessionUpdatedAt.IsZero() || identity.RemoteSessionClientID != uuid.Nil {
			return "", ErrReadinessInvalid
		}
		if (identity.Absence == "no_session" || identity.Absence == ProviderAuthorizationIdentityChaining) && identity.RemoteSessionIssuerID == uuid.Nil {
			return "", ErrReadinessInvalid
		}
		issuer := "no_issuer"
		if identity.RemoteSessionIssuerID != uuid.Nil {
			issuer = identity.RemoteSessionIssuerID.String()
		}
		payload += identity.Absence + "\x00" + issuer
	default:
		return "", ErrReadinessInvalid
	}
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), nil
}

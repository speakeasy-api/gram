package platformmcp

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

const providerAuthorizationFingerprintDomain = "platform-mcp-provider-authorization-v1"

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
	case "no_client", "no_session", "anonymous":
		if identity.RemoteSessionID != uuid.Nil || !identity.RemoteSessionUpdatedAt.IsZero() || identity.RemoteSessionClientID != uuid.Nil {
			return "", ErrReadinessInvalid
		}
		if identity.Absence == "no_session" && identity.RemoteSessionIssuerID == uuid.Nil {
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

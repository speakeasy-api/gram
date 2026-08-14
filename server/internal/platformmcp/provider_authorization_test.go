package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestProviderAuthorizationFingerprintUsesOnlyDurableIdentity(t *testing.T) {
	t.Parallel()

	identity := ProviderAuthorizationIdentity{
		OrganizationID:         "organization",
		Subject:                urn.NewUserSubject("user"),
		RegistrationID:         uuid.New(),
		RemoteSessionID:        uuid.New(),
		RemoteSessionUpdatedAt: time.Now().UTC(),
		RemoteSessionClientID:  uuid.New(),
		RemoteSessionIssuerID:  uuid.New(),
	}

	first, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	second, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, first, 64)

	identity.RemoteSessionUpdatedAt = identity.RemoteSessionUpdatedAt.Add(time.Second)
	changed, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	require.NotEqual(t, first, changed, "token refresh or reauthorization updates the durable session timestamp")
}

func TestProviderAuthorizationFingerprintUsesDistinctAbsenceDomains(t *testing.T) {
	t.Parallel()

	identity := ProviderAuthorizationIdentity{
		OrganizationID:        "organization",
		Subject:               urn.NewUserSubject("user"),
		RegistrationID:        uuid.New(),
		RemoteSessionIssuerID: uuid.New(),
		Absence:               "no_client",
	}
	noClient, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)

	identity.Absence = "no_session"
	noSession, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	require.NotEqual(t, noClient, noSession)
}

func TestProviderAuthorizationFingerprintRejectsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	_, err := ProviderAuthorizationFingerprint(ProviderAuthorizationIdentity{})
	require.ErrorIs(t, err, ErrReadinessInvalid)
}

func TestProviderAuthorizationFingerprintRejectsActiveSessionWithoutIssuer(t *testing.T) {
	t.Parallel()

	_, err := ProviderAuthorizationFingerprint(ProviderAuthorizationIdentity{
		OrganizationID:         "organization",
		Subject:                urn.NewUserSubject("user"),
		RegistrationID:         uuid.New(),
		RemoteSessionID:        uuid.New(),
		RemoteSessionUpdatedAt: time.Now().UTC(),
		RemoteSessionClientID:  uuid.New(),
	})

	require.ErrorIs(t, err, ErrReadinessInvalid)
}

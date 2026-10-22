package platformmcp

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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

func TestAssistantReadinessFingerprintBindsTheActor(t *testing.T) {
	t.Parallel()

	providerFingerprint := "provider-fingerprint"
	first := assistantReadinessFingerprint(providerFingerprint, "user-one", SurfaceProjectAssistant)

	require.Len(t, first, 64)
	require.Equal(t, first, assistantReadinessFingerprint(providerFingerprint, "user-one", SurfaceProjectAssistant))
	require.NotEqual(t, first, assistantReadinessFingerprint(providerFingerprint, "user-two", SurfaceProjectAssistant))
	require.NotEqual(t, first, assistantReadinessFingerprint(providerFingerprint, "user-one", SurfaceDashboard))
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

func TestProviderAuthorizationFingerprintIdentifiesClientCredentialByClient(t *testing.T) {
	t.Parallel()

	identity := ProviderAuthorizationIdentity{
		OrganizationID:         "organization",
		Subject:                urn.NewUserSubject("user"),
		RegistrationID:         uuid.New(),
		RemoteSessionID:        uuid.Nil,
		RemoteSessionUpdatedAt: time.Time{},
		RemoteSessionClientID:  uuid.New(),
		RemoteSessionIssuerID:  uuid.New(),
		Absence:                ProviderAuthorizationClientCredential,
	}
	first, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	again, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	require.Equal(t, first, again, "a replaced client credential is the same authorization")

	identity.RemoteSessionClientID = uuid.New()
	otherClient, err := ProviderAuthorizationFingerprint(identity)
	require.NoError(t, err)
	require.NotEqual(t, first, otherClient)

	noClient := identity
	noClient.RemoteSessionClientID = uuid.Nil
	_, err = ProviderAuthorizationFingerprint(noClient)
	require.ErrorIs(t, err, ErrReadinessInvalid)

	withSession := identity
	withSession.RemoteSessionID = uuid.New()
	_, err = ProviderAuthorizationFingerprint(withSession)
	require.ErrorIs(t, err, ErrReadinessInvalid)
}

func TestProviderAuthorizationAbsenceNamesClientCredential(t *testing.T) {
	t.Parallel()

	require.Equal(t, ProviderAuthorizationClientCredential, ProviderAuthorizationAbsence(remotesessions.ResolvedAuthorization{CredentialOwner: remotesessions.CredentialOwnerSelf}))
	require.Empty(t, ProviderAuthorizationAbsence(remotesessions.ResolvedAuthorization{CredentialOwner: remotesessions.CredentialOwnerSubject}))
}

func TestClientCredentialProbeReadinessNeverAsksForSignIn(t *testing.T) {
	t.Parallel()

	self := remotesessions.ResolvedAuthorization{CredentialOwner: remotesessions.CredentialOwnerSelf}
	state, evidence := ClientCredentialProbeReadiness(self, ReadinessUnauthorized, "upstream_authorization_rejected")
	require.Equal(t, ReadinessNeedsConfiguration, state)
	require.Equal(t, "upstream_client_credential_rejected", evidence)

	state, evidence = ClientCredentialProbeReadiness(self, ReadinessReady, "tools_list_ok")
	require.Equal(t, ReadinessReady, state)
	require.Equal(t, "tools_list_ok", evidence)

	subject := remotesessions.ResolvedAuthorization{CredentialOwner: remotesessions.CredentialOwnerSubject}
	state, evidence = ClientCredentialProbeReadiness(subject, ReadinessUnauthorized, "upstream_authorization_rejected")
	require.Equal(t, ReadinessUnauthorized, state, "a subject's rejected grant is theirs to reauthorize")
	require.Equal(t, "upstream_authorization_rejected", evidence)
}

func TestClientCredentialReadinessNeverAsksForGramAuthorization(t *testing.T) {
	t.Parallel()

	state, evidence, ok := ClientCredentialReadiness(fmt.Errorf("resolve: %w", remotesessions.ErrClientCredentialMisconfigured))
	require.True(t, ok)
	require.Equal(t, ReadinessNeedsConfiguration, state)
	require.Equal(t, "upstream_client_credential_misconfigured", evidence)

	state, evidence, ok = ClientCredentialReadiness(fmt.Errorf("resolve: %w", remotesessions.ErrClientCredentialUnavailable))
	require.True(t, ok)
	require.Equal(t, ReadinessUnreachable, state)
	require.Equal(t, "upstream_client_credential_unavailable", evidence)

	for _, err := range []error{remotesessions.ErrNoValidToken, remotesessions.ErrRemoteSessionMisconfigured, remotesessions.ErrRemoteSessionUnavailable, errors.New("other")} {
		_, _, ok = ClientCredentialReadiness(err)
		require.False(t, ok, "a subject's failure keeps its own classification: %v", err)
	}
}

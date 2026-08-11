package platformmcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestProviderAdaptersOnlyResolveConfiguredProvider(t *testing.T) {
	t.Parallel()

	adapters := NewProviderAdapters([]ProviderAdapter{deterministicProviderAdapter{providerKey: "fixture"}})
	adapter, err := adapters.Get("fixture")
	require.NoError(t, err)
	require.Equal(t, "fixture", adapter.ProviderKey())

	_, err = adapters.Get("unknown")
	require.ErrorIs(t, err, ErrProviderAdapterUnavailable)
}

func TestDeterministicProviderAdapterRejectsConfiguredSetupFailures(t *testing.T) {
	t.Parallel()

	preflightErr := fmt.Errorf("preflight unavailable")
	beginErr := fmt.Errorf("provider setup unavailable")
	request := ProviderSetupRequest{
		UserID:              "user",
		OrganizationID:      "organization",
		ProjectID:           uuid.New(),
		RegistrationID:      uuid.New(),
		UserSessionIssuerID: uuid.New(),
		MCPSlug:             "project-provider",
		ConnectionID:        uuid.New(),
		Generation:          uuid.New(),
	}
	adapter := deterministicProviderAdapter{providerKey: "fixture", preflightErr: preflightErr}
	require.ErrorIs(t, adapter.PreflightSetup(t.Context(), request), preflightErr)

	request.HandoffID = uuid.New()
	adapter = deterministicProviderAdapter{providerKey: "fixture", beginErr: beginErr}
	_, err := adapter.BeginSetup(t.Context(), request)
	require.ErrorIs(t, err, beginErr)
}

func TestProviderSetupResultRequiresTransientAuthorizationURL(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, validateProviderSetupResult(ProviderSetupResult{}), ErrSetupHandoffInvalid)
	require.ErrorIs(t, validateProviderSetupResult(ProviderSetupResult{AuthorizationURL: "http://provider.test/authorize"}), ErrSetupHandoffInvalid)
	require.ErrorIs(t, validateProviderSetupResult(ProviderSetupResult{AuthorizationURL: "https://user:password@provider.test/authorize"}), ErrSetupHandoffInvalid)
	require.ErrorIs(t, validateProviderSetupResult(ProviderSetupResult{AuthorizationURL: "https://:443/authorize"}), ErrSetupHandoffInvalid)
	require.ErrorIs(t, validateProviderSetupResult(ProviderSetupResult{AuthorizationURL: "javascript:alert(1)"}), ErrSetupHandoffInvalid)
	require.NoError(t, validateProviderSetupResult(ProviderSetupResult{AuthorizationURL: "https://provider.test/authorize?state=opaque"}))
}

func TestDeterministicProviderAdapterNormalizesAuthenticatedReadiness(t *testing.T) {
	t.Parallel()

	adapter := deterministicProviderAdapter{providerKey: "fixture"}
	setup, err := adapter.BeginSetup(t.Context(), ProviderSetupRequest{
		UserID:              "user",
		OrganizationID:      "organization",
		ProjectID:           uuid.New(),
		RegistrationID:      uuid.New(),
		UserSessionIssuerID: uuid.New(),
		MCPSlug:             "project-provider",
		ConnectionID:        uuid.New(),
		Generation:          uuid.New(),
		HandoffID:           uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, "https://provider.test/authorize", setup.AuthorizationURL)

	result, err := adapter.ProbeReadiness(t.Context(), ProviderReadinessProbeRequest{
		UserID:              "user",
		OrganizationID:      "organization",
		ProjectID:           uuid.New(),
		RegistrationID:      uuid.New(),
		UserSessionIssuerID: uuid.New(),
		ConnectionID:        uuid.New(),
		Generation:          uuid.New(),
	})
	require.NoError(t, err)
	require.NoError(t, validateProviderReadinessProbeResult(result))
	require.Equal(t, ReadinessReady, result.State)
	require.Equal(t, "authenticated_initialize_tools_list", result.EvidenceCode)
}

type deterministicProviderAdapter struct {
	providerKey  string
	preflightErr error
	beginErr     error
}

func (a deterministicProviderAdapter) ProviderKey() string {
	return a.providerKey
}

func (a deterministicProviderAdapter) PreflightSetup(_ context.Context, request ProviderSetupRequest) error {
	if err := validateProviderSetupPreflightRequest(request); err != nil {
		return err
	}
	return a.preflightErr
}

func (a deterministicProviderAdapter) BeginSetup(_ context.Context, request ProviderSetupRequest) (ProviderSetupResult, error) {
	if err := validateProviderSetupRequest(request); err != nil {
		return ProviderSetupResult{}, err
	}
	if a.beginErr != nil {
		return ProviderSetupResult{}, a.beginErr
	}
	return ProviderSetupResult{AuthorizationURL: "https://provider.test/authorize"}, nil
}

func (a deterministicProviderAdapter) ProbeReadiness(_ context.Context, request ProviderReadinessProbeRequest) (ProviderReadinessProbeResult, error) {
	if err := validateProviderReadinessProbeRequest(request); err != nil {
		return ProviderReadinessProbeResult{}, err
	}
	now := time.Now().UTC()
	return ProviderReadinessProbeResult{
		AuthorizationIdentity: ProviderAuthorizationIdentity{
			OrganizationID:         request.OrganizationID,
			Subject:                urn.NewUserSubject(request.UserID),
			RegistrationID:         request.RegistrationID,
			RemoteSessionID:        uuid.New(),
			RemoteSessionUpdatedAt: now,
			RemoteSessionClientID:  uuid.New(),
			RemoteSessionIssuerID:  uuid.New(),
		},
		State:        ReadinessReady,
		EvidenceCode: "authenticated_initialize_tools_list",
		CheckedAt:    now,
		ExpiresAt:    now.Add(time.Minute),
	}, nil
}

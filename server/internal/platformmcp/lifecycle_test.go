package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestSetupHandoffValue(t *testing.T) {
	t.Parallel()

	first, err := newSetupHandoffValue()
	require.NoError(t, err)
	second, err := newSetupHandoffValue()
	require.NoError(t, err)

	require.NotEmpty(t, first)
	require.NotEqual(t, first, second)
	require.Len(t, setupHandoffHash(first), 64)
}

func TestValidateSetupHandoffBinding(t *testing.T) {
	t.Parallel()

	binding := SetupHandoffBinding{
		ProjectID:        uuid.New(),
		RegistrationID:   uuid.New(),
		ProviderKey:      "registry",
		CatalogReference: "reviewed/mcp",
		Intent:           "authorize",
	}
	require.NoError(t, validateSetupHandoffBinding("organization", binding))

	binding.Intent = ""
	require.ErrorIs(t, validateSetupHandoffBinding("organization", binding), ErrSetupHandoffInvalid)
}

func TestIsSetupMilestone(t *testing.T) {
	t.Parallel()

	for _, milestone := range []string{"provider_setup_started", "provider_setup_failed", "provider_setup_succeeded", "platform_flow_ready"} {
		require.True(t, isSetupMilestone(milestone), milestone)
	}
	require.False(t, isSetupMilestone("registration_succeeded"))
	require.False(t, isSetupMilestone(""))
}

func TestValidateReadinessAcceptsOnlyRFCStates(t *testing.T) {
	t.Parallel()

	binding := ReadinessBinding{
		ProjectID:                        uuid.New(),
		RegistrationID:                   uuid.New(),
		ProviderAuthorizationFingerprint: "opaque-fingerprint",
	}
	for _, state := range []ReadinessState{
		ReadinessReady,
		ReadinessNeedsProviderSetup,
		ReadinessNeedsGramAuthorization,
		ReadinessNeedsConfiguration,
		ReadinessAuthFailed,
		ReadinessUnreachable,
		ReadinessUnsupported,
		ReadinessUnauthorized,
		ReadinessGuideUnavailable,
		ReadinessDegraded,
	} {
		now := time.Now()
		require.NoError(t, validateReadiness("organization", binding, state, now, now.Add(time.Minute)))
	}
	now := time.Now()
	require.ErrorIs(t, validateReadiness("organization", binding, "unknown", now, now.Add(time.Minute)), ErrReadinessInvalid)
	require.ErrorIs(t, validateReadiness("organization", binding, ReadinessReady, now, time.Time{}), ErrReadinessInvalid)
	require.ErrorIs(t, validateReadiness("organization", binding, ReadinessReady, now, now), ErrReadinessInvalid)
}

func TestReadinessFromRowTreatsExpiryAsFreshnessNotState(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	row := platformrepo.PlatformMcpReadiness{
		ID:             uuid.New(),
		ProjectID:      uuid.New(),
		RegistrationID: uuid.New(),
		State:          string(ReadinessReady),
		CheckedAt:      pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true},
		ExpiresAt:      pgtype.Timestamptz{Time: now.Add(-time.Second), Valid: true},
	}

	readiness := readinessFromRow(row, now)
	require.Equal(t, ReadinessReady, readiness.State)
	require.False(t, readiness.Fresh)
}

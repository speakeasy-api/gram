package remotesessions

import (
	"encoding/json"
	orgclientshttp "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_clients/server"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestDelegationStatusCountsSanitized(t *testing.T) {
	t.Parallel()
	rows := []repo.CountTrustedDelegationObservationsRow{
		{ObservationStatus: "refused", ObservationCount: 2},
		{ObservationStatus: "raw upstream token response", ObservationCount: 1},
		{ObservationStatus: "assertion_only", ObservationCount: 3},
		{ObservationStatus: "", ObservationCount: 4},
		{ObservationStatus: "configuration_failure", ObservationCount: 0},
	}
	got := delegationStatusCounts(rows)
	require.Len(t, got, 2)
	require.Equal(t, "assertion_only", got[0].Status)
	require.Equal(t, int64(3), got[0].Count)
	require.Equal(t, "refused", got[1].Status)
	wire, err := json.Marshal(orgclientshttp.NewGetClientDelegationStatusResponseBody(&orgclientsgen.OrganizationClientDelegationStatus{Observations: got}))
	require.NoError(t, err)
	require.NotContains(t, string(wire), "raw upstream")
	require.NotNil(t, delegationStatusCounts(nil))
	require.Empty(t, delegationStatusCounts(nil))
}

func TestDelegationStatusTimestamps(t *testing.T) {
	t.Parallel()
	obtained := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	renewed := obtained.Add(time.Hour)
	observed := renewed.Add(time.Minute)
	got := delegationStatusCounts([]repo.CountTrustedDelegationObservationsRow{{
		ObservationStatus: "durable_credential_present", ObservationCount: 1,
		LastObservedAt:           pgtype.Timestamptz{Time: observed, Valid: true},
		LastCredentialObtainedAt: pgtype.Timestamptz{Time: obtained, Valid: true},
		LastRefreshSucceededAt:   pgtype.Timestamptz{Time: renewed, Valid: true},
	}})
	require.Len(t, got, 1)
	require.Equal(t, observed.Format(time.RFC3339Nano), *got[0].LastObservedAt)
	require.Equal(t, obtained.Format(time.RFC3339Nano), *got[0].LastCredentialObtainedAt)
	require.Equal(t, renewed.Format(time.RFC3339Nano), *got[0].LastRefreshSucceededAt)
	require.Nil(t, delegationStatusTimestamp(pgtype.Timestamptz{}))
	require.Nil(t, delegationStatusTimestamp(pgtype.Timestamptz{Valid: true, InfinityModifier: pgtype.Infinity}))
}

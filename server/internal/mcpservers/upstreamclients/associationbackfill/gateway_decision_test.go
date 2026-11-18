package associationbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDecideGatewayExistingOutboundAssociations(t *testing.T) {
	t.Parallel()
	issuerID, serverID, projectID := uuid.New(), uuid.New(), uuid.New()
	row := func(grant, derived, live, recorded bool) ListGatewayAssociationCandidatesRow {
		return ListGatewayAssociationCandidatesRow{
			IssuerID: issuerID, McpServerID: serverID, ProjectID: projectID,
			RemoteSessionClientID: uuid.New(),
			GrantMatch:            grant, DerivedMatch: derived, PairLive: live, PairRecorded: recorded,
		}
	}
	claimed := func(r ListGatewayAssociationCandidatesRow) ListGatewayAssociationCandidatesRow {
		r.ClaimedBySibling = true
		return r
	}
	tests := []struct {
		name    string
		rows    []ListGatewayAssociationCandidatesRow
		outcome GatewayOutcome
	}{
		{
			name:    "multiple associated registrations share outbound authorization server",
			rows:    []ListGatewayAssociationCandidatesRow{row(false, true, true, true), row(false, true, true, true)},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "multiple grant matches all associated",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, true, true), row(true, true, true, true)},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "detached grant competitor stays detached",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, true, true), row(false, true, true, true), row(true, true, false, true)},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "unrecorded grant competitor still blocks multiple associations",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, true, true), row(false, true, true, true), row(true, true, false, false)},
			outcome: GatewayOutcomeAmbiguous,
		},
		{
			name:    "conflicting unrecorded grant still blocks",
			rows:    []ListGatewayAssociationCandidatesRow{row(false, true, true, true), row(false, true, true, true), row(true, false, false, false)},
			outcome: GatewayOutcomeConflict,
		},
		{
			name:    "grant competitor associated with a sibling on the same upstream need not be added",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, true, true), claimed(row(true, true, false, false))},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "conflicting grant of a sibling's client need not be added",
			rows:    []ListGatewayAssociationCandidatesRow{row(false, true, true, true), claimed(row(true, false, false, false))},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "sibling's grant competitor does not excuse an unclaimed one",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, true, true), claimed(row(true, true, false, false)), row(true, true, false, false)},
			outcome: GatewayOutcomeAmbiguous,
		},
		{
			name:    "sibling's grant competitors alone leave no association to settle by",
			rows:    []ListGatewayAssociationCandidatesRow{claimed(row(true, true, false, false)), claimed(row(true, true, false, false))},
			outcome: GatewayOutcomeAmbiguous,
		},
		{
			name:    "derived only unassociated competitor need not be added",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, true, true), row(false, true, true, true), row(false, true, false, false)},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "existing outbound association outweighs derived only candidates",
			rows:    []ListGatewayAssociationCandidatesRow{row(false, false, true, true), row(false, true, false, false), row(false, true, false, false)},
			outcome: GatewayOutcomeConfigured,
		},
		{
			name:    "inbound bindings without outbound associations remain ambiguous",
			rows:    []ListGatewayAssociationCandidatesRow{row(false, true, false, false), row(false, true, false, false)},
			outcome: GatewayOutcomeAmbiguous,
		},
		{
			name:    "only detached associations remain ambiguous",
			rows:    []ListGatewayAssociationCandidatesRow{row(true, true, false, true), row(true, true, false, true)},
			outcome: GatewayOutcomeAmbiguous,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := DecideGateway(tt.rows)
			require.Equal(t, tt.outcome, result.Outcome)
			require.Equal(t, tt.outcome == GatewayOutcomeConfigured, result.Outcome.Covered())
			require.False(t, result.Associate.Valid, "existing configuration never adds an outbound client")
		})
	}
}

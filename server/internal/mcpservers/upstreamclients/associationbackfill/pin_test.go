package associationbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// pinCandidate is one candidate row for a server; options adjust it.
type pinCandidate func(*ListUpstreamClientBackfillCandidatesRow)

var (
	pinServerID  = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	pinClientA   = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
	pinClientB   = uuid.MustParse("00000000-0000-0000-0000-0000000000c2")
	pinProviderX = uuid.MustParse("00000000-0000-0000-0000-0000000000f1")
	pinProviderY = uuid.MustParse("00000000-0000-0000-0000-0000000000f2")
)

// pinRow builds a candidate row for a proxied server that is its issuer's
// only consumer, bound to client with provider, then applies options.
func pinRow(client, provider uuid.UUID, options ...pinCandidate) ListUpstreamClientBackfillCandidatesRow {
	row := ListUpstreamClientBackfillCandidatesRow{
		McpServerID:             pinServerID,
		ProjectID:               uuid.Nil,
		OrganizationID:          "",
		UserSessionIssuerID:     uuid.Nil,
		RemoteBacked:            true,
		Hosted:                  false,
		Proxied:                 true,
		IssuerServerCount:       1,
		IssuerHasGateway:        false,
		LiveAssociations:        0,
		OffersOwnOauthClient:    false,
		UpstreamAuthNotRequired: false,
		RemoteSessionClientID:   uuid.NullUUID{UUID: client, Valid: client != uuid.Nil},
		ClientProviderID:        uuid.NullUUID{UUID: provider, Valid: provider != uuid.Nil},
		MetadataMatch:           false,
		GrantMatch:              false,
		DerivedMatch:            false,
		PairLive:                false,
		PairRecorded:            false,
	}
	for _, option := range options {
		option(&row)
	}
	return row
}

func shared(row *ListUpstreamClientBackfillCandidatesRow)        { row.IssuerServerCount = 2 }
func hosted(row *ListUpstreamClientBackfillCandidatesRow)        { row.Proxied, row.Hosted = false, true }
func metadataMatch(row *ListUpstreamClientBackfillCandidatesRow) { row.MetadataMatch = true }
func grantMatch(row *ListUpstreamClientBackfillCandidatesRow)    { row.GrantMatch = true }
func derivedMatch(row *ListUpstreamClientBackfillCandidatesRow)  { row.DerivedMatch = true }
func detached(row *ListUpstreamClientBackfillCandidatesRow)      { row.PairRecorded = true }
func associated(row *ListUpstreamClientBackfillCandidatesRow)    { row.LiveAssociations = 1 }
func noAuth(row *ListUpstreamClientBackfillCandidatesRow)        { row.UpstreamAuthNotRequired = true }
func ownClient(row *ListUpstreamClientBackfillCandidatesRow)     { row.OffersOwnOauthClient = true }

func TestDecidePin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rows      []ListUpstreamClientBackfillCandidatesRow
		outcome   PinOutcome
		associate []uuid.UUID
	}{
		{
			name:    "no upstream auth is never pinned",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, noAuth)},
			outcome: PinOutcomeNoAuth,
		},
		{
			name:    "no bound client",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(uuid.Nil, uuid.Nil)},
			outcome: PinOutcomeUnbound,
		},
		{
			name:    "a server with an association is left to it",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, associated, metadataMatch)},
			outcome: PinOutcomeConfigured,
		},
		{
			name:    "a server offering its own client is left to it",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, ownClient)},
			outcome: PinOutcomeConfigured,
		},
		{
			name:      "a sole consumer takes its bound clients",
			rows:      []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX)},
			outcome:   PinOutcomeSoleConsumer,
			associate: []uuid.UUID{pinClientA},
		},
		{
			name:      "a sole consumer keeps an explicit detach",
			rows:      []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, detached), pinRow(pinClientB, pinProviderX)},
			outcome:   PinOutcomeSoleConsumer,
			associate: []uuid.UUID{pinClientB},
		},
		{
			name:    "a sole consumer whose only client was detached",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, detached)},
			outcome: PinOutcomeNoEvidence,
		},
		{
			name:      "a hosted sole consumer takes clients of several providers",
			rows:      []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, hosted), pinRow(pinClientB, pinProviderY, hosted)},
			outcome:   PinOutcomeSoleConsumer,
			associate: []uuid.UUID{pinClientA, pinClientB},
		},
		{
			name:      "a proxied sole consumer spanning providers is decided by metadata",
			rows:      []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX), pinRow(pinClientB, pinProviderY, metadataMatch)},
			outcome:   PinOutcomeMetadataMatch,
			associate: []uuid.UUID{pinClientB},
		},
		{
			name:    "a proxied sole consumer spanning providers without evidence",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX), pinRow(pinClientB, pinProviderY)},
			outcome: PinOutcomeNoEvidence,
		},
		{
			name:      "a shared issuer decided by metadata",
			rows:      []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, metadataMatch), pinRow(pinClientB, pinProviderY, shared)},
			outcome:   PinOutcomeMetadataMatch,
			associate: []uuid.UUID{pinClientA},
		},
		{
			name:    "a shared issuer whose metadata names several clients",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, metadataMatch), pinRow(pinClientB, pinProviderX, shared, metadataMatch)},
			outcome: PinOutcomeAmbiguous,
		},
		{
			name:    "a shared issuer keeps a detach of the metadata match",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, metadataMatch, detached)},
			outcome: PinOutcomeNoEvidence,
		},
		{
			name:      "a shared issuer decided by grants",
			rows:      []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, grantMatch), pinRow(pinClientB, pinProviderY, shared)},
			outcome:   PinOutcomeGrant,
			associate: []uuid.UUID{pinClientA},
		},
		{
			name:    "a shared issuer with grants for several clients",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, grantMatch), pinRow(pinClientB, pinProviderY, shared, grantMatch)},
			outcome: PinOutcomeAmbiguous,
		},
		{
			name:    "the derived authorization server never decides",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, derivedMatch), pinRow(pinClientB, pinProviderY, shared)},
			outcome: PinOutcomeNoEvidence,
		},
		{
			name:    "a hosted server on a shared issuer",
			rows:    []ListUpstreamClientBackfillCandidatesRow{pinRow(pinClientA, pinProviderX, shared, hosted, grantMatch)},
			outcome: PinOutcomeNoEvidence,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision := DecidePin(tc.rows)
			require.Equal(t, pinServerID, decision.MCPServerID)
			require.Equal(t, tc.outcome, decision.Outcome)
			require.Equal(t, tc.associate, decision.Associate)
		})
	}
}

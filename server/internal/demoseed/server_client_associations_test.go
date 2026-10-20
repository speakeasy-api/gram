//go:build demoseed_safety

package demoseed

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
)

func TestDemoServerClientAssociations(t *testing.T) {
	t.Parallel()
	testServerClientAssociations(t, DefaultSpec())
}

func TestLocalServerClientAssociations(t *testing.T) {
	t.Parallel()
	testServerClientAssociations(t, LocalSpec())
}

// Every seeded proxied server whose issuer binds an upstream client carries
// the association it is meant to use, so the backfill has nothing to decide.
// GitHub shares the workforce issuer with the EMA services but uses neither
// of their clients.
func testServerClientAssociations(t *testing.T, spec Spec) {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	det := func(name string) uuid.UUID { return detUUID(spec.NameSeed + name) }
	github := det("mcpserver-github")
	want := map[uuid.UUID][]uuid.UUID{
		det("mcpserver-linear"):      {det("remote-identity-client-linear"), det("attachment-client")},
		det("mcpserver-ema-tracker"): {det("ema-client-tracker")},
		det("mcpserver-ema-wiki"):    {det("ema-client-wiki")},
	}

	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)

		rows, err := associationbackfill.New(db).ListUpstreamClientBackfillCandidates(ctx, associationbackfill.ListUpstreamClientBackfillCandidatesParams{
			Cursor:         uuid.Nil,
			OrganizationID: spec.OrgID,
			PageSize:       1000,
		})
		require.NoError(t, err)
		got := map[uuid.UUID][]uuid.UUID{}
		var uncovered []uuid.UUID
		for _, row := range rows {
			if row.PairLive {
				got[row.McpServerID] = append(got[row.McpServerID], row.RemoteSessionClientID.UUID)
			}
			if row.Proxied && row.RemoteSessionClientID.Valid && row.LiveAssociations == 0 && !slices.Contains(uncovered, row.McpServerID) {
				uncovered = append(uncovered, row.McpServerID)
			}
		}
		require.Len(t, got, len(want))
		for serverID, clients := range want {
			require.ElementsMatch(t, clients, got[serverID], "server %s", serverID)
		}
		require.Equal(t, []uuid.UUID{github}, uncovered, "every proxied server with an issuer-bound client must be associated")

		report, err := associationbackfill.Run(ctx, db, associationbackfill.Options{OrganizationID: spec.OrgID, Apply: false, PageSize: 0})
		require.NoError(t, err)
		require.Zero(t, report.Decided, "the seed must write every association the backfill would decide")
		for _, result := range report.Results {
			if !result.Outcome.Covered() {
				require.Equal(t, github, result.MCPServerID, "unexpected uncovered server")
			}
		}
		for _, result := range report.GatewayResults {
			require.True(t, result.Outcome.Covered(), "gateway server %s is %s", result.MCPServerID, result.Outcome)
		}
	}
}

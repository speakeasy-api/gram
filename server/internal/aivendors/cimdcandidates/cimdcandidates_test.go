package cimdcandidates_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	uncoveredURL       = "https://new-client.example.test/oauth/client.json"
	exactURL           = "https://known.example.test/oauth/client.json"
	patternURL         = "https://connectors.example.test/oauth/tenant-a/client.json"
	disabledExactURL   = "https://retired.example.test/oauth/client.json"
	disabledPatternURL = "https://paused.example.test/oauth/tenant-b/client.json"
)

func testCatalog() *aivendors.Snapshot {
	return aivendors.NewSnapshot([]aivendors.Product{
		{ID: "known", VendorKey: "known", Documents: []aivendors.Document{{URL: exactURL, Enabled: true}}},
		{ID: "connectors", VendorKey: "connectors", Documents: []aivendors.Document{{URL: "https://connectors.example.test/oauth/*/client.json", Enabled: true, Pattern: true}}},
		{ID: "retired", VendorKey: "retired", Documents: []aivendors.Document{{URL: disabledExactURL, Enabled: false}}},
		{ID: "paused", VendorKey: "paused", Documents: []aivendors.Document{{URL: "https://paused.example.test/oauth/*/client.json", Enabled: false, Pattern: true}}},
	})
}

func TestRecordable(t *testing.T) {
	t.Parallel()

	catalog := testCatalog()
	cases := []struct {
		name     string
		mode     admission.Mode
		clientID string
		want     bool
	}{
		{name: "open uncovered", mode: admission.ModeOpen, clientID: uncoveredURL, want: true},
		{name: "open exact", mode: admission.ModeOpen, clientID: exactURL, want: false},
		{name: "open pattern", mode: admission.ModeOpen, clientID: patternURL, want: false},
		{name: "open disabled exact", mode: admission.ModeOpen, clientID: disabledExactURL, want: false},
		{name: "open disabled pattern", mode: admission.ModeOpen, clientID: disabledPatternURL, want: false},
		{name: "open assistant document", mode: admission.ModeOpen, clientID: "https://app.example.test/.well-known/oauth-client/assistants/" + uuid.NewString(), want: false},
		{name: "open assistant path with extra segment", mode: admission.ModeOpen, clientID: "https://app.example.test/.well-known/oauth-client/assistants/" + uuid.NewString() + "/x", want: true},
		{name: "presets uncovered", mode: admission.ModePresets, clientID: uncoveredURL, want: false},
		{name: "reporting uncovered", mode: admission.ModeReporting, clientID: uncoveredURL, want: false},
		{name: "disabled uncovered", mode: admission.ModeDisabled, clientID: uncoveredURL, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, cimdcandidates.Recordable(catalog, tc.mode, tc.clientID))
		})
	}
}

func TestRecorder_NewClientOnceWithSightingPerIssuer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_record")
	first := seedIssuer(t, conn, "first", admission.ModeOpen)
	second := seedIssuer(t, conn, "second", admission.ModeOpen)
	recorder := cimdcandidates.NewRecorder(conn)
	logger := testenv.NewLogger(t)

	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "New Client", UserSessionIssuerID: first.id})
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "New Client", UserSessionIssuerID: first.id})
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "New Client", UserSessionIssuerID: second.id})

	candidates, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, uncoveredURL, candidates[0].Url)
	require.Equal(t, "New Client", candidates[0].ClientName.String)
	require.Equal(t, string(cimdcandidates.StatusPending), candidates[0].Status)

	sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, sightings, 2)
	issuers := []uuid.UUID{sightings[0].UserSessionIssuerID, sightings[1].UserSessionIssuerID}
	require.ElementsMatch(t, []uuid.UUID{first.id, second.id}, issuers)
	for _, s := range sightings {
		require.Equal(t, testOrgID, s.OrganizationID.String, "the sighting carries the issuer's organization")
	}
}

func TestRecorder_DecidedCandidateOnlyBumps(t *testing.T) {
	t.Parallel()

	for _, status := range []cimdcandidates.Status{cimdcandidates.StatusDismissed, cimdcandidates.StatusCustomerSpecific, cimdcandidates.StatusApproved} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			conn := newTestDB(t, "cimd_candidates_decided_"+string(status))
			first := seedIssuer(t, conn, "first", admission.ModeOpen)
			second := seedIssuer(t, conn, "second", admission.ModeOpen)
			logger := testenv.NewLogger(t)

			cimdcandidates.NewRecorder(conn).Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "Original", UserSessionIssuerID: first.id})
			require.NoError(t, testrepo.New(conn).SetCimdClientCandidateStatusFixture(ctx, testrepo.SetCimdClientCandidateStatusFixtureParams{Status: string(status), Url: uncoveredURL}))
			before, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
			require.NoError(t, err)

			// A fresh recorder stands in for another process.
			cimdcandidates.NewRecorder(conn).Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "Renamed", UserSessionIssuerID: first.id})
			cimdcandidates.NewRecorder(conn).Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "Renamed", UserSessionIssuerID: second.id})

			after, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
			require.NoError(t, err)
			require.Len(t, after, 1)
			require.Equal(t, string(status), after[0].Status, "a decision never returns to pending")
			require.Equal(t, "Original", after[0].ClientName.String)
			require.Equal(t, before[0].FirstSeenAt, after[0].FirstSeenAt)
			require.True(t, after[0].LastSeenAt.Time.After(before[0].LastSeenAt.Time), "last_seen_at is bumped")

			sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
			require.NoError(t, err)
			require.Len(t, sightings, 2, "a decided candidate still gains sightings")
		})
	}
}

func TestRecorder_PendingCapSkipsOnlyNewCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_cap")
	flooded := seedIssuer(t, conn, "flooded", admission.ModeOpen)
	other := seedIssuer(t, conn, "other", admission.ModeOpen)
	recorder := cimdcandidates.NewRecorder(conn)
	logger := testenv.NewLogger(t)

	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: other.id})
	for i := range cimdcandidates.MaxPendingPerIssuer {
		recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: fmt.Sprintf("https://flood-%d.example.test/client.json", i), ClientName: "", UserSessionIssuerID: flooded.id})
	}
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: "https://flood-over.example.test/client.json", ClientName: "", UserSessionIssuerID: flooded.id})
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: flooded.id})

	candidates, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, cimdcandidates.MaxPendingPerIssuer+1, "the URL past the cap is not created")
	for _, c := range candidates {
		require.NotEqual(t, "https://flood-over.example.test/client.json", c.Url)
	}

	sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	var flooderSawExisting bool
	for _, s := range sightings {
		if s.Url == uncoveredURL && s.UserSessionIssuerID == flooded.id {
			flooderSawExisting = true
		}
	}
	require.True(t, flooderSawExisting, "an existing candidate is still sighted past the cap")
}

func TestRecorder_WriteFailureIsSwallowed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_failure")
	issuer := seedIssuer(t, conn, "issuer", admission.ModeOpen)
	logger := testenv.NewLogger(t)

	broken, err := infra.CloneTestDatabase(t, "cimd_candidates_failure_closed")
	require.NoError(t, err)
	broken.Close()
	cimdcandidates.NewRecorder(broken).Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: issuer.id})

	sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Empty(t, sightings)

	cimdcandidates.NewRecorder(conn).Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: issuer.id})
	sightings, err = testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, sightings, 1)
}

func TestBackfill_IdempotentAndCoveredOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_backfill")
	open := seedIssuer(t, conn, "open", admission.ModeOpen)
	openToo := seedIssuer(t, conn, "open-too", admission.ModeOpen)
	presets := seedIssuer(t, conn, "presets", admission.ModePresets)
	logger := testenv.NewLogger(t)

	seed := func(issuer testIssuer, clientID string) {
		_, err := usersessionsrepo.New(conn).UpsertUserSessionClientFromCIMD(ctx, usersessionsrepo.UpsertUserSessionClientFromCIMDParams{
			UserSessionIssuerID:     issuer.id,
			ClientID:                clientID,
			ClientName:              "Seeded",
			RedirectUris:            []string{"http://127.0.0.1:33418/callback"},
			CacheTtlSeconds:         time.Hour.Seconds(),
			ClientIDMetadataEtag:    pgtype.Text{},
			TokenEndpointAuthMethod: "none",
			ClientJwks:              nil,
			ClientJwksUri:           pgtype.Text{},
		})
		require.NoError(t, err)
	}
	seed(open, uncoveredURL)
	seed(openToo, uncoveredURL)
	seed(open, exactURL)
	seed(open, patternURL)
	seed(open, disabledExactURL)
	seed(presets, "https://presets-only.example.test/client.json")

	counts, err := cimdcandidates.Backfill(ctx, logger, conn, testCatalog())
	require.NoError(t, err)
	require.Equal(t, int64(5), counts.Scanned)
	require.Equal(t, int64(2), counts.Recorded)

	again, err := cimdcandidates.Backfill(ctx, logger, conn, testCatalog())
	require.NoError(t, err)
	require.Equal(t, counts, again)

	candidates, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, uncoveredURL, candidates[0].Url)
	require.Equal(t, "Seeded", candidates[0].ClientName.String)

	sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, sightings, 2)
}

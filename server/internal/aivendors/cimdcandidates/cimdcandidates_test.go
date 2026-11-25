package cimdcandidates_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
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

func testOrigin(t *testing.T) *url.URL {
	t.Helper()
	origin, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	return origin
}

// compiledURL is an exact, enabled document of the built-in catalog.
func compiledURL(t *testing.T) string {
	t.Helper()
	for _, p := range aivendors.CompiledSnapshot().Catalog() {
		if p.Enabled && !p.IsPattern() {
			return p.URL
		}
	}
	t.Fatal("built-in catalog has no exact document")
	return ""
}

// newRecorder returns a recorder over conn and a reader for its write counter.
func newRecorder(t *testing.T, conn *pgxpool.Pool) (*cimdcandidates.Recorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	return cimdcandidates.NewRecorder(conn, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), nil), reader
}

// writeOutcomes reads cimd.candidates.writes back by outcome.
func writeOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "cimd.candidates.writes" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value(attr.CIMDCandidateOutcomeKey)
				out[v.AsString()] += dp.Value
			}
		}
	}
	return out
}

func TestRecordable(t *testing.T) {
	t.Parallel()

	catalog := testCatalog()
	origin := testOrigin(t)
	assistantDoc := assistants.AssistantClientMetadataDocumentURL(origin, uuid.New())
	foreignAssistantPath := "https://elsewhere.example.test/.well-known/oauth-client/assistants/" + uuid.NewString()
	cases := []struct {
		name     string
		mode     admission.Mode
		origin   *url.URL
		clientID string
		want     bool
	}{
		{name: "open uncovered", mode: admission.ModeOpen, origin: origin, clientID: uncoveredURL, want: true},
		{name: "reporting uncovered", mode: admission.ModeReporting, origin: origin, clientID: uncoveredURL, want: true},
		{name: "presets uncovered", mode: admission.ModePresets, origin: origin, clientID: uncoveredURL, want: false},
		{name: "disabled uncovered", mode: admission.ModeDisabled, origin: origin, clientID: uncoveredURL, want: false},
		{name: "open exact", mode: admission.ModeOpen, origin: origin, clientID: exactURL, want: false},
		{name: "open pattern", mode: admission.ModeOpen, origin: origin, clientID: patternURL, want: false},
		{name: "open disabled exact", mode: admission.ModeOpen, origin: origin, clientID: disabledExactURL, want: false},
		{name: "open disabled pattern", mode: admission.ModeOpen, origin: origin, clientID: disabledPatternURL, want: false},
		{name: "reporting exact", mode: admission.ModeReporting, origin: origin, clientID: exactURL, want: false},
		{name: "assistant document on the deployment origin", mode: admission.ModeOpen, origin: origin, clientID: assistantDoc, want: false},
		{name: "assistant path on another origin", mode: admission.ModeOpen, origin: origin, clientID: foreignAssistantPath, want: true},
		{name: "assistant path on any origin without one", mode: admission.ModeOpen, origin: nil, clientID: foreignAssistantPath, want: false},
		{name: "assistant path with extra segment", mode: admission.ModeOpen, origin: origin, clientID: assistantDoc + "/x", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, cimdcandidates.Recordable(catalog, tc.mode, tc.origin, tc.clientID))
		})
	}
}

func TestRecorder_NewClientOnceWithSightingPerIssuer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_record")
	first := seedIssuer(t, conn, "first", admission.ModeOpen)
	second := seedIssuer(t, conn, "second", admission.ModeOpen)
	recorder, reader := newRecorder(t, conn)
	logger := testenv.NewLogger(t)

	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "New Client", UserSessionIssuerID: first.id})
	recorder.Wait()
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "New Client", UserSessionIssuerID: first.id})
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "New Client", UserSessionIssuerID: second.id})
	recorder.Wait()

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
	require.Equal(t, map[string]int64{"recorded": 2}, writeOutcomes(t, reader), "the repeat in one process is skipped")
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

			original, _ := newRecorder(t, conn)
			original.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "Original", UserSessionIssuerID: first.id})
			original.Wait()
			require.NoError(t, testrepo.New(conn).SetCimdClientCandidateStatusFixture(ctx, testrepo.SetCimdClientCandidateStatusFixtureParams{Status: string(status), Url: uncoveredURL}))
			before, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
			require.NoError(t, err)

			// A fresh recorder stands in for another process.
			other, _ := newRecorder(t, conn)
			other.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "Renamed", UserSessionIssuerID: first.id})
			other.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "Renamed", UserSessionIssuerID: second.id})
			other.Wait()

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

// fillPendingCap gives issuer MaxPendingPerIssuer pending candidates.
func fillPendingCap(t *testing.T, conn *pgxpool.Pool, issuer testIssuer) {
	t.Helper()
	recorder, _ := newRecorder(t, conn)
	for i := range cimdcandidates.MaxPendingPerIssuer {
		recorder.Record(t.Context(), testenv.NewLogger(t), cimdcandidates.Sighting{URL: fmt.Sprintf("https://flood-%d.example.test/client.json", i), ClientName: "", UserSessionIssuerID: issuer.id})
		// Sequential, so the cap is exact rather than soft.
		recorder.Wait()
	}
}

func TestRecorder_PendingCapSkipsOnlyNewCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_cap")
	flooded := seedIssuer(t, conn, "flooded", admission.ModeOpen)
	other := seedIssuer(t, conn, "other", admission.ModeOpen)
	recorder, reader := newRecorder(t, conn)
	logger := testenv.NewLogger(t)

	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: other.id})
	recorder.Wait()
	fillPendingCap(t, conn, flooded)
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: "https://flood-over.example.test/client.json", ClientName: "", UserSessionIssuerID: flooded.id})
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: flooded.id})
	recorder.Wait()

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
	require.Equal(t, map[string]int64{"recorded": 2, "capped": 1}, writeOutcomes(t, reader))
}

// holdCandidateInsert inserts url in an open transaction, so any other insert
// of url blocks until the returned release rolls it back.
func holdCandidateInsert(t *testing.T, conn *pgxpool.Pool, candidateURL string) (release func()) {
	t.Helper()
	tx := testenv.BeginTx(t, t.Context(), conn)
	require.NoError(t, testrepo.New(tx).InsertCimdClientCandidateFixture(t.Context(), candidateURL))
	return func() { require.NoError(t, tx.Rollback(context.Background())) }
}

func TestRecorder_FailedWriteReleasesItsKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_failure")
	issuer := seedIssuer(t, conn, "issuer", admission.ModeOpen)
	recorder, reader := newRecorder(t, conn)
	logger := testenv.NewLogger(t)
	sighting := cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: issuer.id}

	release := holdCandidateInsert(t, conn, uncoveredURL)
	recorder.Record(ctx, logger, sighting)
	recorder.Wait()
	release()
	require.Equal(t, map[string]int64{"failed": 1}, writeOutcomes(t, reader), "the blocked write times out")

	recorder.Record(ctx, logger, sighting)
	recorder.Wait()
	require.Equal(t, map[string]int64{"failed": 1, "recorded": 1}, writeOutcomes(t, reader), "the same recorder retries")
	sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, sightings, 1)
}

func TestRecorder_DropsWhenEverySlotIsBusy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_dropped")
	issuer := seedIssuer(t, conn, "issuer", admission.ModeOpen)
	recorder, reader := newRecorder(t, conn)
	logger := testenv.NewLogger(t)

	release := holdCandidateInsert(t, conn, uncoveredURL)
	// Each blocked write holds a slot until the hold is released.
	for range 8 {
		recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: uuid.New()})
	}
	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: issuer.id})
	require.Equal(t, int64(1), writeOutcomes(t, reader)["dropped"])
	release()
	recorder.Wait()

	recorder.Record(ctx, logger, cimdcandidates.Sighting{URL: uncoveredURL, ClientName: "", UserSessionIssuerID: issuer.id})
	recorder.Wait()
	require.Equal(t, int64(1), writeOutcomes(t, reader)["recorded"], "the dropped key was released")
}

func TestBackfill(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t, "cimd_candidates_backfill")
	origin := testOrigin(t)
	open := seedIssuer(t, conn, "open", admission.ModeOpen)
	reporting := seedIssuer(t, conn, "reporting", admission.ModeReporting)
	presets := seedIssuer(t, conn, "presets", admission.ModePresets)
	retired := seedIssuer(t, conn, "retired", admission.ModeOpen)
	capped := seedIssuer(t, conn, "capped", admission.ModeOpen)
	logger := testenv.NewLogger(t)

	seed := func(issuer testIssuer, clientID string) usersessionsrepo.UserSessionClient {
		row, err := usersessionsrepo.New(conn).UpsertUserSessionClientFromCIMD(ctx, usersessionsrepo.UpsertUserSessionClientFromCIMDParams{
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
		return row
	}
	seed(open, uncoveredURL)
	seed(reporting, uncoveredURL)
	seed(open, exactURL)
	seed(open, patternURL)
	seed(open, disabledExactURL)
	seed(open, compiledURL(t))
	seed(open, assistants.AssistantClientMetadataDocumentURL(origin, uuid.New()))
	seed(presets, "https://presets-only.example.test/client.json")

	revoked := seed(open, "https://revoked.example.test/client.json")
	_, err := usersessionsrepo.New(conn).RevokeUserSessionClient(ctx, usersessionsrepo.RevokeUserSessionClientParams{ID: revoked.ID, ProjectID: open.projectID, OrganizationID: testOrgID})
	require.NoError(t, err)
	seed(retired, "https://retired-issuer.example.test/client.json")
	_, err = usersessionsrepo.New(conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{ID: retired.id, ProjectID: retired.projectID})
	require.NoError(t, err)

	fillPendingCap(t, conn, capped)
	seed(capped, "https://over-cap.example.test/client.json")
	seed(capped, uncoveredURL)

	counts, err := cimdcandidates.Backfill(ctx, logger, conn, testCatalog(), origin)
	require.NoError(t, err)
	// open: 6 live rows; reporting: 1; capped: 2. Presets, revoked and retired rows are not read.
	require.Equal(t, cimdcandidates.BackfillCounts{Scanned: 9, Recorded: 3, Skipped: 1}, counts)

	again, err := cimdcandidates.Backfill(ctx, logger, conn, testCatalog(), origin)
	require.NoError(t, err)
	require.Equal(t, counts, again)

	candidates, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	urls := make([]string, 0, len(candidates))
	for _, c := range candidates {
		urls = append(urls, c.Url)
	}
	require.Contains(t, urls, uncoveredURL)
	require.Len(t, urls, cimdcandidates.MaxPendingPerIssuer+1, "only the uncovered URL joins the cap filler")
	for _, c := range candidates {
		if c.Url == uncoveredURL {
			require.Equal(t, "Seeded", c.ClientName.String)
		}
	}

	sightings, err := testrepo.New(conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	var reach []uuid.UUID
	for _, s := range sightings {
		if s.Url == uncoveredURL {
			reach = append(reach, s.UserSessionIssuerID)
		}
	}
	require.ElementsMatch(t, []uuid.UUID{open.id, reporting.id, capped.id}, reach, "an issuer at the cap still sights an existing candidate")
}

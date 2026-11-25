package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const candidateRedirectURI = "http://127.0.0.1:51423/callback"

// authorizeAndWait drives one authorize and waits for its candidate write.
func authorizeAndWait(t *testing.T, ti *testInstance, slug, clientID string) {
	t.Helper()
	w := doCIMDAuthorize(t, ti, slug, clientID, candidateRedirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	ti.service.WaitForCIMDCandidates()
}

// seedOpenToolset adds a public toolset behind its own issuer in mode.
func seedOpenToolset(t *testing.T, ctx context.Context, ti *testInstance, mode admission.Mode) hostedToolset {
	t.Helper()
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	setIssuerAdmissionMode(t, ctx, ti, toolset, mode)
	testenv.SetHostedMCP(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Visibility: "public"})
	return toolset
}

func TestCIMDCandidates_RecordsUncoveredClientOncePerIssuer(t *testing.T) {
	t.Parallel()

	for _, mode := range []admission.Mode{admission.ModeOpen, admission.ModeReporting} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			ctx, ti, ds, toolset := newTestCIMDService(t)
			setIssuerAdmissionMode(t, ctx, ti, toolset, mode)
			other := seedOpenToolset(t, ctx, ti, mode)

			for _, slug := range []string{toolset.McpSlug.String, toolset.McpSlug.String, other.McpSlug.String} {
				authorizeAndWait(t, ti, slug, ds.clientID)
			}

			candidates, err := testrepo.New(ti.conn).ListCimdClientCandidatesFixture(ctx)
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			require.Equal(t, ds.clientID, candidates[0].Url)
			require.Equal(t, "CIMD Integration Client", candidates[0].ClientName.String)
			require.Equal(t, string(cimdcandidates.StatusPending), candidates[0].Status)

			sightings, err := testrepo.New(ti.conn).ListCimdClientCandidateSightingsFixture(ctx)
			require.NoError(t, err)
			require.Len(t, sightings, 2, "one sighting per issuer")
			got := []string{sightings[0].UserSessionIssuerID.String(), sightings[1].UserSessionIssuerID.String()}
			require.ElementsMatch(t, []string{toolset.UserSessionIssuerID.UUID.String(), other.UserSessionIssuerID.UUID.String()}, got)
		})
	}
}

func TestCIMDCandidates_CoveredClientNeverRecorded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document func(serverURL, clientID string) aivendors.Document
	}{
		{name: "enabled exact", document: func(_, clientID string) aivendors.Document {
			return aivendors.Document{URL: clientID, DisplayName: "Covered", Enabled: true, Pattern: false}
		}},
		{name: "disabled exact", document: func(_, clientID string) aivendors.Document {
			return aivendors.Document{URL: clientID, DisplayName: "Covered", Enabled: false, Pattern: false}
		}},
		{name: "enabled pattern", document: func(serverURL, _ string) aivendors.Document {
			return aivendors.Document{URL: serverURL + "/*/client.json", DisplayName: "Covered", Enabled: true, Pattern: true}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti, ds, toolset := newTestCIMDService(t)
			ti.aiCatalog.Publish(aivendors.NewSnapshot([]aivendors.Product{
				{ID: "covered", VendorKey: "covered", DisplayName: "Covered", Documents: []aivendors.Document{tc.document(ds.srv.URL, ds.clientID)}},
			}))

			authorizeAndWait(t, ti, toolset.McpSlug.String, ds.clientID)
			require.Positive(t, ds.requests.Load(), "the document was fetched and the client row written")

			candidates, err := testrepo.New(ti.conn).ListCimdClientCandidatesFixture(ctx)
			require.NoError(t, err)
			require.Empty(t, candidates)
		})
	}
}

func TestCIMDCandidates_PresetsRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	authorizeAndWait(t, ti, toolset.McpSlug.String, ds.clientID)
	require.Positive(t, ds.requests.Load())

	candidates, err := testrepo.New(ti.conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestCIMDCandidates_RevalidationBumpsLastSeen(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	ds.set(t, func(ds *cimdDocServer) { ds.etag = `"v1"` })

	authorizeAndWait(t, ti, toolset.McpSlug.String, ds.clientID)
	before, err := testrepo.New(ti.conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, before, 1)

	lapseCIMDCache(t, ctx, ti, getCIMDClientRow(t, ctx, ti, toolset, ds.clientID))
	ti.service.ForgetCIMDCandidateSightings(t)
	authorizeAndWait(t, ti, toolset.McpSlug.String, ds.clientID)
	require.Equal(t, int64(1), ds.conditionalRequests.Load(), "the second fetch is a revalidation")

	after, err := testrepo.New(ti.conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.True(t, after[0].LastSeenAt.Time.After(before[0].LastSeenAt.Time), "a 304 is a sighting")
}

func TestCIMDCandidates_CapStopsNewCandidatesOnly(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	other := seedOpenToolset(t, ctx, ti, admission.ModeOpen)
	// httptest TLS servers share one certificate, so the service trusts both.
	fresh := startCIMDDocServer(t)

	authorizeAndWait(t, ti, other.McpSlug.String, ds.clientID)

	filler := cimdcandidates.NewRecorder(ti.conn, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	for i := range cimdcandidates.MaxPendingPerIssuer {
		filler.Record(ctx, testenv.NewLogger(t), cimdcandidates.Sighting{URL: fmt.Sprintf("https://flood-%d.example.test/client.json", i), ClientName: "", UserSessionIssuerID: toolset.UserSessionIssuerID.UUID})
		filler.Wait()
	}

	authorizeAndWait(t, ti, toolset.McpSlug.String, fresh.clientID)
	authorizeAndWait(t, ti, toolset.McpSlug.String, ds.clientID)

	candidates, err := testrepo.New(ti.conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		require.NotEqual(t, fresh.clientID, c.Url, "an issuer at its cap raises no new candidate")
	}
	sightings, err := testrepo.New(ti.conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	var reach []string
	for _, s := range sightings {
		if s.Url == ds.clientID {
			reach = append(reach, s.UserSessionIssuerID.String())
		}
	}
	require.ElementsMatch(t, []string{other.UserSessionIssuerID.UUID.String(), toolset.UserSessionIssuerID.UUID.String()}, reach, "an existing candidate is still sighted")
}

package mcp_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

func TestCIMDCandidates_OpenRecordsUncoveredClientOncePerIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	other, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	setIssuerAdmissionMode(t, ctx, ti, other, admission.ModeOpen)
	testenv.SetHostedMCP(t, ctx, ti.conn, other.ProjectID, other.ID, testenv.HostedMCP{Visibility: "public"})

	for _, slug := range []string{toolset.McpSlug.String, toolset.McpSlug.String, other.McpSlug.String} {
		w := doCIMDAuthorize(t, ti, slug, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t)))
		require.Equal(t, http.StatusFound, w.Code)
	}

	candidates, err := testrepo.New(ti.conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, ds.clientID, candidates[0].Url)
	require.Equal(t, "CIMD Integration Client", candidates[0].ClientName.String)
	require.Equal(t, "pending", candidates[0].Status)

	sightings, err := testrepo.New(ti.conn).ListCimdClientCandidateSightingsFixture(ctx)
	require.NoError(t, err)
	require.Len(t, sightings, 2, "one sighting per issuer")
	got := []string{sightings[0].UserSessionIssuerID.String(), sightings[1].UserSessionIssuerID.String()}
	require.ElementsMatch(t, []string{toolset.UserSessionIssuerID.UUID.String(), other.UserSessionIssuerID.UUID.String()}, got)
}

func TestCIMDCandidates_CoveredClientNeverRecorded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document func(clientID string) aivendors.Document
	}{
		{name: "enabled exact", document: func(clientID string) aivendors.Document {
			return aivendors.Document{URL: clientID, DisplayName: "Covered", Enabled: true, Pattern: false}
		}},
		{name: "disabled exact", document: func(clientID string) aivendors.Document {
			return aivendors.Document{URL: clientID, DisplayName: "Covered", Enabled: false, Pattern: false}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti, ds, toolset := newTestCIMDService(t)
			ti.aiCatalog.Publish(aivendors.NewSnapshot([]aivendors.Product{
				{ID: "covered", VendorKey: "covered", DisplayName: "Covered", Documents: []aivendors.Document{tc.document(ds.clientID)}},
			}))

			w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t)))
			require.Equal(t, http.StatusFound, w.Code)
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

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	require.Positive(t, ds.requests.Load())

	candidates, err := testrepo.New(ti.conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

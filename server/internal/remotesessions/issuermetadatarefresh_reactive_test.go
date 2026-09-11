package remotesessions_test

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// reasonOutcomeCounts groups the issuer metadata refresh metric by reason, then outcome.
func reasonOutcomeCounts(t *testing.T, reader *sdkmetric.ManualReader) map[remotesessionmetrics.IssuerMetadataRefreshReason]map[remotesessionmetrics.IssuerMetadataRefreshOutcome]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	counts := map[remotesessionmetrics.IssuerMetadataRefreshReason]map[remotesessionmetrics.IssuerMetadataRefreshOutcome]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.remote_session_issuer.metadata_refresh" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				reasonValue, _ := dp.Attributes.Value(attr.OAuthIssuerMetadataRefreshReasonKey)
				outcomeValue, _ := dp.Attributes.Value(attr.OutcomeKey)
				reason := remotesessionmetrics.IssuerMetadataRefreshReason(reasonValue.AsString())
				if counts[reason] == nil {
					counts[reason] = map[remotesessionmetrics.IssuerMetadataRefreshOutcome]int64{}
				}
				counts[reason][remotesessionmetrics.IssuerMetadataRefreshOutcome(outcomeValue.AsString())] += dp.Value
			}
		}
	}
	return counts
}

func TestIssuerMetadataRefresh_RequestRefresh_FetchesInsideTheDailyCadence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	var requests atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { requests.Add(1) })
	id := createProjectIssuer(t, ctx, ti, "request-refresh-due", upstream.URL)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &twoHoursAgo, lastError: "", errorAt: nil, errorURL: ""})
	use := remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id))
	require.False(t, fetchDue(t, ctx, ti, id, time.Now()), "the on-use cadence would leave the row alone")

	refresher.RequestRefresh(ctx, use, remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing)
	refresher.Wait()

	require.EqualValues(t, 1, requests.Load())
	after := loadIssuerByID(t, ctx, ti, id)
	require.WithinDuration(t, time.Now(), after.MetadataFetchedAt.Time, time.Minute)
	require.Equal(t, upstream.URL+"/token", after.TokenEndpoint.String)
	counts := reasonOutcomeCounts(t, reader)
	require.Len(t, counts, 1, "every sample carries the trigger reason: %v", counts)
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing][remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
}

func TestIssuerMetadataRefresh_RequestRefresh_RecentVisitIsSkippedAtFlowTime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	var requests atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { requests.Add(1) })
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)

	cases := []struct {
		name     string
		tracking metadataTracking
	}{
		{name: "fetched", tracking: metadataTracking{document: "", fetchedAt: &fiveMinutesAgo, lastError: "", errorAt: nil, errorURL: ""}},
		{name: "failed transiently", tracking: metadataTracking{document: "", fetchedAt: nil, lastError: "unreachable", errorAt: &fiveMinutesAgo, errorURL: upstream.URL + "/.well-known/openid-configuration"}},
		{name: "failed definitively", tracking: metadataTracking{document: "", fetchedAt: nil, lastError: "issuer mismatch", errorAt: &fiveMinutesAgo, errorURL: ""}},
	}
	for i, tc := range cases {
		id := createProjectIssuer(t, ctx, ti, "request-refresh-recent-"+strconv.Itoa(i), upstream.URL+"/"+strconv.Itoa(i))
		setIssuerMetadataTracking(t, ctx, ti, id, tc.tracking)
		before := loadIssuerByID(t, ctx, ti, id)

		refresher.RequestRefresh(ctx, remotesessions.IssuerMetadataUseFromRow(before), remotesessionmetrics.IssuerMetadataRefreshReasonUnknownSigningKey)
		refresher.Wait()

		after := loadIssuerByID(t, ctx, ti, id)
		require.True(t, after.UpdatedAt.Time.Equal(before.UpdatedAt.Time), "%s: the row is untouched", tc.name)
	}
	require.Zero(t, requests.Load(), "no discovery runs inside the reactive interval")
	counts := reasonOutcomeCounts(t, reader)
	require.Len(t, counts, 1)
	reactive := counts[remotesessionmetrics.IssuerMetadataRefreshReasonUnknownSigningKey]
	require.Len(t, reactive, 1, "%v", reactive)
	require.Equal(t, int64(len(cases)), reactive[remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedRecent])
}

func TestIssuerMetadataRefresh_RequestRefresh_ReplansStaleSnapshotAsSkippedRecent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	var requests atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { requests.Add(1) })
	id := createProjectIssuer(t, ctx, ti, "request-refresh-stale-snapshot", upstream.URL)
	staleUse := remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id))
	require.True(t, remotesessions.ReactiveIssuerMetadataRefreshDue(staleUse, time.Now()))

	refresher.RequestRefresh(ctx, staleUse, remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing)
	refresher.Wait()
	require.EqualValues(t, 1, requests.Load())

	// The same never-visited snapshot passes the flow-time check; the reload finds the fresh row and stops.
	refresher.RequestRefresh(ctx, staleUse, remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing)
	refresher.Wait()

	require.EqualValues(t, 1, requests.Load(), "the stale snapshot is replanned from the refreshed row")
	counts := reasonOutcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing]
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedRecent])
	require.Len(t, counts, 2, "%v", counts)
}

func TestIssuerMetadataRefresh_RequestRefresh_NilRefresherIsSafe(t *testing.T) {
	t.Parallel()

	var refresher *remotesessions.IssuerMetadataRefresher
	refresher.RequestRefresh(t.Context(), remotesessions.IssuerMetadataUse{ID: uuid.New(), IssuerURL: "https://idp.example.com"}, remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing)
}

func loadEnvIssuer(t *testing.T, env syntheticExpiryEnv) repo.RemoteSessionIssuer {
	t.Helper()
	row, err := env.q.GetRemoteSessionIssuerByID(t.Context(), repo.GetRemoteSessionIssuerByIDParams{
		ID:                    env.issuerID,
		ProjectID:             conv.ToNullUUID(env.projectID),
		IncludeOrganizational: true,
		OrganizationID:        conv.ToPGText(env.organizationID),
		IncludeGlobal:         true,
	})
	require.NoError(t, err)
	return row
}

// A token endpoint that answers 404 or 410 on refresh says the stored endpoint moved: the issuer is refreshed once, and a second answer inside the reactive interval starts nothing.
func TestRefreshNow_TokenEndpointMissing_RefreshesIssuerMetadataOnce(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			var discoveries atomic.Int32
			upstream := fakeIssuerServer(t, func(map[string]any) { discoveries.Add(1) })
			ctx, env := newSyntheticExpiryEnv(t, "token-endpoint-"+strconv.Itoa(status), func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				if r.Form.Get("grant_type") != "refresh_token" {
					_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"live-refresh"}`))
					return
				}
				w.WriteHeader(status)
			}, withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))
			require.NotEqual(t, upstream.URL+"/token", loadEnvIssuer(t, env).TokenEndpoint.String)

			session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)

			_, refreshErr := env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest)
			var tokenErr *remotesessions.TokenRefreshError
			require.ErrorAs(t, refreshErr, &tokenErr, "the caller's error is unchanged")
			env.issuerMetadata.Wait()

			require.EqualValues(t, 1, discoveries.Load())
			after := loadEnvIssuer(t, env)
			require.WithinDuration(t, time.Now(), after.MetadataFetchedAt.Time, time.Minute)
			require.Equal(t, upstream.URL+"/token", after.TokenEndpoint.String, "the issuer now carries the discovered endpoint")
			counts := reasonOutcomeCounts(t, env.issuerMetadataReader)
			require.Len(t, counts, 1, "the on-use cadence stays silent: %v", counts)
			require.Len(t, counts[remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing], 1, "%v", counts)
			require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing][remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])

			// The discovered endpoint answers 404 too; inside the interval nothing starts.
			session, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)
			_, refreshErr = env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest)
			require.ErrorAs(t, refreshErr, &tokenErr)
			env.issuerMetadata.Wait()

			require.EqualValues(t, 1, discoveries.Load(), "a second answer inside the interval starts no discovery")
			require.True(t, loadEnvIssuer(t, env).UpdatedAt.Time.Equal(after.UpdatedAt.Time), "the row is untouched")
			reactive := reasonOutcomeCounts(t, env.issuerMetadataReader)[remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing]
			require.Equal(t, int64(1), reactive[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
			require.Equal(t, int64(1), reactive[remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedRecent])
		})
	}
}

// An upstream rejection of the grant or client says nothing about the endpoints, whatever status carries it.
func TestRefreshNow_UpstreamRejection_DoesNotRefreshIssuerMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status int
		body   string
	}{
		{status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`},
		{status: http.StatusNotFound, body: `{"error":"invalid_grant"}`},
		{status: http.StatusGone, body: `{"error":"invalid_client","error_description":"client withdrawn"}`},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			t.Parallel()

			var discoveries atomic.Int32
			upstream := fakeIssuerServer(t, func(map[string]any) { discoveries.Add(1) })
			ctx, env := newSyntheticExpiryEnv(t, "token-endpoint-rejects-"+strconv.Itoa(tc.status), func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				if r.Form.Get("grant_type") != "refresh_token" {
					_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"dead-refresh"}`))
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))

			session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)
			_, refreshErr := env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerRequest)
			require.Error(t, refreshErr)
			env.issuerMetadata.Wait()

			require.Zero(t, discoveries.Load())
			require.Empty(t, reasonOutcomeCounts(t, env.issuerMetadataReader))
		})
	}
}

// A code exchange against a withdrawn token endpoint refreshes the issuer the client points at.
func TestRemoteLogin_TokenEndpointMissing_RefreshesIssuerMetadata(t *testing.T) {
	t.Parallel()

	var discoveries atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { discoveries.Add(1) })
	_, env, callback, err := driveSyntheticLogin(t, "exchange-token-endpoint-gone", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}, withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, callback.Code)
	env.issuerMetadata.Wait()

	require.EqualValues(t, 1, discoveries.Load())
	require.Equal(t, upstream.URL+"/token", loadEnvIssuer(t, env).TokenEndpoint.String)
	counts := reasonOutcomeCounts(t, env.issuerMetadataReader)
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing][remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
}

// A 404 that carries an OAuth error body is the endpoint refusing the client, not a withdrawn endpoint.
func TestRemoteLogin_TokenEndpointRefusesClient_DoesNotRefreshIssuerMetadata(t *testing.T) {
	t.Parallel()

	var discoveries atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { discoveries.Add(1) })
	_, env, callback, err := driveSyntheticLogin(t, "exchange-token-endpoint-refuses", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}, withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, callback.Code)
	env.issuerMetadata.Wait()

	require.Zero(t, discoveries.Load())
	require.NotEqual(t, upstream.URL+"/token", loadEnvIssuer(t, env).TokenEndpoint.String)
	require.Empty(t, reasonOutcomeCounts(t, env.issuerMetadataReader))
}

// An ID token signed under a kid the key set lacks, even after the forced key refresh, refreshes the issuer: its jwks_uri may have moved. The exchange itself still succeeds without identity.
func TestRemoteLogin_UnknownSigningKey_RefreshesIssuerMetadata(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-rotated-jwks"
	var nonce atomic.Pointer[string]
	var discoveries atomic.Int32
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		discoveries.Add(1)
		doc["jwks_uri"] = issuer.jwksURI
	})
	_, env := newSyntheticExpiryEnv(t, "idtoken-rotated-jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
			issuer.mintWithKid(t, "rotated-kid", issuer.claims(clientID, loadString(&nonce))) + `"}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))
	env.issuerMetadata.Wait()

	require.False(t, env.session.UpstreamSubject.Valid, "the grant is stored without identity")
	require.EqualValues(t, 1, discoveries.Load())
	require.Equal(t, upstream.URL+"/token", loadEnvIssuer(t, env).TokenEndpoint.String, "the issuer now carries the discovered endpoints")
	counts := reasonOutcomeCounts(t, env.issuerMetadataReader)
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshReasonUnknownSigningKey][remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
	require.Len(t, counts, 1, "the on-use cadence stays silent: %v", counts)
}

// A signature that fails against a published key is not drift.
func TestRemoteLogin_InvalidSignature_DoesNotRefreshIssuerMetadata(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	other := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-bad-signature"
	var nonce atomic.Pointer[string]
	var discoveries atomic.Int32
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		discoveries.Add(1)
		doc["jwks_uri"] = issuer.jwksURI
	})
	_, env := newSyntheticExpiryEnv(t, "idtoken-bad-signature", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
			other.mint(t, issuer.claims(clientID, loadString(&nonce))) + `"}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))
	env.issuerMetadata.Wait()

	require.False(t, env.session.UpstreamSubject.Valid)
	require.Zero(t, discoveries.Load())
	require.Empty(t, reasonOutcomeCounts(t, env.issuerMetadataReader))
}

// A token with no kid against a key set of several keys fails verification but names no missing key: not drift.
func TestRemoteLogin_KidlessIDToken_DoesNotRefreshIssuerMetadata(t *testing.T) {
	t.Parallel()

	issuer := newAmbiguousIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-kidless"
	var nonce atomic.Pointer[string]
	var discoveries atomic.Int32
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		discoveries.Add(1)
		doc["jwks_uri"] = issuer.jwksURI
	})
	_, env := newSyntheticExpiryEnv(t, "idtoken-kidless", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
			issuer.mintWithoutKid(t, issuer.claims(clientID, loadString(&nonce))) + `"}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIssuerURL(upstream.URL), withIssuerMetadataRefresh(), withIssuerMetadataFetchedAt(time.Now().Add(-2*time.Hour)))
	env.issuerMetadata.Wait()

	require.False(t, env.session.UpstreamSubject.Valid, "verification fails and the grant is stored without identity")
	require.Zero(t, discoveries.Load())
	require.Empty(t, reasonOutcomeCounts(t, env.issuerMetadataReader))
}

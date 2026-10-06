package remotemcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

func TestProxyManager_UnrepresentableProtectedResourceRecordsErrorAndBacksOff(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		member string
	}{
		{name: "raw extension NUL", member: `"extension":"\u0000"`},
		{name: "raw extension unpaired surrogate", member: `"extension":"\ud800"`},
		{name: "raw extension numeric overflow", member: `"extension":1e1000000`},
		{name: "extracted scope NUL", member: `"scopes_supported":["read\u0000private"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestServiceForProbe(t)
			body := `{"resource":"{{origin}}","authorization_servers":["https://auth.example.test"],` + tc.member + `}`
			require.True(t, json.Valid([]byte(body)), "the document is valid JSON before PostgreSQL rejects its values")
			upstream, hits := probedUpstream(t, body)
			server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
			manager, probed := newProbingManager(t, ti)
			var starts atomic.Int64
			manager.SetBeforeProtectedResourceProbe(func() { starts.Add(1) })

			postInitialize(t, ctx, manager, server)
			<-probed
			failed := loadProtectedResource(t, ctx, ti, upstream.URL)
			require.EqualValues(t, 1, hits.Load())
			require.Equal(t, upstream.URL+wellknown.OAuthProtectedResourcePath, failed.MetadataUrl.String)
			require.Equal(t, "The metadata document contains values that cannot be stored.", failed.MetadataLastError.String)
			require.True(t, failed.MetadataLastErrorAt.Valid)
			require.False(t, failed.MetadataFetchedAt.Valid)
			require.Nil(t, failed.Metadata)
			require.Nil(t, failed.AuthorizationServers)
			require.Nil(t, failed.ScopesSupported)

			postInitialize(t, ctx, manager, server)
			require.EqualValues(t, 1, starts.Load(), "a recorded failure schedules no second probe")
			require.EqualValues(t, 1, hits.Load())

			// A fresh manager resets the per-replica checks. The persisted
			// error, not the in-memory debounce, prevents another fetch.
			fresh, freshProbed := newProbingManager(t, ti)
			fresh.SetProtectedResourceProbeClock(func() time.Time { return time.Now().Add(23 * time.Hour) })
			postInitialize(t, ctx, fresh, server)
			<-freshProbed
			require.EqualValues(t, 1, hits.Load())
			require.Equal(t, failed.MetadataLastErrorAt, loadProtectedResource(t, ctx, ti, upstream.URL).MetadataLastErrorAt)

			later, laterProbed := newProbingManager(t, ti)
			later.SetProtectedResourceProbeClock(func() time.Time { return time.Now().Add(25 * time.Hour) })
			postInitialize(t, ctx, later, server)
			<-laterProbed
			require.EqualValues(t, 2, hits.Load(), "the persisted failure becomes eligible again after 24 hours")
			retried := loadProtectedResource(t, ctx, ti, upstream.URL)
			require.Equal(t, failed.ID, retried.ID)
			require.Equal(t, failed.MetadataLastError, retried.MetadataLastError)
			require.True(t, retried.MetadataLastErrorAt.Time.After(failed.MetadataLastErrorAt.Time))
			require.False(t, retried.MetadataFetchedAt.Valid)
		})
	}
}

func TestProxyManager_UnrepresentableProtectedResourcePreservesMetadataAndRecovers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	var origin string
	var body atomic.Value
	body.Store(probedResourceDocument)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		document, ok := body.Load().(string)
		if !ok {
			http.Error(w, "missing test document", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(document, "{{origin}}", origin)))
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL

	server := seedRemoteMcpServerWithURL(t, ctx, ti, origin)
	manager, probed := newProbingManager(t, ti)
	postInitialize(t, ctx, manager, server)
	<-probed
	good := loadProtectedResource(t, ctx, ti, origin)
	require.True(t, good.MetadataFetchedAt.Valid)

	body.Store(`{"resource":"{{origin}}","authorization_servers":["https://different.example.test"],"scopes_supported":["write"],"resource_name":"Rejected","extension":"\u0000"}`)
	later, laterProbed := newProbingManager(t, ti)
	later.SetProtectedResourceProbeClock(func() time.Time { return time.Now().Add(25 * time.Hour) })
	postInitialize(t, ctx, later, server)
	<-laterProbed
	failed := loadProtectedResource(t, ctx, ti, origin)
	require.Equal(t, good.ID, failed.ID)
	require.Equal(t, "The metadata document contains values that cannot be stored.", failed.MetadataLastError.String)
	require.True(t, failed.MetadataLastErrorAt.Valid)
	require.Equal(t, good.Metadata, failed.Metadata)
	require.Equal(t, good.MetadataFetchedAt, failed.MetadataFetchedAt)
	require.Equal(t, good.AuthorizationServers, failed.AuthorizationServers)
	require.Equal(t, good.ScopesSupported, failed.ScopesSupported)
	require.Equal(t, good.ResourceName, failed.ResourceName)

	body.Store(strings.ReplaceAll(probedResourceDocument, "Probed", "Recovered"))
	retry, retryProbed := newProbingManager(t, ti)
	retry.SetProtectedResourceProbeClock(func() time.Time { return time.Now().Add(25 * time.Hour) })
	postInitialize(t, ctx, retry, server)
	<-retryProbed
	recovered := loadProtectedResource(t, ctx, ti, origin)
	require.Equal(t, good.ID, recovered.ID)
	require.Equal(t, "Recovered", recovered.ResourceName.String)
	require.True(t, recovered.MetadataFetchedAt.Time.After(good.MetadataFetchedAt.Time))
	require.False(t, recovered.MetadataLastError.Valid)
	require.False(t, recovered.MetadataLastErrorAt.Valid)
}

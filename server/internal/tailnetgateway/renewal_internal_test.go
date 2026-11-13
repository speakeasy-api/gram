package tailnetgateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestARIClientReadsTheRenewalWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	leaf := testCertificate(t, now.Add(90*24*time.Hour)).Leaf
	leaf.AuthorityKeyId = []byte{0x01, 0x02, 0x03}
	start, end := now.Add(60*24*time.Hour), now.Add(62*24*time.Hour)

	var directoryReads atomic.Int32
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/directory", func(w http.ResponseWriter, _ *http.Request) {
		directoryReads.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"renewalInfo": server.URL + "/renewal-info/"})
	})
	mux.HandleFunc("/renewal-info/{id}", func(w http.ResponseWriter, r *http.Request) {
		want := base64.RawURLEncoding.EncodeToString(leaf.AuthorityKeyId) + "." + base64.RawURLEncoding.EncodeToString(leaf.SerialNumber.Bytes())
		if r.PathValue("id") != want {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Retry-After", "21600")
		_ = json.NewEncoder(w).Encode(map[string]any{"suggestedWindow": map[string]time.Time{"start": start, "end": end}})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := &ariClient{directoryURL: server.URL + "/directory", client: server.Client(), mu: sync.Mutex{}, renewalInfoURL: ""}
	renewAt, checkAgain, err := client.RenewAt(t.Context(), leaf, now)
	require.NoError(t, err)
	require.False(t, renewAt.Before(start), "the renewal falls inside the window")
	require.True(t, renewAt.Before(end))
	require.Equal(t, now.Add(6*time.Hour), checkAgain, "Retry-After sets the next check")

	again, _, err := client.RenewAt(t.Context(), leaf, now)
	require.NoError(t, err)
	require.Equal(t, renewAt, again, "every slot holding the certificate picks the same moment")
	require.Equal(t, int32(1), directoryReads.Load(), "the directory is read once")
}

func TestARIClientRejectsAnInvalidWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/directory" {
			_ = json.NewEncoder(w).Encode(map[string]string{"renewalInfo": server.URL + "/renewal-info"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"suggestedWindow": map[string]time.Time{"start": now, "end": now.Add(-time.Hour)}})
	}))
	t.Cleanup(server.Close)

	client := &ariClient{directoryURL: server.URL + "/directory", client: server.Client(), mu: sync.Mutex{}, renewalInfoURL: ""}
	_, _, err := client.RenewAt(t.Context(), testCertificate(t, now.Add(time.Hour)).Leaf, now)
	require.ErrorContains(t, err, "invalid certificate renewal window")
}

func TestRenewalCheckInterval(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)

	require.Equal(t, renewalCheckMax, renewalCheckInterval("", now), "no Retry-After waits the longest")
	require.Equal(t, 3*time.Hour, renewalCheckInterval("10800", now))
	require.Equal(t, renewalCheckMin, renewalCheckInterval("60", now), "a short Retry-After is raised to an hour")
	require.Equal(t, renewalCheckMax, renewalCheckInterval("604800", now), "a long Retry-After is capped so a moved window is seen")
	require.Equal(t, 2*time.Hour, renewalCheckInterval(now.Add(2*time.Hour).Format(http.TimeFormat), now))
}

func TestRenewalPointAndFallback(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 22, 0, 0, 0, time.UTC)
	leaf := testCertificate(t, now.Add(90*24*time.Hour)).Leaf

	require.Equal(t, now, renewalPoint(now, now, leaf), "an empty window renews at its start")
	require.Equal(t, leaf.NotBefore.Add(60*24*time.Hour), fallbackRenewAt(leaf), "two thirds through a 90-day certificate")

	other := testCertificate(t, now.Add(90*24*time.Hour)).Leaf
	spread := map[time.Time]bool{renewalPoint(now, now.Add(48*time.Hour), leaf): true, renewalPoint(now, now.Add(48*time.Hour), other): true}
	require.Len(t, spread, 2, "different certificates renew at different moments in a window")
}

func TestJitteredStaysWithinATenth(t *testing.T) {
	t.Parallel()
	for range 1000 {
		d := jittered(time.Hour)
		require.GreaterOrEqual(t, d, 54*time.Minute)
		require.LessOrEqual(t, d, 66*time.Minute)
	}
}

package assistants

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestWaitKitLocalHelperRetriesTransient503AndValidatesInitVersion(t *testing.T) {
	t.Parallel()
	client := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).Client(
		guardian.WithAllowedCIDRBlocks("127.0.0.1/32"),
	)

	t.Run("retries 503 until 200 with v2 header", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set(ateInitVersionHeader, strconv.Itoa(ateInitWireVersion))
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		require.NoError(t, waitKitLocalHelper(t.Context(), client, srv.URL+"/readyz", 5*time.Millisecond))
		require.EqualValues(t, 3, attempts.Load())
	})

	t.Run("rejects wrong init version header", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(ateInitVersionHeader, "1")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		require.Error(t, waitKitLocalHelper(t.Context(), client, srv.URL+"/readyz", 5*time.Millisecond))
	})

	t.Run("times out on persistent 503", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)
		defer cancel()
		err := waitKitLocalHelper(ctx, client, srv.URL+"/app-readyz", 10*time.Millisecond)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestKitLocalPrepareThreadStopsContainerOnHelperFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		readyStatus    int
		bootstrapFail  bool
		appReadyStatus int
		wantErr        string
	}{
		{name: "readyz-500", readyStatus: http.StatusInternalServerError, appReadyStatus: http.StatusOK, wantErr: "wait for kit local init helper"},
		{name: "bootstrap-500", readyStatus: http.StatusOK, bootstrapFail: true, appReadyStatus: http.StatusOK, wantErr: "kit local bootstrap rejected"},
		{name: "app-readyz-500", readyStatus: http.StatusOK, appReadyStatus: http.StatusInternalServerError, wantErr: "wait for kit local ACP server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k, engine, r, _ := kitLocalFixture(t)
			ensured, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = ensured.BackendMetadataJSON

			engine.newHelper = func(t *testing.T) *fakeKitLocalInitHelper {
				t.Helper()
				h := newFakeKitLocalInitHelper(t)
				h.readyStatus = tc.readyStatus
				h.appReadyStatus = tc.appReadyStatus
				if tc.bootstrapFail {
					h.bootstrapStatus = http.StatusInternalServerError
				}
				return h
			}
			threadID := uuid.New()
			err = k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID})
			require.ErrorContains(t, err, tc.wantErr)
			name, _ := kitLocalThreadNames(r, threadID)
			require.False(t, engine.containers[name].Running)
			require.Contains(t, engine.calls, "stop")
		})
	}
}

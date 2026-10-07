package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gcs "cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

func TestGCSStore_CreateOnlyAndDurableClose(t *testing.T) {
	t.Parallel()
	for _, commitStatus := range []int{http.StatusOK, http.StatusPreconditionFailed} {
		t.Run(fmt.Sprint(commitStatus), func(t *testing.T) {
			t.Parallel()
			var commits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPost:
					require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
					require.Equal(t, "multipart", r.URL.Query().Get("uploadType"))
					_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
					require.NoError(t, err)
					parts := multipart.NewReader(r.Body, params["boundary"])
					_, err = parts.NextPart()
					require.NoError(t, err)
					body, err := parts.NextPart()
					require.NoError(t, err)
					commits.Add(1)
					data, err := io.ReadAll(body)
					require.NoError(t, err)
					require.Equal(t, "complete parquet footer", string(data))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(commitStatus)
					if commitStatus == http.StatusOK {
						_, _ = io.WriteString(w, `{"bucket":"test-bucket","name":"object.parquet","generation":"1","size":"23"}`)
					} else {
						_, _ = io.WriteString(w, `{"error":{"code":412,"message":"object already exists"}}`)
					}
				default:
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			client, err := gcs.NewClient(t.Context(), option.WithEndpoint(server.URL), option.WithoutAuthentication())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			store := &GCSStore{Client: client}
			err = store.Write(t.Context(), Object{Bucket: "test-bucket", Name: "object.parquet"}, func(w io.Writer) error { _, err := io.WriteString(w, "complete parquet footer"); return err })
			if commitStatus == http.StatusOK {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "commit storage object")
			}
			require.Equal(t, int32(1), commits.Load())
		})
	}
}

func TestGCSStore_AbortsFailedEncoder(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not commit", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	client, err := gcs.NewClient(t.Context(), option.WithEndpoint(server.URL), option.WithoutAuthentication())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store := &GCSStore{Client: client}
	err = store.Write(context.WithoutCancel(t.Context()), Object{Bucket: "test-bucket", Name: "aborted.parquet"}, func(w io.Writer) error {
		_, err := io.WriteString(w, "incomplete")
		if err != nil {
			return err
		}
		return errors.New("encoder failed")
	})
	require.ErrorContains(t, err, "encoder failed")
	require.Zero(t, requests.Load(), "a failed small encoding must never publish even a partial object")
}

package auth_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
)

func newEncryptionClient(t *testing.T) *encryption.Client {
	t.Helper()

	key, err := base64.StdEncoding.DecodeString("dGVzdC1rZXktMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM=")
	require.NoError(t, err)

	enc, err := encryption.New(key)
	require.NoError(t, err)
	return enc
}

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func generateValidV1Token(t *testing.T, enc *encryption.Client, id string, expTime time.Time) string {
	t.Helper()

	payload := map[string]any{
		"id":  id,
		"exp": expTime.Unix(),
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	token, err := enc.Encrypt(data)
	require.NoError(t, err)

	return fmt.Sprintf("v01.%s", token)
}

func TestAuthorizeRequest_Success(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Hour))

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, handlerCalled, "handler should have been called")
}

func TestAuthorizeRequest_NoAuthorizationHeader(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
	require.Contains(t, rec.Body.String(), http.StatusText(http.StatusUnauthorized))
}

func TestAuthorizeRequest_EmptyAuthorizationHeader(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
}

func TestAuthorizeRequest_InvalidAuthorizationHeader_TooShort(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	testCases := []struct {
		name   string
		header string
	}{
		{
			name:   "only bearer prefix",
			header: "bearer ",
		},
		{
			name:   "less than bearer prefix",
			header: "bear",
		},
		{
			name:   "empty string",
			header: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.False(t, handlerCalled, "handler should not have been called")
		})
	}
}

func TestAuthorizeRequest_TokenMissingVersionPrefix(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	testCases := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "one character",
			token: "a",
		},
		{
			name:  "two characters",
			token: "ab",
		},
		{
			name:  "three characters",
			token: "abc",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "bearer "+tc.token)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.False(t, handlerCalled, "handler should not have been called")
		})
	}
}

func TestAuthorizeRequest_UnsupportedTokenVersion(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer v99.sometoken")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
}

func TestAuthorizeRequest_InvalidToken_DecryptionFailure(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer invalid-token-that-cannot-decrypt")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
}

func TestAuthorizeRequest_ExpiredToken(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(-1*time.Hour))

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
}

func TestAuthorizeRequest_MissingID(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	payload := map[string]any{
		"id":  "",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	token, err := enc.Encrypt(data)
	require.NoError(t, err)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
}

func TestAuthorizeRequest_InvalidJSON(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token, err := enc.Encrypt([]byte("not-valid-json{{{"))
	require.NoError(t, err)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, handlerCalled, "handler should not have been called")
}

func TestAuthorizeRequest_CaseInsensitiveBearer(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Hour))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	testCases := []struct {
		name   string
		prefix string
	}{
		{
			name:   "lowercase bearer",
			prefix: "bearer ",
		},
		{
			name:   "uppercase BEARER",
			prefix: "BEARER ",
		},
		{
			name:   "mixed case Bearer",
			prefix: "Bearer ",
		},
		{
			name:   "mixed case BeArEr",
			prefix: "BeArEr ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", tc.prefix+token)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestAuthorizeRequest_ContextPropagation(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Hour))

	type contextKey string
	const testKey contextKey = "testKey"
	const testValue = "testValue"

	var receivedCtx context.Context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	ctx := context.WithValue(context.Background(), testKey, testValue)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, receivedCtx)
	require.Equal(t, testValue, receivedCtx.Value(testKey))
}

func TestAuthorizeRequest_MultipleRequests(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Hour))

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	for range 5 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "bearer "+token)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	}

	require.Equal(t, 5, callCount, "handler should have been called 5 times")
}

func TestAuthorizeRequest_DifferentHTTPMethods(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Hour))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead,
		http.MethodOptions,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(method, "/test", nil)
			req.Header.Set("Authorization", "bearer "+token)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestAuthorizeRequest_TokenAtExpiryBoundary(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	t.Run("token expires in 1 second", func(t *testing.T) {
		t.Parallel()

		token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Second))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "bearer "+token)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("token expired 1 second ago", func(t *testing.T) {
		t.Parallel()

		token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(-1*time.Second))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "bearer "+token)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestAuthorizeRequest_ResponseHeaders(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "success")
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	token := generateValidV1Token(t, enc, "test-id-123", time.Now().Add(1*time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "custom-value", rec.Header().Get("X-Custom-Header"))
	require.Equal(t, "success", rec.Body.String())
}

func TestAuthorizeRequest_UnauthorizedResponseFormat(t *testing.T) {
	t.Parallel()

	enc := newEncryptionClient(t)
	logger := newTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	middleware := auth.AuthorizeRequest(logger, enc, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Contains(t, rec.Body.String(), http.StatusText(http.StatusUnauthorized))
}

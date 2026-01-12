package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
)

func TestHandleGetServer_ContentNegotiation(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPService(t)

	// Create metadata service using the same dependencies
	metadataService := mcpmetadata.NewService(
		testInstance.logger,
		testInstance.conn,
		testInstance.sessionManager,
		testInstance.serverURL,
		testInstance.siteURL,
		testInstance.cacheAdapter,
	)

	tests := []struct {
		name                string
		acceptHeader        string
		expectedStatusCode  int
		expectedContentType string
		shouldReturnJSON    bool
	}{
		{
			name:                "no accept header returns 405 JSON",
			acceptHeader:        "",
			expectedStatusCode:  http.StatusMethodNotAllowed,
			expectedContentType: "application/json",
			shouldReturnJSON:    true,
		},
		{
			name:                "application/json accept header returns 405 JSON",
			acceptHeader:        "application/json",
			expectedStatusCode:  http.StatusMethodNotAllowed,
			expectedContentType: "application/json",
			shouldReturnJSON:    true,
		},
		{
			name:                "text/html accept header delegates to metadata service",
			acceptHeader:        "text/html",
			expectedStatusCode:  0, // We won't check status for HTML since metadata service behavior varies
			expectedContentType: "",
			shouldReturnJSON:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a test request
			req := httptest.NewRequest("GET", "/mcp/test-slug", nil)
			req = req.WithContext(ctx)

			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}

			// Add URL param for mcpSlug
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("mcpSlug", "test-slug")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			// Create a response recorder
			rr := httptest.NewRecorder()

			// Call the handler
			err := testInstance.service.HandleGetServer(rr, req, metadataService)

			if tt.shouldReturnJSON {
				// For JSON responses, we expect the handler to write the response and return nil (success)
				require.NoError(t, err, "Expected no error for successful JSON response")
				assert.Equal(t, tt.expectedStatusCode, rr.Code)
				assert.Equal(t, tt.expectedContentType, rr.Header().Get("Content-Type"))

				// Verify it's valid JSON with the expected structure
				var response struct {
					ID      interface{} `json:"id"`
					Code    int         `json:"code"`
					Message string      `json:"message"`
					Data    interface{} `json:"data"`
				}
				unmarshalErr := json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, unmarshalErr, "Response should be valid JSON")
				assert.Equal(t, -32000, response.Code) // methodNotAllowed errorCode value
				assert.NotEqual(t, "", response.Message)
			} else {
				// For HTML responses, we expect delegation to metadata service
				// The key test here is content negotiation - we should NOT return JSON
				assert.NotEqual(t, "application/json", rr.Header().Get("Content-Type"))

				// The metadata service will likely error because "test-slug" doesn't exist in the test DB,
				// but that's expected - we're testing content negotiation, not data retrieval.
				// The important thing is that we took the HTML delegation path instead of JSON error path.
				if err != nil {
					t.Logf("Expected metadata service error for non-existent slug: %v", err)
				} else {
					t.Log("Metadata service successfully handled HTML request")
				}
			}
		})
	}
}

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var funcs functions.ToolCaller

func newTestToolDescriptor() *ToolDescriptor {
	return &ToolDescriptor{
		ID:               uuid.New().String(),
		Name:             "test_tool",
		Description:      conv.Ptr("test_tool_description"),
		DeploymentID:     uuid.New().String(),
		ProjectID:        uuid.New().String(),
		ProjectSlug:      "test-project",
		OrganizationID:   uuid.New().String(),
		OrganizationSlug: "test-org",
		URN:              urn.NewTool(urn.ToolKindHTTP, "doc", "test_tool"),
	}
}

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	res, cleanup, err := testenv.Launch(ctx)
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err = cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func TestToolProxy_Do_PathParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		pathTemplate  string
		pathParam     map[string]any
		expectedPath  string
		expectedError bool
	}{
		{
			name:          "integer path param",
			pathTemplate:  "/users/{id}",
			pathParam:     map[string]any{"id": 123},
			expectedPath:  "/users/123",
			expectedError: false,
		},
		{
			name:          "float path param",
			pathTemplate:  "/products/{price}",
			pathParam:     map[string]any{"price": 19.99},
			expectedPath:  "/products/19.99",
			expectedError: false,
		},
		{
			name:          "large integer path param",
			pathTemplate:  "/orders/{orderId}",
			pathParam:     map[string]any{"orderId": 9007199254740991}, // will break without json.Number decoding
			expectedPath:  "/orders/9007199254740991",
			expectedError: false,
		},
		{
			name:          "very large integer as string",
			pathTemplate:  "/bignum/{value}",
			pathParam:     map[string]any{"value": "99999999999999999999999999999999"},
			expectedPath:  "/bignum/99999999999999999999999999999999",
			expectedError: false,
		},
		{
			name:          "negative integer path param",
			pathTemplate:  "/temperature/{temp}",
			pathParam:     map[string]any{"temp": -15},
			expectedPath:  "/temperature/-15",
			expectedError: false,
		},
		{
			name:          "negative float path param",
			pathTemplate:  "/delta/{value}",
			pathParam:     map[string]any{"value": -15.5},
			expectedPath:  "/delta/-15.5",
			expectedError: false,
		},
		{
			name:          "zero path param",
			pathTemplate:  "/count/{num}",
			pathParam:     map[string]any{"num": 0},
			expectedPath:  "/count/0",
			expectedError: false,
		},
		{
			name:          "zero integer",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": 0},
			expectedPath:  "/data/0",
			expectedError: false,
		},
		{
			name:          "max safe integer",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": 9007199254740991}, // will break without json.Number decoding
			expectedPath:  "/data/9007199254740991",
			expectedError: false,
		},
		{
			name:          "beyond safe integer as string",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": "9007199254740992"},
			expectedPath:  "/data/9007199254740992",
			expectedError: false,
		},
		{
			name:          "very large number as string",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": "999999999999999999999999999999"},
			expectedPath:  "/data/999999999999999999999999999999",
			expectedError: false,
		},
		{
			name:          "high precision decimal as string",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": "3.141592653589793238462643383279"},
			expectedPath:  "/data/3.141592653589793238462643383279",
			expectedError: false,
		},
		{
			name:          "scientific notation as string",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": "2.5E+3"},
			expectedPath:  "/data/2.5E+3",
			expectedError: false,
		},
		{
			name:          "regular float",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": 3.14159},
			expectedPath:  "/data/3.14159",
			expectedError: false,
		},
		{
			name:          "negative integer",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": -42},
			expectedPath:  "/data/-42",
			expectedError: false,
		},
		{
			name:          "negative float",
			pathTemplate:  "/data/{value}",
			pathParam:     map[string]any{"value": -3.14},
			expectedPath:  "/data/-3.14",
			expectedError: false,
		},
		{
			name:          "mixed path params",
			pathTemplate:  "/users/{userId}/orders/{orderId}",
			pathParam:     map[string]any{"userId": 123, "orderId": 456789},
			expectedPath:  "/users/123/orders/456789",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock server that captures the request
			var capturedRequest *http.Request
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success": true}`))
			}))
			defer mockServer.Close()

			// Setup test dependencies
			ctx := context.Background()
			logger := testenv.NewLogger(t)
			tracerProvider := testenv.NewTracerProvider(t)
			meterProvider := testenv.NewMeterProvider(t)
			enc := testenv.NewEncryptionClient(t)
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			tool := newTestToolDescriptor()
			// Create plan with path parameter configuration
			plan := &HTTPToolCallPlan{
				ServerEnvVar:       "TEST_SERVER_URL",
				DefaultServerUrl:   NullString{Value: mockServer.URL, Valid: true},
				Security:           []*HTTPToolSecurity{},
				SecurityScopes:     map[string][]string{},
				Method:             "GET",
				Path:               tt.pathTemplate,
				Schema:             []byte{},
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams:        map[string]*HTTPParameter{},
				PathParams:         map[string]*HTTPParameter{},
				RequestContentType: NullString{Value: "application/json", Valid: true},
				ResponseFilter:     nil,
			}

			// Add path parameter configuration for the parameter in the test
			for paramName := range tt.pathParam {
				plan.PathParams[paramName] = &HTTPParameter{
					Name:            paramName,
					Style:           "simple",
					Explode:         conv.Ptr(false),
					AllowEmptyValue: false,
				}
			}

			// Create request body with path parameters
			requestBody := ToolCallBody{
				PathParameters:       tt.pathParam,
				QueryParameters:      nil,
				HeaderParameters:     nil,
				Body:                 nil,
				ResponseFilter:       nil,
				EnvironmentVariables: nil,
				GramRequestSummary:   "",
			}

			bodyBytes, err := json.Marshal(requestBody)
			require.NoError(t, err)

			// Create tool proxy
			proxy := NewToolProxy(
				logger,
				tracerProvider,
				meterProvider,
				ToolCallSourceDirect,
				enc,
				nil, // no cache needed for this test
				policy,
				funcs,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			ciEnv := NewCaseInsensitiveEnv()
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
				SystemEnv:  NewCaseInsensitiveEnv(),
				UserConfig: ciEnv,
			}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

			if tt.expectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Verify the path was correctly constructed with the number
			require.Equal(t, tt.expectedPath, capturedRequest.URL.Path)
		})
	}
}

func TestToolProxy_Do_HeaderParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		headerParam    map[string]any
		expectedHeader string
		expectedError  bool
	}{
		{
			name:           "string header param",
			headerParam:    map[string]any{"Authorization": "Bearer token123"},
			expectedHeader: "Bearer token123",
			expectedError:  false,
		},
		{
			name:           "large integer header param",
			headerParam:    map[string]any{"X-Order-ID": 9007199254740991},
			expectedHeader: "9007199254740991",
			expectedError:  false,
		},
		{
			name:           "float header param",
			headerParam:    map[string]any{"X-Rating": 4.5},
			expectedHeader: "4.5",
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock server that captures the request headers
			var capturedRequest *http.Request
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success": true}`))
			}))
			defer mockServer.Close()

			// Setup test dependencies
			ctx := context.Background()
			logger := testenv.NewLogger(t)
			tracerProvider := testenv.NewTracerProvider(t)
			meterProvider := testenv.NewMeterProvider(t)
			enc := testenv.NewEncryptionClient(t)
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			tool := newTestToolDescriptor()
			// Create plan with header parameter configuration
			plan := &HTTPToolCallPlan{
				ServerEnvVar:       "TEST_SERVER_URL",
				DefaultServerUrl:   NullString{Value: mockServer.URL, Valid: true},
				Security:           []*HTTPToolSecurity{},
				SecurityScopes:     map[string][]string{},
				Method:             "GET",
				Path:               "/test",
				Schema:             []byte{},
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams:        map[string]*HTTPParameter{},
				PathParams:         map[string]*HTTPParameter{},
				RequestContentType: NullString{Value: "application/json", Valid: true},
				ResponseFilter:     nil,
			}

			// Add header parameter configuration for the parameter in the test
			for paramName := range tt.headerParam {
				plan.HeaderParams[paramName] = &HTTPParameter{
					Name:            paramName,
					Style:           "simple",
					Explode:         conv.Ptr(false),
					AllowEmptyValue: true,
				}
			}

			// Create request body with header parameters
			requestBody := ToolCallBody{
				PathParameters:       nil,
				QueryParameters:      nil,
				HeaderParameters:     tt.headerParam,
				Body:                 nil,
				ResponseFilter:       nil,
				EnvironmentVariables: nil,
				GramRequestSummary:   "",
			}

			bodyBytes, err := json.Marshal(requestBody)
			require.NoError(t, err)

			// Create tool proxy
			proxy := NewToolProxy(
				logger,
				tracerProvider,
				meterProvider,
				ToolCallSourceDirect,
				enc,
				nil, // no cache needed for this test
				policy,
				funcs,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			ciEnv := NewCaseInsensitiveEnv()
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
				SystemEnv:  NewCaseInsensitiveEnv(),
				UserConfig: ciEnv,
			}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

			if tt.expectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Verify the header was correctly set with the expected value
			for headerName := range tt.headerParam {
				actualHeaderValue := capturedRequest.Header.Get(headerName)
				require.Equal(t, tt.expectedHeader, actualHeaderValue, "header %s value mismatch", headerName)
			}
		})
	}
}

func TestToolProxy_Do_QueryParams(t *testing.T) {
	t.Parallel()

	// Test timestamp in RFC3339Nano format
	testTime := time.Date(2023, 12, 25, 15, 30, 45, 123456789, time.UTC)
	timestampStr := testTime.Format(time.RFC3339Nano)

	tests := []struct {
		name            string
		queryParams     map[string]any
		paramSettings   map[string]*HTTPParameter
		expectedQueries url.Values
	}{
		{
			name: "integer query param",
			queryParams: map[string]any{
				"page": 1,
			},
			paramSettings: map[string]*HTTPParameter{
				"page": {
					Name:            "page",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"page": []string{"1"},
			},
		},
		{
			name: "float query param",
			queryParams: map[string]any{
				"price": 19.99,
			},
			paramSettings: map[string]*HTTPParameter{
				"price": {
					Name:            "price",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"price": []string{"19.99"},
			},
		},
		{
			name: "multiple number query params",
			queryParams: map[string]any{
				"min":  10,
				"max":  100,
				"rate": 0.05,
			},
			paramSettings: map[string]*HTTPParameter{
				"min": {
					Name:            "min",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"max": {
					Name:            "max",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"rate": {
					Name:            "rate",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"min":  []string{"10"},
				"max":  []string{"100"},
				"rate": []string{"0.05"},
			},
		},
		{
			name: "large integer query param", // will break without json.Number decoding
			queryParams: map[string]any{
				"timestamp": 9007199254740991,
			},
			paramSettings: map[string]*HTTPParameter{
				"timestamp": {
					Name:            "timestamp",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"timestamp": []string{"9007199254740991"},
			},
		},
		{
			name: "scientific notation query param",
			queryParams: map[string]any{
				"value": "1.23e10",
			},
			paramSettings: map[string]*HTTPParameter{
				"value": {
					Name:            "value",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"value": []string{"1.23e10"},
			},
		},
		{
			name: "negative number query param",
			queryParams: map[string]any{
				"offset": -5,
			},
			paramSettings: map[string]*HTTPParameter{
				"offset": {
					Name:            "offset",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"offset": []string{"-5"},
			},
		},
		{
			name: "decimal with trailing zeros",
			queryParams: map[string]any{
				"amount": "50.00",
			},
			paramSettings: map[string]*HTTPParameter{
				"amount": {
					Name:            "amount",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"amount": []string{"50.00"},
			},
		},
		{
			name: "string query params",
			queryParams: map[string]any{
				"name":     "john doe",
				"category": "electronics",
				"status":   "active",
			},
			paramSettings: map[string]*HTTPParameter{
				"name": {
					Name:            "name",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"category": {
					Name:            "category",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"status": {
					Name:            "status",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"name":     []string{"john doe"},
				"category": []string{"electronics"},
				"status":   []string{"active"},
			},
		},
		{
			name: "timestamp query param",
			queryParams: map[string]any{
				"created_at": timestampStr,
				"expires":    "2024-01-01T00:00:00.000000000Z",
			},
			paramSettings: map[string]*HTTPParameter{
				"created_at": {
					Name:            "created_at",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"expires": {
					Name:            "expires",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"created_at": []string{timestampStr},
				"expires":    []string{"2024-01-01T00:00:00.000000000Z"},
			},
		},
		{
			name: "mixed strings, numbers and timestamps",
			queryParams: map[string]any{
				"id":         12345,
				"name":       "test user",
				"created_at": timestampStr,
				"price":      99.99,
				"active":     "true",
			},
			paramSettings: map[string]*HTTPParameter{
				"id": {
					Name:            "id",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"name": {
					Name:            "name",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"created_at": {
					Name:            "created_at",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"price": {
					Name:            "price",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
				"active": {
					Name:            "active",
					Style:           "form",
					Explode:         conv.Ptr(true),
					AllowEmptyValue: false,
				},
			},
			expectedQueries: url.Values{
				"id":         []string{"12345"},
				"name":       []string{"test user"},
				"created_at": []string{timestampStr},
				"price":      []string{"99.99"},
				"active":     []string{"true"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock server that captures the request
			var capturedRequest *http.Request
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success": true}`))
			}))
			defer mockServer.Close()

			// Setup test dependencies
			ctx := context.Background()
			logger := testenv.NewLogger(t)
			tracerProvider := testenv.NewTracerProvider(t)
			meterProvider := testenv.NewMeterProvider(t)
			enc := testenv.NewEncryptionClient(t)
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			tool := newTestToolDescriptor()
			// Create plan with query parameter configuration
			plan := &HTTPToolCallPlan{
				ServerEnvVar:       "TEST_SERVER_URL",
				DefaultServerUrl:   NullString{Value: mockServer.URL, Valid: true},
				Security:           []*HTTPToolSecurity{},
				SecurityScopes:     map[string][]string{},
				Method:             "GET",
				Path:               "/api/data",
				Schema:             []byte{},
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams:        tt.paramSettings,
				PathParams:         map[string]*HTTPParameter{},
				RequestContentType: NullString{Value: "application/json", Valid: true},
				ResponseFilter:     nil,
			}

			// Create request body with query parameters
			requestBody := ToolCallBody{
				PathParameters:       nil,
				QueryParameters:      tt.queryParams,
				HeaderParameters:     nil,
				Body:                 nil,
				ResponseFilter:       nil,
				EnvironmentVariables: nil,
				GramRequestSummary:   "",
			}

			bodyBytes, err := json.Marshal(requestBody)
			require.NoError(t, err)

			// Create tool proxy
			proxy := NewToolProxy(
				logger,
				tracerProvider,
				meterProvider,
				ToolCallSourceDirect,
				enc,
				nil, // no cache needed for this test
				policy,
				funcs,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			ciEnv := NewCaseInsensitiveEnv()
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
				SystemEnv:  NewCaseInsensitiveEnv(),
				UserConfig: ciEnv,
			}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())
			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Parse the captured query parameters
			actualQueries := capturedRequest.URL.Query()

			// Verify each expected query parameter
			for expectedKey, expectedValues := range tt.expectedQueries {
				actualValues, exists := actualQueries[expectedKey]
				require.True(t, exists, "expected query parameter %s not found", expectedKey)
				require.Equal(t, expectedValues, actualValues, "query parameter %s has incorrect values", expectedKey)
			}
		})
	}
}

func TestToolProxy_Do_Body(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requestBody  map[string]any
		contentType  string
		expectedBody string
		expectedForm url.Values
	}{
		{
			name: "simple JSON body",
			requestBody: map[string]any{
				"name":  "John Doe",
				"email": "john@example.com",
				"age":   30,
			},
			contentType:  "application/json",
			expectedBody: `{"name":"John Doe","email":"john@example.com","age":30}`,
			expectedForm: nil,
		},
		{
			name: "complex nested JSON body",
			requestBody: map[string]any{
				"user": map[string]any{
					"id":   123,
					"name": "Jane Smith",
					"metadata": map[string]any{
						"created_at": "2023-12-25T15:30:45.123456789Z",
						"tags":       []string{"vip", "premium"},
						"score":      95.5,
					},
				},
				"settings": map[string]any{
					"notifications": true,
					"theme":         "dark",
				},
			},
			contentType:  "application/json",
			expectedBody: `{"user":{"id":123,"name":"Jane Smith","metadata":{"created_at":"2023-12-25T15:30:45.123456789Z","tags":["vip","premium"],"score":95.5}},"settings":{"notifications":true,"theme":"dark"}}`,
			expectedForm: nil,
		},
		{
			name: "body with numbers and precision",
			requestBody: map[string]any{
				"id":           9007199254740991,
				"price":        19.99,
				"quantity":     100,
				"discount":     0.15,
				"large_number": "99999999999999999999999999",
			},
			contentType:  "application/json",
			expectedBody: `{"id":9007199254740991,"price":19.99,"quantity":100,"discount":0.15,"large_number":"99999999999999999999999999"}`,
			expectedForm: nil,
		},
		{
			name:         "empty JSON body",
			requestBody:  map[string]any{},
			contentType:  "application/json",
			expectedBody: `{}`,
			expectedForm: nil,
		},
		{
			name: "simple form data",
			requestBody: map[string]any{
				"name":  "John Doe",
				"email": "john@example.com",
				"age":   "30",
			},
			contentType:  "application/x-www-form-urlencoded",
			expectedBody: "",
			expectedForm: url.Values{
				"name":  []string{"John Doe"},
				"email": []string{"john@example.com"},
				"age":   []string{"30"},
			},
		},
		{
			name: "form data with numbers",
			requestBody: map[string]any{
				"id":       123,
				"price":    19.99,
				"quantity": 5,
			},
			contentType:  "application/x-www-form-urlencoded",
			expectedBody: "",
			expectedForm: url.Values{
				"id":       []string{"123"},
				"price":    []string{"19.99"},
				"quantity": []string{"5"},
			},
		},
		{
			name: "form data with arrays",
			requestBody: map[string]any{
				"tags":  []string{"tag1", "tag2", "tag3"},
				"items": []int{1, 2, 3},
			},
			contentType:  "application/x-www-form-urlencoded",
			expectedBody: "",
			expectedForm: url.Values{
				"tags[0]":  []string{"tag1"},
				"tags[1]":  []string{"tag2"},
				"tags[2]":  []string{"tag3"},
				"items[0]": []string{"1"},
				"items[1]": []string{"2"},
				"items[2]": []string{"3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock server that captures the request body
			var capturedBody []byte
			var capturedRequest *http.Request
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "failed to read body", http.StatusInternalServerError)
					return
				}
				capturedBody = body
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success": true}`))
			}))
			defer mockServer.Close()

			// Setup test dependencies
			ctx := context.Background()
			logger := testenv.NewLogger(t)
			tracerProvider := testenv.NewTracerProvider(t)
			meterProvider := testenv.NewMeterProvider(t)
			enc := testenv.NewEncryptionClient(t)
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			// Create tool configuration
			var path string
			if tt.contentType == "application/x-www-form-urlencoded" {
				path = "/api/form"
			} else {
				path = "/api/users"
			}

			tool := newTestToolDescriptor()
			plan := &HTTPToolCallPlan{
				ServerEnvVar:       "TEST_SERVER_URL",
				DefaultServerUrl:   NullString{Value: mockServer.URL, Valid: true},
				Security:           []*HTTPToolSecurity{},
				SecurityScopes:     map[string][]string{},
				Method:             "POST",
				Path:               path,
				Schema:             []byte{},
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams:        map[string]*HTTPParameter{},
				PathParams:         map[string]*HTTPParameter{},
				RequestContentType: NullString{Value: tt.contentType, Valid: true},
				ResponseFilter:     nil,
			}

			// Marshal the test request body
			bodyJSON, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			// Create request body for the tool call
			toolCallBody := ToolCallBody{
				PathParameters:       nil,
				QueryParameters:      nil,
				HeaderParameters:     nil,
				Body:                 json.RawMessage(bodyJSON),
				ResponseFilter:       nil,
				EnvironmentVariables: nil,
				GramRequestSummary:   "",
			}

			toolCallBodyBytes, err := json.Marshal(toolCallBody)
			require.NoError(t, err)

			// Create tool proxy
			proxy := NewToolProxy(
				logger,
				tracerProvider,
				meterProvider,
				ToolCallSourceDirect,
				enc,
				nil, // no cache needed for this test
				policy,
				funcs,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			ciEnv := NewCaseInsensitiveEnv()
			err = proxy.Do(ctx, recorder, bytes.NewReader(toolCallBodyBytes), ToolCallEnv{
				SystemEnv:  NewCaseInsensitiveEnv(),
				UserConfig: ciEnv,
			}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())
			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Verify content type header
			require.Equal(t, tt.contentType, capturedRequest.Header.Get("Content-Type"))

			if tt.contentType == "application/x-www-form-urlencoded" {
				// Parse the captured form data
				actualFormData, err := url.ParseQuery(string(capturedBody))
				require.NoError(t, err)

				// Verify each expected form field
				for expectedKey, expectedValues := range tt.expectedForm {
					actualValues, exists := actualFormData[expectedKey]
					require.True(t, exists, "expected form field %s not found", expectedKey)
					require.Equal(t, expectedValues, actualValues, "form field %s has incorrect values", expectedKey)
				}
			} else {
				// Verify the request body was passed through correctly by unmarshaling both and comparing
				var expectedJSON, actualJSON map[string]any
				err = json.Unmarshal([]byte(tt.expectedBody), &expectedJSON)
				require.NoError(t, err)
				err = json.Unmarshal(capturedBody, &actualJSON)
				require.NoError(t, err)
				require.Equal(t, expectedJSON, actualJSON)
			}

		})
	}
}

func TestToolProxy_Do_StringifiedJSONBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		toolCallBody   string
		schema         string
		expectedBody   map[string]any
		expectError    bool
		shouldValidate bool
	}{
		{
			name: "nested stringified JSON",
			toolCallBody: `{
				"body": {
					"benefitFields": {
						"primaryFields": "{\"name\": \"Super-Duper Benefit\", \"description\": \"A benefit that is super-duper.\", \"type\": \"special\", \"category\": \"miscellaneous\"}"
					}
				}
			}`,
			schema: `{
				"type": "object",
				"properties": {
					"body": {
						"type": "object",
						"properties": {
							"benefitFields": {
								"type": "object",
								"properties": {
									"primaryFields": {
										"type": "object",
										"properties": {
											"name": {"type": "string"},
											"description": {"type": "string"},
											"type": {"type": "string"},
											"category": {"type": "string"}
										}
									}
								}
							}
						}
					}
				}
			}`,
			expectedBody: map[string]any{
				"benefitFields": map[string]any{
					"primaryFields": map[string]any{
						"name":        "Super-Duper Benefit",
						"description": "A benefit that is super-duper.",
						"type":        "special",
						"category":    "miscellaneous",
					},
				},
			},
			expectError:    false,
			shouldValidate: true,
		},
		{
			name: "top-level stringified JSON",
			toolCallBody: `{
				"body": "{\"type\": \"custom\", \"description\": \"Ryan\", \"properties\": {}}"
			}`,
			schema: `{
				"type": "object",
				"properties": {
					"body": {
						"type": "object",
						"properties": {
							"type": {"type": "string"},
							"description": {"type": "string"},
							"properties": {"type": "object"}
						}
					}
				}
			}`,
			expectedBody: map[string]any{
				"type":        "custom",
				"description": "Ryan",
				"properties":  map[string]any{},
			},
			expectError:    false,
			shouldValidate: true,
		},
		{
			name: "multiple levels of stringified JSON",
			toolCallBody: `{
				"body": {
					"outer": {
						"middle": "{\"inner\": \"{\\\"deepest\\\": \\\"value\\\"}\"}"
					}
				}
			}`,
			schema: `{
				"type": "object",
				"properties": {
					"body": {
						"type": "object",
						"properties": {
							"outer": {
								"type": "object",
								"properties": {
									"middle": {
										"type": "object",
										"properties": {
											"inner": {
												"type": "object",
												"properties": {
													"deepest": {"type": "string"}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}`,
			expectedBody: map[string]any{
				"outer": map[string]any{
					"middle": map[string]any{
						"inner": map[string]any{
							"deepest": "value",
						},
					},
				},
			},
			expectError:    false,
			shouldValidate: true,
		},
		{
			name: "no schema - body sent as-is",
			toolCallBody: `{
				"body": {"type": "custom", "description": "Ryan"}
			}`,
			schema: "",
			expectedBody: map[string]any{
				"type":        "custom",
				"description": "Ryan",
			},
			expectError:    false,
			shouldValidate: false,
		},
		{
			name: "nested oneOf with stringified JSON - benefitFields case",
			toolCallBody: `{
				"body": {
					"benefitFields": {
						"primaryFields": "{\"name\": \"Super-Duper Benefit\", \"description\": \"A benefit that is super-duper.\", \"type\": \"special\", \"category\": \"miscellaneous\"}"
					}
				}
			}`,
			schema: `{
				"type": "object",
				"properties": {
					"body": {
						"type": "object",
						"properties": {
							"benefitFields": {
								"type": "object",
								"properties": {
									"primaryFields": {
										"oneOf": [
											{
												"type": "object",
												"properties": {
													"name": {"type": "string"},
													"description": {"type": "string"},
													"type": {"type": "string"},
													"category": {"type": "string"}
												},
												"required": ["name"]
											},
											{
												"type": "object",
												"properties": {
													"id": {"type": "string"}
												},
												"required": ["id"]
											}
										]
									}
								}
							}
						}
					}
				}
			}`,
			expectedBody: map[string]any{
				"benefitFields": map[string]any{
					"primaryFields": map[string]any{
						"name":        "Super-Duper Benefit",
						"description": "A benefit that is super-duper.",
						"type":        "special",
						"category":    "miscellaneous",
					},
				},
			},
			expectError:    false,
			shouldValidate: true,
		},
		{
			name: "array with stringified objects",
			toolCallBody: `{
				"body": {
					"items": [
						"{\"id\": 1, \"name\": \"Item 1\"}",
						"{\"id\": 2, \"name\": \"Item 2\"}"
					]
				}
			}`,
			schema: `{
				"type": "object",
				"properties": {
					"body": {
						"type": "object",
						"properties": {
							"items": {
								"type": "array",
								"items": {
									"type": "object",
									"properties": {
										"id": {"type": "number"},
										"name": {"type": "string"}
									}
								}
							}
						}
					}
				}
			}`,
			expectedBody: map[string]any{
				"items": []any{
					map[string]any{"id": float64(1), "name": "Item 1"},
					map[string]any{"id": float64(2), "name": "Item 2"},
				},
			},
			expectError:    false,
			shouldValidate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock server that captures the request body
			var capturedBody []byte
			var capturedRequest *http.Request
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "failed to read body", http.StatusInternalServerError)
					return
				}
				capturedBody = body
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success": true}`))
			}))
			defer mockServer.Close()

			// Setup test dependencies
			ctx := context.Background()
			logger := testenv.NewLogger(t)
			tracerProvider := testenv.NewTracerProvider(t)
			meterProvider := testenv.NewMeterProvider(t)
			enc := testenv.NewEncryptionClient(t)
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			tool := newTestToolDescriptor()
			// Create plan configuration
			plan := &HTTPToolCallPlan{
				ServerEnvVar:       "TEST_SERVER_URL",
				DefaultServerUrl:   NullString{Value: mockServer.URL, Valid: true},
				Security:           []*HTTPToolSecurity{},
				SecurityScopes:     map[string][]string{},
				Method:             "POST",
				Path:               "/api/test",
				Schema:             []byte(tt.schema),
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams:        map[string]*HTTPParameter{},
				PathParams:         map[string]*HTTPParameter{},
				RequestContentType: NullString{Value: "application/json", Valid: true},
				ResponseFilter:     nil,
			}
			// Create tool proxy
			proxy := NewToolProxy(
				logger,
				tracerProvider,
				meterProvider,
				ToolCallSourceDirect,
				enc,
				nil,
				policy,
				funcs,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			ciEnv := NewCaseInsensitiveEnv()
			err = proxy.Do(ctx, recorder, bytes.NewReader([]byte(tt.toolCallBody)), ToolCallEnv{
				SystemEnv:  NewCaseInsensitiveEnv(),
				UserConfig: ciEnv,
			}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Verify the body was correctly de-stringified
			var actualBody map[string]any
			err = json.Unmarshal(capturedBody, &actualBody)
			require.NoError(t, err)

			// Compare the expected body structure
			require.Equal(t, tt.expectedBody, actualBody)
		})
	}
}

func TestResourceProxy_ReadResource(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID

	// Create a mock server that returns a test response
	var capturedRequest *http.Request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Test resource content"))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	// Create resource descriptor
	descriptor := &ResourceDescriptor{
		ID:               uuid.New().String(),
		Name:             "test_resource",
		DeploymentID:     uuid.New().String(),
		ProjectID:        uuid.New().String(),
		ProjectSlug:      "test-project",
		OrganizationID:   uuid.New().String(),
		OrganizationSlug: "test-org",
		URN:              urn.NewResource(urn.ResourceKindFunction, "functions", "resource://test"),
		URI:              "resource://test",
	}

	// Create function resource plan
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &ResourceFunctionCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		URI:               "resource://test",
		MimeType:          "text/plain",
		Variables:         []string{},
	}

	resourcePlan := NewResourceFunctionCallPlan(descriptor, plan)
	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create resource proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Execute the resource read
	ciEnv := NewCaseInsensitiveEnv()
	err = proxy.ReadResource(ctx, recorder, bytes.NewReader([]byte("{}")), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: ciEnv,
	}, resourcePlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedRequest)

	// Verify the response was proxied correctly
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Test resource content", recorder.Body.String())
}

// mockToolCaller is a mock implementation of functions.ToolCaller for testing
type mockToolCaller struct {
	serverURL string
	onCall    func(uuid.UUID)
}

func (m *mockToolCaller) ToolCall(ctx context.Context, req functions.RunnerToolCallRequest) (*http.Request, error) {
	if m.onCall != nil {
		m.onCall(req.InvocationID)
	}

	// Create request payload with environment variables
	payload := map[string]any{
		"environment": req.Environment,
		"input":       req.Input,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", m.serverURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create mock request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	return httpReq, nil
}

func (m *mockToolCaller) ReadResource(ctx context.Context, req functions.RunnerResourceReadRequest) (*http.Request, error) {
	if m.onCall != nil {
		m.onCall(req.InvocationID)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", m.serverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create mock request: %w", err)
	}
	return httpReq, nil
}

func TestToolProxy_Do_FunctionMetricsTrailers(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID

	// Create a mock server that returns trailers for CPU and memory metrics
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set required invocation ID header
		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		// Announce trailers
		w.Header().Set("Trailer", functions.FunctionsCPUHeader+", "+functions.FunctionsMemoryHeader+", "+functions.FunctionsExecutionTimeHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Write response body
		_, _ = w.Write([]byte(`{"result": "success"}`))

		// Set trailer values (after body is written)
		w.Header().Set(functions.FunctionsCPUHeader, "1.23")
		w.Header().Set(functions.FunctionsMemoryHeader, "5678900")
		w.Header().Set(functions.FunctionsExecutionTimeHeader, "2.50")
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan for functions runner path
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput:         nil,
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)
	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Execute the proxy call
	ciEnv := NewCaseInsensitiveEnv()
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: ciEnv,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)

	// Verify trailers were proxied through
	result := recorder.Result()
	require.NotNil(t, result)

	// Check that CPU and memory trailers are present
	cpuValue := result.Trailer.Get(functions.FunctionsCPUHeader)
	memValue := result.Trailer.Get(functions.FunctionsMemoryHeader)

	require.Equal(t, "1.23", cpuValue, "CPU trailer should be proxied through")
	require.Equal(t, "5678900", memValue, "Memory trailer should be proxied through")
}

func TestToolProxy_Do_HTTPTool_UserConfigVariablesSent(t *testing.T) {
	t.Parallel()

	// Create a mock server that captures the request
	var capturedRequest *http.Request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()
	// Create plan that includes API_KEY in security
	plan := &HTTPToolCallPlan{
		ServerEnvVar:     "TEST_SERVER_URL",
		DefaultServerUrl: NullString{Value: mockServer.URL, Valid: true},
		Security: []*HTTPToolSecurity{
			{
				Type:         NullString{Value: "apiKey", Valid: true},
				Scheme:       NullString{Value: "apiKey", Valid: true},
				Name:         NullString{Value: "X-API-Key", Valid: true},
				Placement:    NullString{Value: "header", Valid: true},
				EnvVariables: []string{"API_KEY"},
			},
		},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	}

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 nil,
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		funcs,
	)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with user config containing API_KEY
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("API_KEY", "test-user-api-key")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedRequest)

	// Verify the API key from user config was sent
	require.Equal(t, "test-user-api-key", capturedRequest.Header.Get("X-API-Key"))
}

func TestToolProxy_Do_HTTPTool_UserConfigNotInPlanNotSent(t *testing.T) {
	t.Parallel()

	// Create a mock server that captures the request
	var capturedRequest *http.Request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()
	// Create plan with NO security requirements
	plan := &HTTPToolCallPlan{
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: mockServer.URL, Valid: true},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	}

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 nil,
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		funcs,
	)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with user config containing SECRET_VAR that is NOT in the plan
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("SECRET_VAR", "should-not-be-sent")
	userConfig.Set("ANOTHER_VAR", "also-should-not-be-sent")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedRequest)

	// Verify the variables NOT in the plan were NOT sent
	require.Empty(t, capturedRequest.Header.Get("SECRET_VAR"))
	require.Empty(t, capturedRequest.Header.Get("ANOTHER_VAR"))
}

func TestToolProxy_Do_FunctionTool_UserConfigNotInPlanNotSent(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with specific variables list
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{"ALLOWED_VAR": nil}, // Only ALLOWED_VAR is in the plan
		AuthInput:         nil,
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with user config containing variables
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("ALLOWED_VAR", "this-should-be-sent")
	userConfig.Set("NOT_IN_PLAN", "this-should-not-be-sent")
	userConfig.Set("SECRET_KEY", "also-should-not-be-sent")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify only ALLOWED_VAR was sent (system env keys are uppercase)
	require.Equal(t, "this-should-be-sent", capturedEnvironment["ALLOWED_VAR"])
	require.NotContains(t, capturedEnvironment, "NOT_IN_PLAN")
	require.NotContains(t, capturedEnvironment, "SECRET_KEY")
}

func TestToolProxy_Do_HTTPTool_SystemEnvSentWhenInPlan(t *testing.T) {
	t.Parallel()

	// Create a mock server that captures the request
	var capturedRequest *http.Request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()
	// Create plan that includes SYSTEM_API_KEY in security
	plan := &HTTPToolCallPlan{
		ServerEnvVar:     "TEST_SERVER_URL",
		DefaultServerUrl: NullString{Value: mockServer.URL, Valid: true},
		Security: []*HTTPToolSecurity{
			{
				Type:         NullString{Value: "apiKey", Valid: true},
				Scheme:       NullString{Value: "apiKey", Valid: true},
				Name:         NullString{Value: "X-System-Key", Valid: true},
				Placement:    NullString{Value: "header", Valid: true},
				EnvVariables: []string{"SYSTEM_API_KEY"},
			},
		},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	}

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 nil,
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		funcs,
	)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with system env containing the API key
	systemEnv := NewCaseInsensitiveEnv()
	systemEnv.Set("SYSTEM_API_KEY", "system-secret-key")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  systemEnv,
		UserConfig: NewCaseInsensitiveEnv(),
	}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedRequest)

	// Verify the system API key was sent
	require.Equal(t, "system-secret-key", capturedRequest.Header.Get("X-System-Key"))
}

func TestToolProxy_Do_FunctionTool_SystemEnvSentWhenInPlan(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with specific variables list
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{"SYSTEM_VAR": nil, "DB_PASSWORD": nil},
		AuthInput:         nil,
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with system env containing sensitive variables
	systemEnv := NewCaseInsensitiveEnv()
	systemEnv.Set("SYSTEM_VAR", "system-value")
	systemEnv.Set("DB_PASSWORD", "super-secret-password")
	systemEnv.Set("NOT_IN_PLAN", "should-not-be-sent")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  systemEnv,
		UserConfig: NewCaseInsensitiveEnv(),
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify all system variables were sent (even those not in plan)
	// System env keys are uppercase
	require.Equal(t, "system-value", capturedEnvironment["SYSTEM_VAR"])
	require.Equal(t, "super-secret-password", capturedEnvironment["DB_PASSWORD"])
	require.Equal(t, "should-not-be-sent", capturedEnvironment["NOT_IN_PLAN"])
}

func TestToolProxy_Do_HTTPTool_UserConfigPrefersOverSystemEnv(t *testing.T) {
	t.Parallel()

	// Create a mock server that captures the request
	var capturedRequest *http.Request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()
	// Create plan that includes API_KEY in security
	plan := &HTTPToolCallPlan{
		ServerEnvVar:     "TEST_SERVER_URL",
		DefaultServerUrl: NullString{Value: mockServer.URL, Valid: true},
		Security: []*HTTPToolSecurity{
			{
				Type:         NullString{Value: "apiKey", Valid: true},
				Scheme:       NullString{Value: "apiKey", Valid: true},
				Name:         NullString{Value: "X-API-Key", Valid: true},
				Placement:    NullString{Value: "header", Valid: true},
				EnvVariables: []string{"API_KEY"},
			},
		},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	}

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 nil,
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		funcs,
	)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up BOTH system env and user config with API_KEY
	systemEnv := NewCaseInsensitiveEnv()
	systemEnv.Set("API_KEY", "system-api-key")

	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("API_KEY", "user-override-key")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  systemEnv,
		UserConfig: userConfig,
	}, NewHTTPToolCallPlan(tool, plan), tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedRequest)

	// Verify the USER CONFIG value was used (not system env)
	require.Equal(t, "user-override-key", capturedRequest.Header.Get("X-API-Key"))
}

func TestToolProxy_Do_FunctionTool_UserConfigPrefersOverSystemEnv(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with specific variables list
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{"DATABASE_URL": nil, "API_KEY": nil},
		AuthInput:         nil,
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up BOTH system env and user config with overlapping variables
	systemEnv := NewCaseInsensitiveEnv()
	systemEnv.Set("DATABASE_URL", "postgres://system-db")
	systemEnv.Set("API_KEY", "system-key")

	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("DATABASE_URL", "postgres://user-override-db")
	userConfig.Set("API_KEY", "user-override-key")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  systemEnv,
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify the USER CONFIG values were used (not system env)
	require.Equal(t, "postgres://user-override-db", capturedEnvironment["DATABASE_URL"])
	require.Equal(t, "user-override-key", capturedEnvironment["API_KEY"])
}

func TestToolProxy_Do_FunctionTool_AuthInputSentWhenInUserConfig(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with AuthInput configured
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput: &functions.ManifestAuthInputAttributeV0{
			Type:     "oauth",
			Variable: "OAUTH_TOKEN",
		},
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with user config containing the auth input variable
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("OAUTH_TOKEN", "user-oauth-token-value")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify the auth input variable was sent
	require.Equal(t, "user-oauth-token-value", capturedEnvironment["OAUTH_TOKEN"])
}

func TestToolProxy_Do_FunctionTool_AuthInputNotSentWhenNotInUserConfig(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with AuthInput configured
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput: &functions.ManifestAuthInputAttributeV0{
			Type:     "apiKey",
			Variable: "API_KEY",
		},
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment WITHOUT the auth input variable in user config
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("OTHER_VAR", "some-value")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify the auth input variable was NOT sent
	require.NotContains(t, capturedEnvironment, "API_KEY")
}

func TestToolProxy_Do_FunctionTool_AuthInputPrefersUserConfigOverSystemEnv(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with AuthInput configured
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput: &functions.ManifestAuthInputAttributeV0{
			Type:     "bearer",
			Variable: "BEARER_TOKEN",
		},
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up BOTH system env and user config with the auth input variable
	systemEnv := NewCaseInsensitiveEnv()
	systemEnv.Set("BEARER_TOKEN", "system-bearer-token")

	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("BEARER_TOKEN", "user-bearer-token")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  systemEnv,
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify the USER CONFIG value was used (not system env)
	require.Equal(t, "user-bearer-token", capturedEnvironment["BEARER_TOKEN"])
}

func TestToolProxy_Do_FunctionTool_AuthInputSentWithRegularVariables(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with both Variables and AuthInput configured
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables: map[string]*functions.ManifestVariableAttributeV0{
			"DATABASE_URL": nil,
			"API_KEY":      nil,
		},
		AuthInput: &functions.ManifestAuthInputAttributeV0{
			Type:     "oauth",
			Variable: "OAUTH_TOKEN",
		},
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with both regular variables and auth input variable
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("DATABASE_URL", "postgres://localhost/db")
	userConfig.Set("API_KEY", "regular-api-key")
	userConfig.Set("OAUTH_TOKEN", "oauth-token-value")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify both regular variables and auth input variable were sent
	require.Equal(t, "postgres://localhost/db", capturedEnvironment["DATABASE_URL"])
	require.Equal(t, "regular-api-key", capturedEnvironment["API_KEY"])
	require.Equal(t, "oauth-token-value", capturedEnvironment["OAUTH_TOKEN"])
}

func TestToolProxy_Do_FunctionTool_AuthInputNilNotSent(t *testing.T) {
	t.Parallel()

	// Track the invocation ID that will be generated
	var invocationID uuid.UUID
	var capturedEnvironment map[string]string

	// Create a mock server that captures the function request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to capture environment variables
		bodyBytes, _ := io.ReadAll(r.Body)
		var requestPayload map[string]any
		_ = json.Unmarshal(bodyBytes, &requestPayload)

		if env, ok := requestPayload["environment"].(map[string]any); ok {
			capturedEnvironment = make(map[string]string)
			for k, v := range env {
				if strVal, ok := v.(string); ok {
					capturedEnvironment[k] = strVal
				}
			}
		}

		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer mockServer.Close()

	// Setup test dependencies
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy([]string{})
	require.NoError(t, err)

	tool := newTestToolDescriptor()

	// Create function call plan with AuthInput set to nil
	functionID := uuid.New().String()
	accessID := uuid.New().String()
	plan := &FunctionToolCallPlan{
		FunctionID:        functionID,
		FunctionsAccessID: accessID,
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput:         nil, // Explicitly nil
	}
	toolCallPlan := NewFunctionToolCallPlan(tool, plan)

	// Mock the functions.ToolCaller to return our mock server URL
	mockFuncCaller := &mockToolCaller{
		serverURL: mockServer.URL,
		onCall: func(invID uuid.UUID) {
			invocationID = invID
		},
	}

	// Create tool proxy
	proxy := NewToolProxy(
		logger,
		tracerProvider,
		meterProvider,
		ToolCallSourceDirect,
		enc,
		nil,
		policy,
		mockFuncCaller,
	)

	// Create request body
	requestBody := ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Set up environment with a potential auth input variable
	userConfig := NewCaseInsensitiveEnv()
	userConfig.Set("OAUTH_TOKEN", "should-not-be-sent")

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), ToolCallEnv{
		SystemEnv:  NewCaseInsensitiveEnv(),
		UserConfig: userConfig,
	}, toolCallPlan, tm.NewNoopToolCallLogger())

	require.NoError(t, err)
	require.NotNil(t, capturedEnvironment)

	// Verify the auth input variable was NOT sent when AuthInput is nil
	require.NotContains(t, capturedEnvironment, "OAUTH_TOKEN")
}

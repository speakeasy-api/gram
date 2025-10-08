package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var sharedToolMetricsClient *toolmetrics.ClickhouseClient

func TestMain(m *testing.M) {
	ctx := context.Background()
	client, cleanup, err := testenv.NewSharedToolMetricsClient(ctx)
	if err != nil {
		log.Fatalf("failed to create shared tool metrics client: %v", err)
	}

	sharedToolMetricsClient = client

	code := m.Run()

	if err = cleanup(); err != nil {
		log.Fatalf("failed to cleanup clickhouse: %v", err)
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
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			// Create tool with path parameter configuration
			tool := &HTTPTool{
				ID:                 uuid.New().String(),
				ProjectID:          uuid.New().String(),
				DeploymentID:       uuid.New().String(),
				OrganizationID:     uuid.New().String(),
				Name:               "test_tool",
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
				tool.PathParams[paramName] = &HTTPParameter{
					Name:            paramName,
					Style:           "simple",
					Explode:         boolPtr(false),
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
				nil, // no cache needed for this test
				policy,
				sharedToolMetricsClient,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), map[string]string{}, tool)

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
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			// Create tool with header parameter configuration
			tool := &HTTPTool{
				ID:                 uuid.New().String(),
				ProjectID:          uuid.New().String(),
				DeploymentID:       uuid.New().String(),
				OrganizationID:     uuid.New().String(),
				Name:               "test_tool",
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
				tool.HeaderParams[paramName] = &HTTPParameter{
					Name:            paramName,
					Style:           "simple",
					Explode:         boolPtr(false),
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
				nil, // no cache needed for this test
				policy,
				sharedToolMetricsClient,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), map[string]string{}, tool)

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

			// Give ClickHouse a moment to process async writes
			time.Sleep(100 * time.Millisecond)

			// Verify that logs were written to ClickHouse
			pagination := &toolmetrics.PaginationRequest{
				PerPage:   10,
				Sort:      "DESC",
				Direction: toolmetrics.Next,
			}
			logs, err := sharedToolMetricsClient.List(ctx, tool.ProjectID, time.Now().Add(-1*time.Hour), time.Now().Add(1*time.Hour), time.Now().Add(1*time.Hour), pagination)
			require.NoError(t, err)
			require.NotNil(t, logs)
			require.Len(t, logs.Logs, 1, "expected exactly one log entry in ClickHouse")

			toolHTTPRequest := logs.Logs[0]
			require.Equal(t, tool.ProjectID, toolHTTPRequest.ProjectID)
			require.Equal(t, tool.OrganizationID, toolHTTPRequest.OrganizationID)
			require.Equal(t, tool.DeploymentID, toolHTTPRequest.DeploymentID)
			require.Equal(t, tool.ID, toolHTTPRequest.ToolID)
			require.Equal(t, tool.Method, toolHTTPRequest.HTTPMethod)
			require.Equal(t, tool.Path, toolHTTPRequest.HTTPRoute)
			require.Equal(t, uint16(200), toolHTTPRequest.StatusCode)
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
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"max": {
					Name:            "max",
					Style:           "form",
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"rate": {
					Name:            "rate",
					Style:           "form",
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"category": {
					Name:            "category",
					Style:           "form",
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"status": {
					Name:            "status",
					Style:           "form",
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"expires": {
					Name:            "expires",
					Style:           "form",
					Explode:         boolPtr(true),
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
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"name": {
					Name:            "name",
					Style:           "form",
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"created_at": {
					Name:            "created_at",
					Style:           "form",
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"price": {
					Name:            "price",
					Style:           "form",
					Explode:         boolPtr(true),
					AllowEmptyValue: false,
				},
				"active": {
					Name:            "active",
					Style:           "form",
					Explode:         boolPtr(true),
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
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			// Create tool with query parameter configuration
			tool := &HTTPTool{
				ID:                 uuid.New().String(),
				ProjectID:          uuid.New().String(),
				DeploymentID:       uuid.New().String(),
				OrganizationID:     uuid.New().String(),
				Name:               "test_tool",
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
				nil, // no cache needed for this test
				policy,
				sharedToolMetricsClient,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), map[string]string{}, tool)
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
			policy, err := guardian.NewUnsafePolicy([]string{})
			require.NoError(t, err)

			// Create tool configuration
			var path string
			if tt.contentType == "application/x-www-form-urlencoded" {
				path = "/api/form"
			} else {
				path = "/api/users"
			}

			tool := &HTTPTool{
				ID:                 uuid.New().String(),
				ProjectID:          uuid.New().String(),
				DeploymentID:       uuid.New().String(),
				OrganizationID:     uuid.New().String(),
				Name:               "test_tool",
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
				nil, // no cache needed for this test
				policy,
				sharedToolMetricsClient,
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(toolCallBodyBytes), map[string]string{}, tool)
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

// boolPtr is a helper function to create a pointer to a boolean value
func boolPtr(b bool) *bool {
	return &b
}

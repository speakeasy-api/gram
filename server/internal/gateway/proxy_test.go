package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestToolProxy_Do_NumbersInPathParams(t *testing.T) {
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
			pathParam:     map[string]any{"orderId": 9007199254740991},
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
				Headers:              nil,
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

func TestToolProxy_Do_NumbersInQueryParams(t *testing.T) {
	t.Parallel()

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
			name: "large integer query param",
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
				Headers:              nil,
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

func TestToolProxy_Do_MixedParameterTypes(t *testing.T) {
	t.Parallel()

	// Test that combines path params, query params with numbers and strings
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

	// Create tool with mixed parameter configuration
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
		Path:               "/users/{userId}/orders/{orderId}",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams: map[string]*HTTPParameter{
			"limit": {
				Name:            "limit",
				Style:           "form",
				Explode:         boolPtr(true),
				AllowEmptyValue: false,
			},
			"price_min": {
				Name:            "price_min",
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
		},
		PathParams: map[string]*HTTPParameter{
			"userId": {
				Name:            "userId",
				Style:           "simple",
				Explode:         boolPtr(false),
				AllowEmptyValue: false,
			},
			"orderId": {
				Name:            "orderId",
				Style:           "simple",
				Explode:         boolPtr(false),
				AllowEmptyValue: false,
			},
		},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	}

	// Create request body with mixed parameters
	requestBody := ToolCallBody{
		PathParameters: map[string]any{
			"userId":  123,      // integer
			"orderId": 456789,   // integer
		},
		QueryParameters: map[string]any{
			"limit":     50,      // integer
			"price_min": 19.99,   // float
			"category":  "electronics", // string
		},
		Headers:              nil,
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
	)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Execute the proxy call
	err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), map[string]string{}, tool)
	require.NoError(t, err)
	require.NotNil(t, capturedRequest)

	// Verify path parameters were correctly replaced
	require.Equal(t, "/users/123/orders/456789", capturedRequest.URL.Path)

	// Verify query parameters were correctly encoded
	expectedQueries := url.Values{
		"limit":     []string{"50"},
		"price_min": []string{"19.99"},
		"category":  []string{"electronics"},
	}

	actualQueries := capturedRequest.URL.Query()
	for expectedKey, expectedValues := range expectedQueries {
		actualValues, exists := actualQueries[expectedKey]
		require.True(t, exists, "expected query parameter %s not found", expectedKey)
		require.Equal(t, expectedValues, actualValues, "query parameter %s has incorrect values", expectedKey)
	}
}

func TestToolProxy_Do_NumberConversionEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inputValue  any
		expectedStr string
		description string
	}{
		{
			name:        "zero integer",
			inputValue:  0,
			expectedStr: "0",
			description: "Zero should be preserved as string",
		},
		{
			name:        "max safe integer",
			inputValue:  9007199254740991,
			expectedStr: "9007199254740991",
			description: "Maximum safe integer should be preserved exactly",
		},
		{
			name:        "beyond safe integer as string",
			inputValue:  "9007199254740992",
			expectedStr: "9007199254740992",
			description: "Numbers beyond safe integer range should be passed as strings",
		},
		{
			name:        "very large number as string",
			inputValue:  "999999999999999999999999999999",
			expectedStr: "999999999999999999999999999999",
			description: "Very large numbers should be passed as strings",
		},
		{
			name:        "high precision decimal as string",
			inputValue:  "3.141592653589793238462643383279",
			expectedStr: "3.141592653589793238462643383279",
			description: "High precision decimals should be passed as strings",
		},
		{
			name:        "scientific notation as string",
			inputValue:  "2.5E+3",
			expectedStr: "2.5E+3",
			description: "Scientific notation should be passed as strings",
		},
		{
			name:        "regular float",
			inputValue:  3.14159,
			expectedStr: "3.14159",
			description: "Regular floats should be converted correctly",
		},
		{
			name:        "negative integer",
			inputValue:  -42,
			expectedStr: "-42",
			description: "Negative integers should be converted correctly",
		},
		{
			name:        "negative float",
			inputValue:  -3.14,
			expectedStr: "-3.14",
			description: "Negative floats should be converted correctly",
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

			// Test both path and query parameters
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
				Path:               "/data/{value}",
				Schema:             []byte{},
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams: map[string]*HTTPParameter{
					"param": {
						Name:            "param",
						Style:           "form",
						Explode:         boolPtr(true),
						AllowEmptyValue: false,
					},
				},
				PathParams: map[string]*HTTPParameter{
					"value": {
						Name:            "value",
						Style:           "simple",
						Explode:         boolPtr(false),
						AllowEmptyValue: false,
					},
				},
				RequestContentType: NullString{Value: "application/json", Valid: true},
				ResponseFilter:     nil,
			}

			// Create request body with the test value in both path and query params
			requestBody := ToolCallBody{
				PathParameters: map[string]any{
					"value": tt.inputValue,
				},
				QueryParameters: map[string]any{
					"param": tt.inputValue,
				},
				Headers:              nil,
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
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(bodyBytes), map[string]string{}, tool)
			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Verify path parameter was correctly converted
			expectedPath := fmt.Sprintf("/data/%s", tt.expectedStr)
			require.Equal(t, expectedPath, capturedRequest.URL.Path, tt.description)

			// Verify query parameter was correctly converted
			actualQueries := capturedRequest.URL.Query()
			actualParamValues, exists := actualQueries["param"]
			require.True(t, exists, "expected query parameter 'param' not found")
			require.Len(t, actualParamValues, 1, "expected exactly one query parameter value")
			require.Equal(t, tt.expectedStr, actualParamValues[0], tt.description)
		})
	}
}

func TestToolProxy_Do_StringsAndTimestampsInQueryParams(t *testing.T) {
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
				Headers:              nil,
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

func TestToolProxy_Do_RequestBodyPassthrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requestBody  map[string]any
		contentType  string
		expectedBody string
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
		},
		{
			name: "empty JSON body",
			requestBody: map[string]any{},
			contentType: "application/json",
			expectedBody: `{}`,
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
				Path:               "/api/users",
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
				Headers:              nil,
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
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(toolCallBodyBytes), map[string]string{}, tool)
			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Verify the request body was passed through correctly by unmarshaling both and comparing
			var expectedJSON, actualJSON map[string]any
			err = json.Unmarshal([]byte(tt.expectedBody), &expectedJSON)
			require.NoError(t, err)
			err = json.Unmarshal(capturedBody, &actualJSON)
			require.NoError(t, err)
			require.Equal(t, expectedJSON, actualJSON)

			// Verify content type header
			require.Equal(t, tt.contentType, capturedRequest.Header.Get("Content-Type"))
		})
	}
}

func TestToolProxy_Do_FormEncodedBodyPassthrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requestBody  map[string]any
		expectedForm url.Values
	}{
		{
			name: "simple form data",
			requestBody: map[string]any{
				"name":  "John Doe",
				"email": "john@example.com",
				"age":   "30",
			},
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
				Path:               "/api/form",
				Schema:             []byte{},
				HeaderParams:       map[string]*HTTPParameter{},
				QueryParams:        map[string]*HTTPParameter{},
				PathParams:         map[string]*HTTPParameter{},
				RequestContentType: NullString{Value: "application/x-www-form-urlencoded", Valid: true},
				ResponseFilter:     nil,
			}

			// Marshal the test request body
			bodyJSON, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			// Create request body for the tool call
			toolCallBody := ToolCallBody{
				PathParameters:       nil,
				QueryParameters:      nil,
				Headers:              nil,
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
			)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Execute the proxy call
			err = proxy.Do(ctx, recorder, bytes.NewReader(toolCallBodyBytes), map[string]string{}, tool)
			require.NoError(t, err)
			require.NotNil(t, capturedRequest)

			// Parse the captured form data
			actualFormData, err := url.ParseQuery(string(capturedBody))
			require.NoError(t, err)

			// Verify each expected form field
			for expectedKey, expectedValues := range tt.expectedForm {
				actualValues, exists := actualFormData[expectedKey]
				require.True(t, exists, "expected form field %s not found", expectedKey)
				require.Equal(t, expectedValues, actualValues, "form field %s has incorrect values", expectedKey)
			}

			// Verify content type header
			require.Equal(t, "application/x-www-form-urlencoded", capturedRequest.Header.Get("Content-Type"))
		})
	}
}

// boolPtr is a helper function to create a pointer to a boolean value
func boolPtr(b bool) *bool {
	return &b
}
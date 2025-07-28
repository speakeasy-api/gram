package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHandleResponseFiltering_NoFilter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter:     nil,
	}

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           make(http.Header),
		Body:             io.NopCloser(bytes.NewReader([]byte(`{"data": "test"}`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	result := handleResponseFiltering(ctx, logger, tool, nil, resp)
	require.Nil(t, result)
}

func TestHandleResponseFiltering_NoResponseFilter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           make(http.Header),
		Body:             io.NopCloser(bytes.NewReader([]byte(`{"data": "test"}`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	result := handleResponseFiltering(ctx, logger, tool, nil, resp)
	require.Nil(t, result)
}

func TestHandleResponseFiltering_ContentTypeMismatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".data",
	}

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/xml"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(`<data>test</data>`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	result := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.Nil(t, result)
}

func TestHandleResponseFiltering_StatusCodeMismatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".data",
	}

	resp := &http.Response{
		Status:           "404 Not Found",
		StatusCode:       404,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/json"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(`{"error": "not found"}`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	result := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.Nil(t, result)
}

func TestHandleResponseFiltering_InvalidJQFilter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".invalid[syntax",
	}

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/json"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(`{"data": "test"}`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	result := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.Nil(t, result)
}

func TestHandleResponseFiltering_SuccessfulJSONFilter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".data",
	}

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/json"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(`{"data": "test", "meta": {"count": 1}}`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	filterResult := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.NotNil(t, filterResult)
	require.Equal(t, 200, filterResult.statusCode)
	require.Equal(t, "application/json", filterResult.contentType)

	// Verify the filtered response
	data, err := io.ReadAll(filterResult.resp)
	require.NoError(t, err)

	var result []interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "test", result[0])
}

func TestHandleResponseFiltering_SuccessfulYAMLFilter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/yaml"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".items | map(.name)",
	}

	yamlData := `items:
  - name: "item1"
    id: 1
  - name: "item2"
    id: 2
meta:
  count: 2`

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/yaml"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(yamlData))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	filterResult := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.NotNil(t, filterResult)
	require.Equal(t, 200, filterResult.statusCode)
	require.Equal(t, "application/yaml", filterResult.contentType)

	// The response should be YAML encoded
	data, err := io.ReadAll(filterResult.resp)
	require.NoError(t, err)
	require.NotEmpty(t, string(data))
}

func TestHandleResponseFiltering_ComplexJQFilter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".users | map(select(.active == true)) | map({id, name})",
	}

	jsonData := `{
		"users": [
			{"id": 1, "name": "Alice", "active": true, "email": "alice@example.com"},
			{"id": 2, "name": "Bob", "active": false, "email": "bob@example.com"},
			{"id": 3, "name": "Charlie", "active": true, "email": "charlie@example.com"}
		],
		"meta": {"total": 3}
	}`

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/json"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(jsonData))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	filterResult := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.NotNil(t, filterResult)
	require.Equal(t, 200, filterResult.statusCode)
	require.Equal(t, "application/json", filterResult.contentType)

	// Verify the filtered response
	data, err := io.ReadAll(filterResult.resp)
	require.NoError(t, err)

	var result []interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	require.Len(t, result, 1) // The result is wrapped in an array by gojq

	// Extract the actual filtered data
	actualResults, ok := result[0].([]interface{})
	require.True(t, ok, "Expected result[0] to be []interface{}")
	require.Len(t, actualResults, 2) // Only active users

	// Check the structure of filtered results
	firstUser, ok := actualResults[0].(map[string]interface{})
	require.True(t, ok, "Expected actualResults[0] to be map[string]interface{}")
	require.Contains(t, firstUser, "id")
	require.Contains(t, firstUser, "name")
	require.NotContains(t, firstUser, "email")  // Should be filtered out
	require.NotContains(t, firstUser, "active") // Should be filtered out
}

func TestHandleResponseFiltering_FilterError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".[0] | .field",
	}

	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/json"}},
		Body:             io.NopCloser(bytes.NewReader([]byte(`{"data": "test"}`))),
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	filterResult := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.NotNil(t, filterResult)
	require.Equal(t, http.StatusBadRequest, filterResult.statusCode)
	require.Equal(t, "application/json", filterResult.contentType)

	// Should return a 400 error with error message
	data, err := io.ReadAll(filterResult.resp)
	require.NoError(t, err)

	var errorResp map[string]string
	err = json.Unmarshal(data, &errorResp)
	require.NoError(t, err)
	require.Contains(t, errorResp, "error")
	require.Contains(t, errorResp["error"], "Response filter failed to match response structure")
}

func TestHandleResponseFiltering_ReadBodyError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tool := &HTTPTool{
		ID:                 uuid.New().String(),
		ProjectID:          uuid.New().String(),
		DeploymentID:       uuid.New().String(),
		OrganizationID:     uuid.New().String(),
		Name:               "test_tool",
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: "", Valid: false},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             "GET",
		Path:               "/test",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "", Valid: false},
		ResponseFilter: &ResponseFilter{
			Type:         FilterTypeJQ,
			Schema:       []byte{},
			StatusCodes:  []string{"200"},
			ContentTypes: []string{"application/json"},
		},
	}

	responseFilter := &ResponseFilterRequest{
		Type:   "jq",
		Filter: ".data",
	}

	// Create a response with a body that will error on read
	resp := &http.Response{
		Status:           "200 OK",
		StatusCode:       200,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           http.Header{"Content-Type": []string{"application/json"}},
		Body:             &errorReader{},
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}

	filterResult := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
	require.NotNil(t, filterResult)
	require.Equal(t, http.StatusInternalServerError, filterResult.statusCode)
	require.Equal(t, "application/octet-stream", filterResult.contentType)
}

// errorReader is a helper that always returns an error when reading
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func (e *errorReader) Close() error {
	return nil
}

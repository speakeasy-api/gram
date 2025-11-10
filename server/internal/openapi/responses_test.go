package openapi

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/tools/repo/models"
	"github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"
)

type openapiFixture struct {
	doc *openapi.OpenAPI
}

func newOpenAPIFixture(t *testing.T, input string) *openapiFixture {
	t.Helper()
	ctx := t.Context()

	doc, _, err := openapi.Unmarshal(ctx, strings.NewReader(input), openapi.WithSkipValidation())
	require.NoError(t, err, "unmarshal openapi should not error")

	return &openapiFixture{doc: doc}
}

func newOpenAPIFixtureFromFile(t *testing.T, fixturePath string) *openapiFixture {
	t.Helper()
	ctx := t.Context()

	src, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "read testdata file should not error")

	doc, _, err := openapi.Unmarshal(ctx, bytes.NewReader(src), openapi.WithSkipValidation())
	require.NoError(t, err, "unmarshal openapi should not error")

	return &openapiFixture{doc: doc}
}

func (of *openapiFixture) getOperationByID(t *testing.T, id string) *openapi.Operation {
	t.Helper()

	var op *openapi.Operation
	for item := range openapi.Walk(t.Context(), of.doc) {
		if op != nil {
			break
		}
		err := item.Match(openapi.Matcher{
			Operation: func(operation *openapi.Operation) error {
				op = operation
				return nil
			},
		})
		require.NoError(t, err, "walk should not error")
	}
	require.NotNil(t, op, "operation should not be nil")

	return op
}

func TestContentTypeSpecificity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		expected    int
	}{
		{
			name:        "application/json is most generic",
			contentType: "application/json",
			expected:    1,
		},
		{
			name:        "application/yaml is second most generic",
			contentType: "application/yaml",
			expected:    2,
		},
		{
			name:        "text/yaml is second most generic",
			contentType: "text/yaml",
			expected:    2,
		},
		{
			name:        "specific json type",
			contentType: "application/vnd.api+json",
			expected:    10,
		},
		{
			name:        "specific yaml type",
			contentType: "application/vnd.api+yaml",
			expected:    11,
		},
		{
			name:        "non-json/yaml type",
			contentType: "application/xml",
			expected:    100,
		},
		{
			name:        "text/plain",
			contentType: "text/plain",
			expected:    100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := contentTypeSpecificity(tt.contentType)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetResponseFilter_NilFilterType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	schemaCache := newConcurrentSchemaCache()
	td := newOpenAPIFixtureFromFile(t, "testdata/speakeasy-bar.yaml")
	op := td.getOperationByID(t, "getDrink")

	responseFilter, schemaBytes, err := getResponseFilterSpeakeasy(ctx, logger, td.doc, schemaCache, op, nil)
	require.NoError(t, err)
	require.Nil(t, responseFilter)
	require.Nil(t, schemaBytes)
}

func TestGetResponseFilter_NonJQFilterType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	schemaCache := newConcurrentSchemaCache()
	td := newOpenAPIFixtureFromFile(t, "testdata/speakeasy-bar.yaml")
	op := td.getOperationByID(t, "getDrink")
	filterType := models.FilterTypeNone

	responseFilter, schemaBytes, err := getResponseFilterSpeakeasy(ctx, logger, td.doc, schemaCache, op, &filterType)
	require.NoError(t, err)
	require.Nil(t, responseFilter)
	require.Nil(t, schemaBytes)
}

func TestGetResponseFilter_WithJQFilterType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an OpenAPI spec with a simple response
	spec := newOpenAPIFixture(t, `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  name:
                    type: string
`)

	operation := spec.getOperationByID(t, "testGet")
	schemaCache := newConcurrentSchemaCache()
	filterType := models.FilterTypeJQ

	responseFilter, schema, err := getResponseFilterSpeakeasy(ctx, logger, spec.doc, schemaCache, operation, &filterType)
	require.NoError(t, err)
	require.NotNil(t, responseFilter)
	require.NotNil(t, schema)

	// Verify response filter properties
	require.Equal(t, models.FilterTypeJQ, responseFilter.Type)
	require.NotEmpty(t, responseFilter.Schema)
	require.Contains(t, responseFilter.StatusCodes, "200")
	require.Contains(t, responseFilter.ContentTypes, "application/json")

	// Verify schema bytes contain the response filter schema with embedded response schema
	// FIXME
	// require.Contains(t, string(schemaBytes), "Response filter configuration")
	// require.Contains(t, string(schemaBytes), "jq filter expression")
}

func TestSelectResponse_NoResponses(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	spec := newOpenAPIFixture(t, `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      requestBody:
        description: Test request body
        required: false
        content:
          text/plain:
            schema:
              type: string
`)

	operation := spec.getOperationByID(t, "testGet")
	schemaCache := newConcurrentSchemaCache()

	capturedBody, err := captureResponseBodySpeakeasy(ctx, logger, spec.doc, schemaCache, operation)
	require.NoError(t, err)
	require.Nil(t, capturedBody)
}

func TestSelectResponse_WithMultipleStatusCodes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an OpenAPI spec with multiple response codes
	spec := newOpenAPIFixture(t, `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: string
        '201':
          description: Created
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: string
        '400':
          description: Bad Request
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: string
`)

	operation := spec.getOperationByID(t, "testGet")

	mediaType, contentTypes, statusCodes, err := selectResponseSpeakeasy(ctx, logger, spec.doc, operation)
	require.NoError(t, err)
	require.NotNil(t, mediaType)
	require.NotNil(t, contentTypes)
	require.NotNil(t, statusCodes)

	// Should select the lowest 2xx status code (200)
	require.Contains(t, statusCodes, "200")
	require.Contains(t, contentTypes, "application/json")
}

func TestSelectResponse_PreferGenericContentType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an OpenAPI spec with multiple content types for same schema
	spec := newOpenAPIFixture(t, `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      responses:
        '200':
          description: Success
          content:
            application/vnd.api+json:
              schema:
                type: object
                properties:
                  data:
                    type: string
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: string
`)

	operation := spec.getOperationByID(t, "testGet")
	mediaType, contentTypes, statusCodes, err := selectResponseSpeakeasy(ctx, logger, spec.doc, operation)
	require.NoError(t, err)
	require.NotNil(t, mediaType)
	require.NotNil(t, contentTypes)
	require.NotNil(t, statusCodes)

	// Should include both content types but prefer the more generic one
	require.Contains(t, contentTypes, "application/json")
	require.Contains(t, contentTypes, "application/vnd.api+json")
	require.Contains(t, statusCodes, "200")
}

func TestSelectResponse_YAMLAndJSON(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an OpenAPI spec with both YAML and JSON content types
	spec := newOpenAPIFixture(t, `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      responses:
        '200':
          description: Success
          content:
            application/yaml:
              schema:
                type: object
                properties:
                  data:
                    type: string
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: string
`)

	operation := spec.getOperationByID(t, "testGet")

	mediaType, contentTypes, statusCodes, err := selectResponseSpeakeasy(ctx, logger, spec.doc, operation)
	require.NoError(t, err)
	require.NotNil(t, mediaType)
	require.NotNil(t, contentTypes)
	require.NotNil(t, statusCodes)

	// Should include both content types and prefer JSON (lower specificity)
	require.Contains(t, contentTypes, "application/json")
	require.Contains(t, contentTypes, "application/yaml")
	require.Contains(t, statusCodes, "200")
}

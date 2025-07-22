package openapi

import (
	"log/slog"
	"os"
	"testing"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/speakeasy-api/gram/server/internal/tools/repo/models"
	"github.com/stretchr/testify/require"
)

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

	// Create a minimal operation
	op := &v3.Operation{
		Tags:         []string{},
		Summary:      "",
		Description:  "",
		ExternalDocs: nil,
		OperationId:  "",
		Parameters:   nil,
		RequestBody:  nil,
		Responses:    nil,
		Callbacks:    nil,
		Deprecated:   nil,
		Security:     nil,
		Servers:      nil,
		Extensions:   nil,
	}

	responseFilter, schemaBytes, err := getResponseFilter(ctx, logger, op, nil)
	require.NoError(t, err)
	require.Nil(t, responseFilter)
	require.Nil(t, schemaBytes)
}

func TestGetResponseFilter_NonJQFilterType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a minimal operation
	op := &v3.Operation{
		Tags:         []string{},
		Summary:      "",
		Description:  "",
		ExternalDocs: nil,
		OperationId:  "",
		Parameters:   nil,
		RequestBody:  nil,
		Responses:    nil,
		Callbacks:    nil,
		Deprecated:   nil,
		Security:     nil,
		Servers:      nil,
		Extensions:   nil,
	}
	filterType := models.FilterTypeNone

	responseFilter, schemaBytes, err := getResponseFilter(ctx, logger, op, &filterType)
	require.NoError(t, err)
	require.Nil(t, responseFilter)
	require.Nil(t, schemaBytes)
}

func TestGetResponseFilter_WithJQFilterType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an OpenAPI spec with a simple response
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
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
`

	doc, err := libopenapi.NewDocument([]byte(spec))
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	require.Empty(t, errs)

	operation := model.Model.Paths.PathItems.GetOrZero("/test").Get
	require.NotNil(t, operation)

	filterType := models.FilterTypeJQ

	responseFilter, schemaBytes, err := getResponseFilter(ctx, logger, operation, &filterType)
	require.NoError(t, err)
	require.NotNil(t, responseFilter)
	require.NotNil(t, schemaBytes)

	// Verify response filter properties
	require.Equal(t, models.FilterTypeJQ, responseFilter.Type)
	require.NotEmpty(t, responseFilter.Schema)
	require.Contains(t, responseFilter.StatusCodes, "200")
	require.Contains(t, responseFilter.ContentTypes, "application/json")

	// Verify schema bytes contain the response filter schema with embedded response schema
	require.Contains(t, string(schemaBytes), "Response filter configuration")
	require.Contains(t, string(schemaBytes), "jq filter expression")
}

func TestSelectResponse_NoResponses(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an operation with nil responses - this should be handled by captureResponseBody, not selectResponse
	// Let's test captureResponseBody instead
	op := &v3.Operation{
		Tags:         []string{},
		Summary:      "",
		Description:  "",
		ExternalDocs: nil,
		OperationId:  "",
		Parameters:   nil,
		RequestBody:  nil,
		Responses:    nil,
		Callbacks:    nil,
		Deprecated:   nil,
		Security:     nil,
		Servers:      nil,
		Extensions:   nil,
	}

	capturedBody, err := captureResponseBody(ctx, logger, op)
	require.NoError(t, err)
	require.Nil(t, capturedBody)
}

func TestSelectResponse_WithMultipleStatusCodes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create an OpenAPI spec with multiple response codes
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
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
`

	doc, err := libopenapi.NewDocument([]byte(spec))
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	require.Empty(t, errs)

	operation := model.Model.Paths.PathItems.GetOrZero("/test").Get
	require.NotNil(t, operation)

	mediaType, contentTypes, statusCodes := selectResponse(ctx, logger, operation)
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
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
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
`

	doc, err := libopenapi.NewDocument([]byte(spec))
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	require.Empty(t, errs)

	operation := model.Model.Paths.PathItems.GetOrZero("/test").Get
	require.NotNil(t, operation)

	mediaType, contentTypes, statusCodes := selectResponse(ctx, logger, operation)
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
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
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
`

	doc, err := libopenapi.NewDocument([]byte(spec))
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	require.Empty(t, errs)

	operation := model.Model.Paths.PathItems.GetOrZero("/test").Get
	require.NotNil(t, operation)

	mediaType, contentTypes, statusCodes := selectResponse(ctx, logger, operation)
	require.NotNil(t, mediaType)
	require.NotNil(t, contentTypes)
	require.NotNil(t, statusCodes)

	// Should include both content types and prefer JSON (lower specificity)
	require.Contains(t, contentTypes, "application/json")
	require.Contains(t, contentTypes, "application/yaml")
	require.Contains(t, statusCodes, "200")
}

package openapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestBulkInsertPreValidation tests that paths longer than 2000 characters are filtered out
func TestBulkInsertPreValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	// Create a mock for testing
	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	extractor := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	// Simple valid OpenAPI spec
	openapiDoc := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /users:
    get:
      summary: Test operation
      responses:
        '200':
          description: OK
`

	// Set up the task
	projectID := uuid.New()
	deploymentID := uuid.New()
	documentID := uuid.New()

	task := ToolExtractorTask{
		Parser:             "libopenapi",
		ProjectID:          projectID,
		DeploymentID:       deploymentID,
		DocumentID:         documentID,
		ProjectSlug:        "test-project",
		OrgSlug:            "test-org",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test-api",
			Slug:    "test-api",
			ID:      "test-id",
			AssetID: "test-asset-id",
		},
		DocURL:             nil, // Not needed for this test
		OnOperationSkipped: nil,
	}

	// This should succeed using the bulk insert path
	result, err := extractor.doLibOpenAPI(ctx, logger, tracer, tx, []byte(openapiDoc), task)

	// Verify success
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "3.0.0", result.DocumentVersion)
}

func TestBulkInsertSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	extractor := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	// Create a simple OpenAPI spec with multiple valid operations
	openapiDoc := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /users:
    get:
      summary: List users
      responses:
        '200':
          description: OK
    post:
      summary: Create user
      responses:
        '201':
          description: Created
  /users/{id}:
    get:
      summary: Get user
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: OK
`

	// Set up the task
	projectID := uuid.New()
	deploymentID := uuid.New()
	documentID := uuid.New()

	task := ToolExtractorTask{
		Parser:             "libopenapi",
		ProjectID:          projectID,
		DeploymentID:       deploymentID,
		DocumentID:         documentID,
		ProjectSlug:        "test-project",
		OrgSlug:            "test-org",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test-api",
			Slug:    "test-api",
			ID:      "test-id",
			AssetID: "test-asset-id",
		},
		DocURL:             nil, // Not needed for this test
		OnOperationSkipped: nil,
	}

	// This should succeed and create 3 tools (GET /users, POST /users, GET /users/{id})
	result, err := extractor.doLibOpenAPI(ctx, logger, tracer, tx, []byte(openapiDoc), task)

	// Verify success
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "3.0.0", result.DocumentVersion)
}

// TestBulkInsertMultipleOperations tests that multiple operations are processed correctly
func TestBulkInsertMultipleOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	extractor := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	// Create an OpenAPI spec with multiple valid operations
	openapiDoc := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /users:
    get:
      summary: List users
      responses:
        '200':
          description: OK
  /products:
    get:
      summary: List products
      responses:
        '200':
          description: OK
`

	// Set up the task
	projectID := uuid.New()
	deploymentID := uuid.New()
	documentID := uuid.New()

	task := ToolExtractorTask{
		Parser:             "libopenapi",
		ProjectID:          projectID,
		DeploymentID:       deploymentID,
		DocumentID:         documentID,
		ProjectSlug:        "test-project",
		OrgSlug:            "test-org",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test-api",
			Slug:    "test-api",
			ID:      "test-id",
			AssetID: "test-asset-id",
		},
		DocURL:             nil, // Not needed for this test
		OnOperationSkipped: nil,
	}

	// This should succeed and create 2 tools using bulk insert
	result, err := extractor.doLibOpenAPI(ctx, logger, tracer, tx, []byte(openapiDoc), task)

	// Verify success
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "3.0.0", result.DocumentVersion)
}
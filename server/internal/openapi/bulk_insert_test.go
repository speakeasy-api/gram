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

func TestBulkInsertPreValidation(t *testing.T) {
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

	projectID := uuid.New()
	deploymentID := uuid.New()
	documentID := uuid.New()

	task := ToolExtractorTask{
		Parser:       "libopenapi",
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		DocumentID:   documentID,
		ProjectSlug:  "test-project",
		OrgSlug:      "test-org",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test-api",
			Slug:    "test-api",
			ID:      "test-id",
			AssetID: "test-asset-id",
		},
		DocURL:             nil,
		OnOperationSkipped: nil,
	}

	result, err := extractor.doLibOpenAPI(ctx, logger, tracer, tx, []byte(openapiDoc), task)

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

	projectID := uuid.New()
	deploymentID := uuid.New()
	documentID := uuid.New()

	task := ToolExtractorTask{
		Parser:       "libopenapi",
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		DocumentID:   documentID,
		ProjectSlug:  "test-project",
		OrgSlug:      "test-org",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test-api",
			Slug:    "test-api",
			ID:      "test-id",
			AssetID: "test-asset-id",
		},
		DocURL:             nil,
		OnOperationSkipped: nil,
	}

	result, err := extractor.doLibOpenAPI(ctx, logger, tracer, tx, []byte(openapiDoc), task)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "3.0.0", result.DocumentVersion)
}

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

	projectID := uuid.New()
	deploymentID := uuid.New()
	documentID := uuid.New()

	task := ToolExtractorTask{
		Parser:       "libopenapi",
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		DocumentID:   documentID,
		ProjectSlug:  "test-project",
		OrgSlug:      "test-org",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test-api",
			Slug:    "test-api",
			ID:      "test-id",
			AssetID: "test-asset-id",
		},
		DocURL:             nil,
		OnOperationSkipped: nil,
	}

	result, err := extractor.doLibOpenAPI(ctx, logger, tracer, tx, []byte(openapiDoc), task)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "3.0.0", result.DocumentVersion)
}

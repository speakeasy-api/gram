package openapi

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type MockedRow struct{}

func (m *MockedRow) Scan(dest ...any) error {
	return nil
}

type MockedDBTX struct {
	recordedExec      [][]any
	recordedQueryRows [][]any
}

func (m *MockedDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	m.recordedExec = append(m.recordedExec, args)
	return pgconn.CommandTag{}, nil
}

func (m *MockedDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	panic("not implemented")
}

func (m *MockedDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	m.recordedQueryRows = append(m.recordedQueryRows, args)
	return &MockedRow{}
}

func (m *MockedDBTX) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}

func TestDoProcess_ValidatesJSONSchema_Speakeasy(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	p := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	deploymentID := uuid.MustParse("87654321-4321-4321-4321-210987654321")
	projectID := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	openapiDocID := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	// Valid OpenAPI document
	validDoc := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      summary: Test operation
      responses:
        '200':
          description: OK
`)

	tet := ToolExtractorTask{
		Parser: "speakeasy",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test",
			Slug:    "test",
			ID:      "a",
			AssetID: "b",
		},
		ProjectID:          projectID,
		DeploymentID:       deploymentID,
		DocumentID:         openapiDocID,
		DocURL:             nil,
		ProjectSlug:        "c",
		OrgSlug:            "d",
		OnOperationSkipped: nil,
	}

	// This should succeed and validate the generated JSON schema
	result, err := p.doSpeakeasy(t.Context(), logger, tracer, tx, validDoc, tet)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestDoProcess_RejectsInvalidJSONSchema_Speakeasy(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	p := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	deploymentID := uuid.MustParse("87654321-4321-4321-4321-210987654321")
	projectID := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	openapiDocID := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	// OpenAPI document with invalid JSON schema syntax in parameter
	invalidDoc := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      summary: Test operation
      parameters:
        - name: testParam
          in: query
          schema:
            type: invalid_type_that_breaks_jsonschema
            properties:
              nested: null
      responses:
        '200':
          description: OK
`)

	// Track if operation was skipped due to validation error
	var skippedErr error
	tet := ToolExtractorTask{
		Parser: "speakeasy",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test",
			Slug:    "test",
			ID:      "a",
			AssetID: "b",
		},
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		DocumentID:   openapiDocID,
		DocURL:       nil,
		ProjectSlug:  "c",
		OrgSlug:      "d",
		OnOperationSkipped: func(err error) {
			skippedErr = err
		},
	}

	// Extraction succeeds but operation is skipped
	result, err := p.doSpeakeasy(t.Context(), logger, tracer, tx, invalidDoc, tet)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that the operation was skipped due to invalid schema
	require.Error(t, skippedErr)
	require.Contains(t, skippedErr.Error(), "invalid tool input schema")
}

func TestDoProcess_RecursiveSchema_Speakeasy(t *testing.T) {
	t.Parallel()
	testRecursiveSchema(t, "speakeasy")
}

func testRecursiveSchema(t *testing.T, parser string) {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	p := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	deploymentID := uuid.MustParse("87654321-4321-4321-4321-210987654321")
	projectID := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	openapiDocID := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	// OpenAPI document with recursive Filter schema
	recursiveDoc := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: testGet
      summary: Test operation with recursive schema
      parameters:
        - name: filter
          in: query
          schema:
            $ref: '#/components/schemas/Filter'
      responses:
        '200':
          description: OK
components:
  schemas:
    Filter:
      type: object
      required:
        - conjunction
        - clauses
      properties:
        conjunction:
          type: string
          title: FilterConjunction
          enum:
            - and
            - or
        clauses:
          type: array
          title: Clauses
          items:
            anyOf:
              - type: object
                title: FilterClause
                required:
                  - property
                  - operator
                  - value
                properties:
                  property:
                    type: string
                    title: Property
                  operator:
                    type: string
                    title: FilterOperator
                    enum:
                      - eq
                      - ne
                      - gt
                      - gte
                      - lt
                      - lte
                      - like
                      - not_like
                  value:
                    title: Value
                    anyOf:
                      - type: string
                        maxLength: 1000
                      - type: integer
                        maximum: 2147483647
                        minimum: -2147483648
                      - type: boolean
              - $ref: '#/components/schemas/Filter'
`)

	// Track if operation was skipped due to validation error
	var skippedErr error
	tet := ToolExtractorTask{
		Parser: parser,
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test",
			Slug:    "test",
			ID:      "a",
			AssetID: "b",
		},
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		DocumentID:   openapiDocID,
		DocURL:       nil,
		ProjectSlug:  "c",
		OrgSlug:      "d",
		OnOperationSkipped: func(err error) {
			skippedErr = err
		},
	}

	// This should succeed and handle the recursive schema
	result, err := p.doSpeakeasy(t.Context(), logger, tracer, tx, recursiveDoc, tet)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, skippedErr)
}

// capturingHandler is a slog handler that captures log records for testing
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
	attrs   []slog.Attr
	group   string
}

func newCapturingHandler() *capturingHandler {
	return &capturingHandler{
		records: make([]slog.Record, 0),
	}
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &capturingHandler{
		records: h.records,
		attrs:   append(h.attrs, attrs...),
		group:   h.group,
	}
}

func (h *capturingHandler) WithGroup(name string) slog.Handler {
	return &capturingHandler{
		records: h.records,
		attrs:   h.attrs,
		group:   name,
	}
}

func (h *capturingHandler) getRecords() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record{}, h.records...)
}

func TestDoProcess_DeprecatedOperationsLoggedNotErrors(t *testing.T) {
	t.Parallel()

	handler := newCapturingHandler()
	logger := slog.New(handler)
	tracer := testenv.NewTracerProvider(t).Tracer("github.com/speakeasy-api/gram/server/internal/openapi")

	p := &ToolExtractor{
		logger:       logger,
		tracer:       tracer,
		db:           nil,
		feature:      nil,
		assetStorage: nil,
	}

	mockedDBTX := &MockedDBTX{
		recordedQueryRows: [][]any{},
		recordedExec:      [][]any{},
	}
	tx := repo.New(mockedDBTX)

	deploymentID := uuid.MustParse("87654321-4321-4321-4321-210987654321")
	projectID := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	openapiDocID := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	// OpenAPI document with a deprecated operation
	docWithDeprecated := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /active:
    get:
      operationId: activeEndpoint
      summary: Active endpoint
      responses:
        '200':
          description: OK
  /deprecated:
    get:
      operationId: deprecatedEndpoint
      summary: Deprecated endpoint
      deprecated: true
      responses:
        '200':
          description: OK
`)

	var skippedErr error
	tet := ToolExtractorTask{
		Parser: "speakeasy",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "test",
			Slug:    "test",
			ID:      "a",
			AssetID: "b",
		},
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		DocumentID:   openapiDocID,
		DocURL:       nil,
		ProjectSlug:  "c",
		OrgSlug:      "d",
		OnOperationSkipped: func(err error) {
			skippedErr = err
		},
	}

	result, err := p.doSpeakeasy(t.Context(), logger, tracer, tx, docWithDeprecated, tet)

	// Should succeed without error
	require.NoError(t, err)
	require.NotNil(t, result)

	// OnOperationSkipped should NOT be called for deprecated operations
	// (it's only called for actual errors, not expected skips)
	require.NoError(t, skippedErr, "deprecated operations should not trigger OnOperationSkipped")

	// Verify that the deprecated operation was logged
	records := handler.getRecords()
	var foundDeprecatedLog bool
	for _, r := range records {
		if r.Level == slog.LevelInfo && strings.Contains(r.Message, "skipping deprecated operation") {
			foundDeprecatedLog = true
			require.Contains(t, r.Message, "deprecatedEndpoint", "log should contain the operation ID")
			break
		}
	}
	require.True(t, foundDeprecatedLog, "expected info log for skipped deprecated operation")

	// Verify the active endpoint was processed (should have a tool definition created)
	require.Len(t, mockedDBTX.recordedQueryRows, 1, "should have created one tool definition for the active endpoint")
}

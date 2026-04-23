package openapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/sequencedmap"
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

func (m *MockedDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.recordedExec = append(m.recordedExec, args)
	return pgconn.CommandTag{}, nil
}

func (m *MockedDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	panic("not implemented")
}

func (m *MockedDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	m.recordedQueryRows = append(m.recordedQueryRows, args)
	return &MockedRow{}
}

func (m *MockedDBTX) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}

func TestDoProcess_ValidatesJSONSchema_Speakeasy(t *testing.T) {
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

	// Verify start/end info logs are emitted
	records := handler.getRecords()
	var foundStartLog, foundEndLog bool
	for _, r := range records {
		if r.Level == slog.LevelInfo && strings.Contains(r.Message, "[test] processing OpenAPI source") {
			foundStartLog = true
		}
		if r.Level == slog.LevelInfo && strings.Contains(r.Message, "[test] processed OpenAPI source") {
			foundEndLog = true
			require.Contains(t, r.Message, "1 tools created", "should report 1 tool created")
			require.Contains(t, r.Message, "0 tools skipped", "should report 0 tools skipped")
		}
	}
	require.True(t, foundStartLog, "expected info log for processing OpenAPI source")
	require.True(t, foundEndLog, "expected info log for processed OpenAPI source")
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
	mu      *sync.Mutex
	records *[]slog.Record
	attrs   []slog.Attr
	group   string
}

func newCapturingHandler() *capturingHandler {
	return &capturingHandler{
		mu:      &sync.Mutex{},
		records: &[]slog.Record{},
	}
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}

func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &capturingHandler{
		mu:      h.mu,
		records: h.records,
		attrs:   append(h.attrs, attrs...),
		group:   h.group,
	}
}

func (h *capturingHandler) WithGroup(name string) slog.Handler {
	return &capturingHandler{
		mu:      h.mu,
		records: h.records,
		attrs:   h.attrs,
		group:   name,
	}
}

func (h *capturingHandler) getRecords() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record{}, *h.records...)
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

func TestDoProcess_MissingServerURLLoggedAsWarning(t *testing.T) {
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

	// OpenAPI document with no servers section
	docWithoutServers := []byte(`
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

	result, err := p.doSpeakeasy(t.Context(), logger, tracer, tx, docWithoutServers, tet)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify tool definition was still created
	require.Len(t, mockedDBTX.recordedQueryRows, 1, "should have created one tool definition despite missing server URL")

	// Verify no error-level logs about server URLs
	records := handler.getRecords()
	for _, r := range records {
		if r.Level == slog.LevelError && strings.Contains(r.Message, "server") {
			t.Errorf("unexpected error log about server: %s", r.Message)
		}
	}
}

// TestConcurrentSchemaCache_ReturnsIndependentSchemas guards the invariant
// the extraction race fix depends on: concurrent readers of the same cache
// entry must receive independent schema trees. The old cache handed out
// aliased pointers, so one goroutine's `.Defs = nil` would be visible to
// others and downstream marshalling would race on the shared core model.
func TestConcurrentSchemaCache_ReturnsIndependentSchemas(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cache := newConcurrentSchemaCache()

	defs := sequencedmap.New[string, *oas3.JSONSchema[oas3.Referenceable]]()
	defs.Set("Widget", oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{
		Type: oas3.NewTypeFromString("object"),
	}))
	original := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{
		Type: oas3.NewTypeFromString("object"),
		Defs: defs,
	})

	const key = "test-key"
	require.NoError(t, cache.set(ctx, key, original))

	workers := max(runtime.GOMAXPROCS(0)*4, 8)

	gotPtrs := make([]*oas3.JSONSchema[oas3.Referenceable], workers)
	defsIntactOnArrival := make([]bool, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, ok, err := cache.get(ctx, key)
			if err != nil {
				errs[i] = err
				return
			}
			if !ok {
				errs[i] = fmt.Errorf("cache miss for key %q", key)
				return
			}
			if got.IsLeft() && got.GetLeft().Defs != nil && got.GetLeft().Defs.Len() == 1 {
				defsIntactOnArrival[i] = true
			}
			// Simulate the real mutation that tripped the old cache:
			// extractJSONSchemaSpeakeasy bubbles defs to the caller and
			// nulls them on the returned schema. This must not be visible
			// to sibling workers.
			if got.IsLeft() {
				got.GetLeft().Defs = nil
			}
			gotPtrs[i] = got
		}(i)
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...), "cache.get should not return errors")

	seen := make(map[*oas3.JSONSchema[oas3.Referenceable]]struct{}, workers)
	for i, got := range gotPtrs {
		require.NotNilf(t, got, "worker %d received a nil schema", i)
		require.NotSamef(t, original, got, "worker %d received the writer's pointer — cache must not alias its stored value", i)
		_, dup := seen[got]
		require.Falsef(t, dup, "worker %d received a pointer already seen by another worker — cache returned aliased schemas", i)
		seen[got] = struct{}{}
		require.Truef(t, defsIntactOnArrival[i], "worker %d observed missing Defs on arrival — cache handed out a mutated schema", i)
	}

	// The writer's original schema must remain untouched; the cache owns
	// its copies by value (serialized bytes), not by pointer.
	require.True(t, original.IsLeft())
	require.NotNil(t, original.GetLeft().Defs, "writer's original Defs should remain populated")
	require.Equal(t, 1, original.GetLeft().Defs.Len(), "writer's original Defs should still contain Widget")
}

// TestDoSpeakeasy_ConcurrentExtraction_CacheContention drives doSpeakeasy
// on a spec with many operations that all share the same hashable body
// schema shape. With GOMAXPROCS workers concurrently calling into
// extractJSONSchemaSpeakeasy, every worker after the first hits the
// shared cache entry — this is exactly the path that raced under the old
// cache design (shared *JSONSchema pointer + subsequent Marshal mutation).
// Run under `go test -race`. Uses inline YAML rather than a $ref-heavy
// fixture to isolate the cache-sharing race from unrelated resolution
// races in the openapi library.
func TestDoSpeakeasy_ConcurrentExtraction_CacheContention(t *testing.T) {
	t.Parallel()

	// Build a spec with many operations that all carry an identical inline
	// body schema. hashing.Hash is content-addressed, so each operation's
	// schema hashes to the same cache key → N-1 cache hits per run, which
	// is where the old pointer-sharing bug blew up.
	const numOps = 16
	var specBuilder strings.Builder
	specBuilder.WriteString(`openapi: 3.0.0
info:
  title: Cache contention
  version: 1.0.0
paths:
`)
	for i := range numOps {
		fmt.Fprintf(&specBuilder, `  /op%d:
    post:
      operationId: op%d
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [id]
              properties:
                id:
                  type: string
                count:
                  type: integer
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  count:
                    type: integer
`, i, i)
	}

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

	tet := ToolExtractorTask{
		Parser: "speakeasy",
		DocInfo: &types.OpenAPIv3DeploymentAsset{
			Name:    "cache-contention",
			Slug:    "cache-contention",
			ID:      "a",
			AssetID: "b",
		},
		ProjectID:          uuid.MustParse("12345678-1234-1234-1234-123456789012"),
		DeploymentID:       uuid.MustParse("87654321-4321-4321-4321-210987654321"),
		DocumentID:         uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		DocURL:             nil,
		ProjectSlug:        "c",
		OrgSlug:            "d",
		OnOperationSkipped: nil,
	}

	result, err := p.doSpeakeasy(t.Context(), logger, tracer, tx, []byte(specBuilder.String()), tet)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Every operation should produce exactly one tool row written to the
	// mock DB. If a race had corrupted a schema or panicked a worker, the
	// count would be short.
	require.Len(t, mockedDBTX.recordedQueryRows, numOps, "one tool row per operation")

	// Verify the completion log reports full extraction. Looking for the
	// full summary string avoids a false positive match between substrings
	// like "0 tools created" and "10 tools created".
	expectedSummary := fmt.Sprintf("%d tools created, 0 tools skipped", numOps)
	var foundEndLog bool
	for _, r := range handler.getRecords() {
		if r.Level == slog.LevelInfo && strings.Contains(r.Message, "processed OpenAPI source") {
			foundEndLog = true
			require.Contains(t, r.Message, expectedSummary, "expected all operations extracted, none skipped")
		}
	}
	require.True(t, foundEndLog, "expected processed-OpenAPI-source log")
}

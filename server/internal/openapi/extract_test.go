package openapi

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compareJSONWithResponseSchema compares JSON strings that may contain <ResponseSchema> tags
// Handles both literal tags and Unicode-escaped tags (\\u003cResponseSchema\\u003e)
func compareJSONWithResponseSchema(t *testing.T, expectedJSON, actualJSON string, msgAndArgs ...interface{}) bool {
	t.Helper()

	// Create regex patterns for both literal and Unicode-escaped ResponseSchema tags
	// Match from the first { to the last } within the tags
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?s)<ResponseSchema>({.*?)<\/ResponseSchema>`),
		regexp.MustCompile(`(?s)\\u003cResponseSchema\\u003e({.*?)\\u003c\/ResponseSchema\\u003e`),
	}

	var expectedSchemas, actualSchemas []string
	expectedWithoutSchemas := expectedJSON
	actualWithoutSchemas := actualJSON

	// Extract schemas using both patterns
	for _, pattern := range patterns {
		// Extract from expected
		matches := pattern.FindAllStringSubmatch(expectedWithoutSchemas, -1)
		for _, match := range matches {
			expectedSchemas = append(expectedSchemas, match[1])
		}
		expectedWithoutSchemas = pattern.ReplaceAllString(expectedWithoutSchemas, "")

		// Extract from actual
		matches = pattern.FindAllStringSubmatch(actualWithoutSchemas, -1)
		for _, match := range matches {
			actualSchemas = append(actualSchemas, match[1])
		}
		actualWithoutSchemas = pattern.ReplaceAllString(actualWithoutSchemas, "")
	}

	// If one side has ResponseSchema blocks and the other doesn't,
	// just compare the original JSON strings directly
	if len(expectedSchemas) != len(actualSchemas) {
		return assert.JSONEq(t, expectedJSON, actualJSON, msgAndArgs...)
	}

	// Compare each ResponseSchema JSON content separately
	for i, expectedSchema := range expectedSchemas {
		actualSchema := actualSchemas[i]

		// Unquote the JSON strings to handle escaped quotes
		expectedUnquoted, err := strconv.Unquote(`"` + expectedSchema + `"`)
		if err != nil {
			expectedUnquoted = expectedSchema
		}

		actualUnquoted, err := strconv.Unquote(`"` + actualSchema + `"`)
		if err != nil {
			actualUnquoted = actualSchema
		}

		if !assert.JSONEq(t, expectedUnquoted, actualUnquoted,
			"ResponseSchema JSON content differs at index %d", i) {
			return false
		}
	}

	// Compare the remaining JSON content (with ResponseSchema blocks removed)
	return assert.JSONEq(t, expectedWithoutSchemas, actualWithoutSchemas, msgAndArgs...)
}

// callSortKey generates a sort key from a recorded call's arguments.
// It concatenates string representations of the arguments to create a stable sort key.
func callSortKey(call []any) string {
	var key string
	for _, arg := range call {
		switch v := arg.(type) {
		case string:
			key += v
		case fmt.Stringer:
			key += v.String()
		case []byte:
			key += string(v)
		default:
			key += fmt.Sprintf("%v", v)
		}
		key += "|"
	}
	return key
}

// assertRecordedCallsUnordered compares two recorded call slices (recordedExec
// or recordedQueryRows) and uses JSONEq for []byte fields to properly compare
// JSON content, recursively handling nested structures. Calls are sorted before
// comparison to handle parallel execution order variations.
func assertRecordedCallsUnordered(
	t *testing.T,
	expected,
	actual [][]any, msgAndArgs ...any,
) bool {
	t.Helper()

	if !assert.Len(t, actual, len(expected), msgAndArgs...) {
		return false
	}

	// Sort both slices by their sort keys to handle parallel execution
	expectedSorted := slices.Clone(expected)
	actualSorted := slices.Clone(actual)

	slices.SortFunc(expectedSorted, func(a, b []any) int {
		return cmpString(callSortKey(a), callSortKey(b))
	})

	slices.SortFunc(actualSorted, func(a, b []any) int {
		return cmpString(callSortKey(a), callSortKey(b))
	})

	for i, expectedCall := range expectedSorted {
		actualCall := actualSorted[i]
		if !assert.Len(t, actualCall, len(expectedCall), "call %d has different number of arguments", i) {
			return false
		}

		for j, expectedArg := range expectedCall {
			actualArg := actualCall[j]

			if !compareRecursively(t, expectedArg, actualArg, i, j) {
				return false
			}
		}
	}

	return true
}

// cmpString compares two strings for sorting.
func cmpString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareRecursively compares two values recursively, handling []byte fields with JSON comparison
func compareRecursively(t *testing.T, expected, actual interface{}, callIndex, argIndex int) bool {
	t.Helper()

	// Check if both arguments are []byte (or uint8 slice) using reflection
	expectedValue := reflect.ValueOf(expected)
	actualValue := reflect.ValueOf(actual)

	// Handle nil values
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		return assert.Equal(t, expected, actual,
			"call %d, arg %d: nil mismatch", callIndex, argIndex)
	}

	// Handle pointers by dereferencing them
	if expectedValue.Kind() == reflect.Ptr && actualValue.Kind() == reflect.Ptr {
		if expectedValue.IsNil() && actualValue.IsNil() {
			return true
		}
		if expectedValue.IsNil() || actualValue.IsNil() {
			return assert.Equal(t, expected, actual,
				"call %d, arg %d: pointer nil mismatch", callIndex, argIndex)
		}
		// Dereference pointers and compare the underlying values
		return compareRecursively(t, expectedValue.Elem().Interface(), actualValue.Elem().Interface(), callIndex, argIndex)
	}

	// Check if both are byte slices ([]byte or []uint8)
	if expectedValue.Kind() == reflect.Slice && actualValue.Kind() == reflect.Slice &&
		expectedValue.Type().Elem().Kind() == reflect.Uint8 && actualValue.Type().Elem().Kind() == reflect.Uint8 {

		expectedBytes := expectedValue.Bytes()
		actualBytes := actualValue.Bytes()

		// Handle empty or nil byte slices
		if len(expectedBytes) == 0 && len(actualBytes) == 0 {
			// Both are empty, they're equal
			return true
		}

		// Only use JSONEq if both slices contain data
		if len(expectedBytes) > 0 && len(actualBytes) > 0 {
			return compareJSONWithResponseSchema(t, string(expectedBytes), string(actualBytes),
				"call %d, arg %d: JSON content differs", callIndex, argIndex)
		} else {
			// One is empty and the other isn't, use regular Equal
			return assert.Equal(t, expectedBytes, actualBytes,
				"call %d, arg %d: byte slice differs", callIndex, argIndex)
		}
	}

	// Handle slices recursively
	expectedSlice, expectedIsSlice := expected.([]interface{})
	actualSlice, actualIsSlice := actual.([]interface{})
	if expectedIsSlice && actualIsSlice {
		if len(expectedSlice) != len(actualSlice) {
			return assert.Len(t, actualSlice, len(expectedSlice),
				"call %d, arg %d: slice length differs", callIndex, argIndex)
		}
		for i, expectedItem := range expectedSlice {
			if !compareRecursively(t, expectedItem, actualSlice[i], callIndex, argIndex) {
				return false
			}
		}
		return true
	}

	// Handle maps recursively
	expectedMap, expectedIsMap := expected.(map[string]interface{})
	actualMap, actualIsMap := actual.(map[string]interface{})
	if expectedIsMap && actualIsMap {
		if len(expectedMap) != len(actualMap) {
			return assert.Len(t, actualMap, len(expectedMap),
				"call %d, arg %d: map length differs", callIndex, argIndex)
		}
		for key, expectedValue := range expectedMap {
			actualValue, exists := actualMap[key]
			if !exists {
				return assert.Fail(t, "Missing key in actual map",
					"call %d, arg %d: key %s missing", callIndex, argIndex, key)
			}
			if !compareRecursively(t, expectedValue, actualValue, callIndex, argIndex) {
				return false
			}
		}
		return true
	}

	// Check if both are structs and of the same type
	if expectedValue.Kind() == reflect.Struct && actualValue.Kind() == reflect.Struct &&
		expectedValue.Type() == actualValue.Type() {

		for i := 0; i < expectedValue.NumField(); i++ {
			expectedField := expectedValue.Field(i)
			actualField := actualValue.Field(i)

			// Skip unexported fields
			if !expectedField.CanInterface() {
				continue
			}

			// Get the interface values for comparison
			expectedFieldValue := expectedField.Interface()
			actualFieldValue := actualField.Interface()

			// Recursively compare the field values
			if !compareRecursively(t, expectedFieldValue, actualFieldValue, callIndex, argIndex) {
				return false
			}
		}
		return true
	}

	// For all other types, use regular Equal
	return assert.Equal(t, expected, actual,
		"call %d, arg %d: argument differs", callIndex, argIndex)
}

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

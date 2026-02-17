package openapi

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
	"github.com/speakeasy-api/gram/server/internal/tools/repo/models"
	"github.com/speakeasy-api/openapi/hashing"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/yml"
)

func getResponseFilterSpeakeasy(ctx context.Context, logger *slog.Logger, doc *openapi.OpenAPI, schemaCache *concurrentSchemaCache, op *openapi.Operation, responseFilterType *models.FilterType) (*models.ResponseFilter, *oas3.JSONSchema[oas3.Referenceable], error) {
	// Only JQ response filters are supported for now
	if responseFilterType == nil || *responseFilterType != models.FilterTypeJQ {
		return nil, nil, nil
	}

	var responseFilter *models.ResponseFilter

	capturedResponseBody, err := captureResponseBodySpeakeasy(ctx, logger, doc, schemaCache, op)
	if err != nil {
		return nil, nil, fmt.Errorf("error capturing response body: %w", err)
	}

	var schema *oas3.JSONSchema[oas3.Referenceable]

	if capturedResponseBody != nil {
		var buf bytes.Buffer
		ctx = yml.ContextWithConfig(ctx, &yml.Config{
			OutputFormat: yml.OutputFormatJSON,
		})
		if err := marshaller.Marshal(ctx, capturedResponseBody.schema, &buf); err != nil {
			return nil, nil, fmt.Errorf("error marshalling response schema: %w", err)
		}

		// Escape the schema JSON for safe embedding in the description
		escapedSchema := buf.String()
		escapedSchema = strings.ReplaceAll(escapedSchema, `\`, `\\`)  // Escape backslashes first
		escapedSchema = strings.ReplaceAll(escapedSchema, `"`, `\"`)  // Escape quotes
		escapedSchema = strings.ReplaceAll(escapedSchema, "\n", `\n`) // Escape newlines
		escapedSchema = strings.ReplaceAll(escapedSchema, "\r", `\r`) // Escape carriage returns
		escapedSchema = strings.ReplaceAll(escapedSchema, "\t", `\t`) // Escape tabs
		schemaBytes := fmt.Appendf(nil, responseFilterSchema, escapedSchema)

		// TODO when libopenapi is gone we should be able to avoid unmarshaling here from a string and just build the schema directly
		var outSchema oas3.JSONSchema[oas3.Referenceable]
		if _, err := marshaller.Unmarshal(ctx, bytes.NewReader(schemaBytes), &outSchema); err != nil {
			return nil, nil, fmt.Errorf("error unmarshaling response schema: %w", err)
		}
		schema = &outSchema

		responseFilter = &models.ResponseFilter{
			Type:         *responseFilterType,
			Schema:       buf.Bytes(),
			StatusCodes:  capturedResponseBody.statusCodes,
			ContentTypes: capturedResponseBody.contentTypes,
		}
	}

	return responseFilter, schema, nil
}

type capturedResponseBodySpeakeasy struct {
	schema       *oas3.JSONSchema[oas3.Referenceable]
	contentTypes []string
	statusCodes  []string
}

func captureResponseBodySpeakeasy(ctx context.Context, logger *slog.Logger, doc *openapi.OpenAPI, schemaCache *concurrentSchemaCache, op *openapi.Operation) (*capturedResponseBodySpeakeasy, error) {
	if op.Responses.Default == nil && op.Responses.Len() == 0 {
		return nil, nil
	}

	selectedResponse, selectedContentTypes, codeGroup, err := selectResponseSpeakeasy(ctx, logger, doc, op)
	if err != nil {
		return nil, fmt.Errorf("error selecting response: %w", err)
	}
	if selectedResponse == nil {
		return nil, nil
	}

	schema, d, err := extractJSONSchemaSpeakeasy(ctx, doc, schemaCache, "responseBody", selectedResponse.Schema)
	if err != nil {
		return nil, fmt.Errorf("failed to extract json schema: %w", err)
	}
	if d != nil {
		// Stuff the defs back in as the response schema is just represented as a string in the description of the responseFilter schema
		schema.GetLeft().Defs = d
	}

	return &capturedResponseBodySpeakeasy{
		schema:       schema,
		contentTypes: selectedContentTypes,
		statusCodes:  codeGroup,
	}, nil
}

func selectResponseSpeakeasy(ctx context.Context, logger *slog.Logger, doc *openapi.OpenAPI, op *openapi.Operation) (*openapi.MediaType, []string, []string, error) {
	// Map schema hash to status codes that use that schema
	schemaToStatusCodes := make(map[string][]string)
	// Map schema hash to the best MediaType for that schema (prefer JSON over YAML)
	schemaToBestMediaType := make(map[string]*openapi.MediaType)
	// Map schema hash to all content types for that schema
	schemaToContentTypes := make(map[string][]string)
	// Map schema hash to the best content type for that schema
	schemaToBestContentType := make(map[string]string)

	for code, r := range op.GetResponses().All() {
		_, err := r.Resolve(ctx, openapi.ResolveOptions{
			TargetLocation:      "/",
			RootDocument:        doc,
			DisableExternalRefs: true,
			SkipValidation:      true,
		})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error resolving response: %w", err)
		}
		response := r.GetObject()

		if response.GetContent().Len() == 0 {
			continue
		}

		for contentType, content := range response.GetContent().All() {
			if contenttypes.IsJSON(contentType) || contenttypes.IsYAML(contentType) {
				hash := hashing.Hash(content.Schema)

				// Add this status code to the schema group
				schemaToStatusCodes[hash] = append(schemaToStatusCodes[hash], code)

				// Add this content type to the schema's content types
				if !slices.Contains(schemaToContentTypes[hash], contentType) {
					schemaToContentTypes[hash] = append(schemaToContentTypes[hash], contentType)
				}

				// Determine if this is a better content type than what we have
				existingContentType, exists := schemaToBestContentType[hash]
				if !exists || contentTypeSpecificity(contentType) < contentTypeSpecificity(existingContentType) {
					// Use this content type if:
					// 1. We don't have one yet, OR
					// 2. This content type is more generic (lower specificity score)
					schemaToBestMediaType[hash] = content
					schemaToBestContentType[hash] = contentType
				}
			}
		}
	}

	if len(schemaToStatusCodes) == 0 {
		return nil, nil, nil, nil
	}

	// Create a map from status code to its schema group for easy lookup
	codeGroups := map[string][]string{}
	compatibleCodes := []string{}
	for _, codes := range schemaToStatusCodes {
		for _, code := range codes {
			codeGroups[code] = codes
			compatibleCodes = append(compatibleCodes, code)
		}
	}

	// Convert string codes to int so we can sort and select the lowest successful (2xx) code
	codes := make([]int, len(compatibleCodes))
	for i, codeString := range compatibleCodes {
		codeString = strings.ToLower(codeString)
		if strings.HasSuffix(codeString, "xx") {
			codeString = strings.Replace(codeString, "xx", "99", 1)
		}

		code, err := strconv.Atoi(codeString)
		if err != nil {
			logger.WarnContext(ctx, "failed to parse status code", attr.SlogHTTPStatusCodePattern(codeString), attr.SlogError(err))
			return nil, nil, nil, nil
		}
		codes[i] = code
	}

	codeLookup := make(map[int]string, len(codes))
	for i, code := range codes {
		codeLookup[code] = compatibleCodes[i]
	}

	slices.SortFunc(codes, func(a, b int) int {
		return a - b
	})

	// Find the lowest successful (2xx) status code
	for _, code := range codes {
		if code >= 200 && code < 300 {
			selectedCode := codeLookup[code]
			selectedResponse := op.GetResponses().GetOrZero(selectedCode).GetObject()
			if selectedResponse == nil {
				continue
			}

			// Find the schema hash for this status code using our stored mapping
			var selectedSchemaHash string
			for contentType, content := range selectedResponse.GetContent().All() {
				if contenttypes.IsJSON(contentType) || contenttypes.IsYAML(contentType) {
					hash := hashing.Hash(content.Schema)

					if schemaToBestMediaType[hash] != nil {
						selectedSchemaHash = hash
						break
					}
				}
			}

			// Return the best MediaType for this schema and all content types that share this schema
			return schemaToBestMediaType[selectedSchemaHash], schemaToContentTypes[selectedSchemaHash], codeGroups[selectedCode], nil
		}
	}

	return nil, nil, nil, nil
}

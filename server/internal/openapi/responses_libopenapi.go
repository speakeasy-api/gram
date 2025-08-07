package openapi

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
	"github.com/speakeasy-api/gram/server/internal/tools/repo/models"
)

func getResponseFilterLibOpenAPI(ctx context.Context, logger *slog.Logger, op *v3.Operation, responseFilterType *models.FilterType) (*models.ResponseFilter, []byte, error) {
	// Only JQ response filters are supported for now
	if responseFilterType == nil || *responseFilterType != models.FilterTypeJQ {
		return nil, nil, nil
	}

	var responseFilter *models.ResponseFilter
	var schemaBytes []byte

	capturedResponseBody, err := captureResponseBodyLibOpenAPI(ctx, logger, op)
	if err != nil {
		return nil, nil, fmt.Errorf("error capturing response body: %w", err)
	}

	if capturedResponseBody != nil {
		// Escape the schema JSON for safe embedding in the description
		escapedSchema := string(capturedResponseBody.schema)
		escapedSchema = strings.ReplaceAll(escapedSchema, `\`, `\\`)  // Escape backslashes first
		escapedSchema = strings.ReplaceAll(escapedSchema, `"`, `\"`)  // Escape quotes
		escapedSchema = strings.ReplaceAll(escapedSchema, "\n", `\n`) // Escape newlines
		escapedSchema = strings.ReplaceAll(escapedSchema, "\r", `\r`) // Escape carriage returns
		escapedSchema = strings.ReplaceAll(escapedSchema, "\t", `\t`) // Escape tabs
		schemaBytes = []byte(fmt.Sprintf(responseFilterSchema, escapedSchema))

		responseFilter = &models.ResponseFilter{
			Type:         *responseFilterType,
			Schema:       capturedResponseBody.schema,
			StatusCodes:  capturedResponseBody.statusCodes,
			ContentTypes: capturedResponseBody.contentTypes,
		}
	}

	return responseFilter, schemaBytes, nil
}

type capturedResponseBodyLibOpenAPI struct {
	schema       []byte
	contentTypes []string
	statusCodes  []string
	defs         Defs
}

func captureResponseBodyLibOpenAPI(ctx context.Context, logger *slog.Logger, op *v3.Operation) (*capturedResponseBodyLibOpenAPI, error) {
	if op.Responses == nil || (op.Responses.Default == nil && op.Responses.Codes.Len() == 0) {
		return nil, nil
	}

	selectedResponse, selectedContentTypes, codeGroup := selectResponseLibOpenAPI(ctx, logger, op)
	if selectedResponse == nil {
		return nil, nil
	}

	schemaBytes, err := extractJSONSchemaFromYamlLibOpenAPI("responseBody", selectedResponse.Schema)
	if err != nil {
		return nil, fmt.Errorf("failed to extract json schema: %w", err)
	}

	return &capturedResponseBodyLibOpenAPI{
		schema:       schemaBytes,
		contentTypes: selectedContentTypes,
		statusCodes:  codeGroup,
	}, nil
}

func selectResponseLibOpenAPI(ctx context.Context, logger *slog.Logger, op *v3.Operation) (*v3.MediaType, []string, []string) {
	// Map schema hash to status codes that use that schema
	schemaToStatusCodes := make(map[string][]string)
	// Map schema hash to the best MediaType for that schema (prefer JSON over YAML)
	schemaToBestMediaType := make(map[string]*v3.MediaType)
	// Map schema hash to all content types for that schema
	schemaToContentTypes := make(map[string][]string)
	// Map schema hash to the best content type for that schema
	schemaToBestContentType := make(map[string]string)
	// Map status code + content type to schema hash for consistent lookup
	codeContentToHash := make(map[string]string)

	for code, response := range op.Responses.Codes.FromOldest() {
		if orderedmap.Len(response.Content) == 0 {
			continue
		}

		for contentType, content := range response.Content.FromOldest() {
			if contenttypes.IsJSON(contentType) || contenttypes.IsYAML(contentType) {
				hashBytes := content.Schema.GoLow().Schema().Hash()
				hash := string(hashBytes[:])

				// Store the hash for this specific code+contentType combination
				codeContentKey := code + "|" + contentType
				codeContentToHash[codeContentKey] = hash

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
		return nil, nil, nil
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
			return nil, nil, nil
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
			selectedResponse := op.Responses.Codes.GetOrZero(selectedCode)
			if selectedResponse == nil {
				continue
			}

			// Find the schema hash for this status code using our stored mapping
			var selectedSchemaHash string
			for contentType := range selectedResponse.Content.FromOldest() {
				if contenttypes.IsJSON(contentType) || contenttypes.IsYAML(contentType) {
					codeContentKey := selectedCode + "|" + contentType
					if hash, exists := codeContentToHash[codeContentKey]; exists {
						selectedSchemaHash = hash
						break
					}
				}
			}

			if selectedSchemaHash == "" {
				continue
			}

			// Return the best MediaType for this schema and all content types that share this schema
			return schemaToBestMediaType[selectedSchemaHash], schemaToContentTypes[selectedSchemaHash], codeGroups[selectedCode]
		}
	}

	return nil, nil, nil
}

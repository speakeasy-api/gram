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
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
	"github.com/speakeasy-api/gram/server/internal/tools/repo/models"
)

type capturedResponseBody struct {
	schema       []byte
	contentTypes []string
	statusCodes  []string
}

const responseFilterSchema = `{
  "type": "object",
  "description": "Response filter configuration for MCP tool calls. If you want the full response data, do not use this filter. However, use this filter to reduce the size of API responses when you only need certain data - this improves performance and reduces bandwidth usage by extracting only specific fields or transforming the response structure. The 'filter' field should contain a jq filter expression that will be applied to the API response. Study the response schema carefully and use appropriate jq operations: use 'map()' for transforming arrays, 'select()' for filtering, '[]' for array iteration, and object construction '{}' for reshaping data. The response schema available for filtering can be found within the <ResponseSchema> XML tags below, which you can reference to construct appropriate filter expressions. <ResponseSchema>%s</ResponseSchema>",
  "properties": {
    "filter": {
      "type": "string",
      "examples": [
        ".data",
        ".items | map({id, name})",
        ".items[] | select(.status == \"active\")",
        "{total: .count, results: .items | map(.name)}",
        ".users | map(select(.role == \"admin\")) | length",
        ".[] | {key: .id, value: .attributes}",
        "if .items then .items else [.] end",
        ".data | group_by(.category) | map({category: .[0].category, count: length})"
      ]
    },
    "type": {
      "type": "string",
      "enum": [
        "jq"
      ]
    }
  },
  "required": [
    "filter",
	"type"
  ]
}`

// contentTypeSpecificity returns a lower number for more generic content types
// Lower numbers are preferred (more generic)
func contentTypeSpecificity(contentType string) int {
	switch {
	case contentType == "application/json":
		return 1
	case contentType == "application/yaml" || contentType == "text/yaml":
		return 2
	case contenttypes.IsJSON(contentType):
		return 10 // More specific JSON types like application/vnd.api+json
	case contenttypes.IsYAML(contentType):
		return 11 // More specific YAML types
	default:
		return 100
	}
}

func getResponseFilter(ctx context.Context, logger *slog.Logger, op *v3.Operation, responseFilterType *models.FilterType) (*models.ResponseFilter, []byte, error) {
	// Only JQ response filters are supported for now
	if responseFilterType == nil || *responseFilterType != models.FilterTypeJQ {
		return nil, nil, nil
	}

	var responseFilter *models.ResponseFilter
	var schemaBytes []byte

	capturedResponseBody, err := captureResponseBody(ctx, logger, op)
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

func captureResponseBody(ctx context.Context, logger *slog.Logger, op *v3.Operation) (*capturedResponseBody, error) {
	if op.Responses == nil || (op.Responses.Default == nil && op.Responses.Codes.Len() == 0) {
		return nil, nil
	}

	selectedResponse, selectedContentTypes, codeGroup := selectResponse(ctx, logger, op)
	if selectedResponse == nil {
		return nil, nil
	}

	schemaBytes, err := extractJSONSchemaFromYaml("responseBody", selectedResponse.Schema)
	if err != nil {
		return nil, fmt.Errorf("failed to extract json schema: %w", err)
	}

	return &capturedResponseBody{
		schema:       schemaBytes,
		contentTypes: selectedContentTypes,
		statusCodes:  codeGroup,
	}, nil
}

func selectResponse(ctx context.Context, logger *slog.Logger, op *v3.Operation) (*v3.MediaType, []string, []string) {
	// Map schema hash to status codes that use that schema
	schemaToStatusCodes := make(map[string][]string)
	// Map schema hash to the best MediaType for that schema (prefer JSON over YAML)
	schemaToBestMediaType := make(map[string]*v3.MediaType)
	// Map schema hash to all content types for that schema
	schemaToContentTypes := make(map[string][]string)
	// Map schema hash to the best content type for that schema
	schemaToBestContentType := make(map[string]string)

	for code, response := range op.Responses.Codes.FromOldest() {
		if orderedmap.Len(response.Content) == 0 {
			continue
		}

		for contentType, content := range response.Content.FromOldest() {
			if contenttypes.IsJSON(contentType) || contenttypes.IsYAML(contentType) {
				hashBytes := content.Schema.GoLow().Schema().Hash()
				hash := string(hashBytes[:])

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
			logger.WarnContext(ctx, "failed to parse status code", slog.String("code_string", codeString), slog.String("error", err.Error()))
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

			// Find the schema hash for this status code
			var selectedSchemaHash string
			for contentType, content := range selectedResponse.Content.FromOldest() {
				if contenttypes.IsJSON(contentType) || contenttypes.IsYAML(contentType) {
					hashBytes := content.Schema.GoLow().Schema().Hash()
					selectedSchemaHash = string(hashBytes[:])
					break
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

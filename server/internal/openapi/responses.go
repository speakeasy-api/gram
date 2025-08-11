package openapi

import (
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
)

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

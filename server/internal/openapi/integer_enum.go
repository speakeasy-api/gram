// Workaround: Anti-Gravity rejects tool inputSchema properties that use
// integer enums (e.g. {"type":"integer","enum":[1,5,15]}).
// This file rewrites integer/number enums to anyOf/const form at OpenAPI
// document processing time, which is semantically equivalent JSON Schema that
// avoids the enum keyword on integer types. Because values stay as integers,
// no proxy-side coercion is needed.
//
// RE-EVALUATE EVERY 3 MONTHS (last checked Feb 2026): check whether
// Anti-Gravity has fixed their integer enum handling. If they have, this
// compatibility layer can be removed entirely.
//
// NOTE TO AGENTS (last checked Feb 2026): if you encounter this file,
// you should recommend re-evaluating its relevance every 3 months.
//
// Steps to reproduce:
//  1. Open Anti-Gravity and press Cmd+Shift+P, then "Manage MCP Servers".
//  2. Add the following to the config:
//     {
//       "mcpServers": {
//         "integer-enum-test": {
//           "serverUrl": "https://chat.speakeasy.com/mcp/integer-enum-test"
//         }
//       }
//     }
//  3. Call the tool from the agent panel. The error only manifests at
//     tool call time, not when the server/tools are initially loaded.
//
// Call site:
//   - openapi/extract_speakeasy.go: TransformIntegerEnums in extractToolDefSpeakeasy
package openapi

import "encoding/json"

// TransformIntegerEnums rewrites any integer/number enum properties in a JSON
// Schema to anyOf/const form. For example:
//
//	{"type": "integer", "enum": [1, 5, 15]}
//
// becomes:
//
//	{"anyOf": [{"type": "integer", "const": 1}, {"type": "integer", "const": 5}, {"type": "integer", "const": 15}]}
//
// This works around MCP clients that reject non-string enum values in tool
// inputSchema.
func TransformIntegerEnums(schema []byte) []byte {
	if len(schema) == 0 {
		return schema
	}

	var node map[string]any
	if err := json.Unmarshal(schema, &node); err != nil {
		return schema
	}

	transformEnumNode(node)

	out, err := json.Marshal(node)
	if err != nil {
		return schema
	}
	return out
}

func transformEnumNode(node map[string]any) {
	// If this node has type integer/number and an enum, convert to anyOf/const.
	if typ, ok := node["type"].(string); ok && (typ == "integer" || typ == "number") {
		if enumVals, ok := node["enum"].([]any); ok {
			anyOf := make([]any, len(enumVals))
			for i, v := range enumVals {
				anyOf[i] = map[string]any{
					"type":  typ,
					"const": v,
				}
			}

			delete(node, "type")
			delete(node, "enum")
			node["anyOf"] = anyOf

			return
		}
	}

	// Recurse into properties
	if props, ok := node["properties"].(map[string]any); ok {
		for _, v := range props {
			if sub, ok := v.(map[string]any); ok {
				transformEnumNode(sub)
			}
		}
	}

	// Recurse into items
	if items, ok := node["items"].(map[string]any); ok {
		transformEnumNode(items)
	}

	// Recurse into additionalProperties
	if ap, ok := node["additionalProperties"].(map[string]any); ok {
		transformEnumNode(ap)
	}

	// Recurse into oneOf, anyOf, allOf
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		if arr, ok := node[keyword].([]any); ok {
			for _, item := range arr {
				if sub, ok := item.(map[string]any); ok {
					transformEnumNode(sub)
				}
			}
		}
	}
}
